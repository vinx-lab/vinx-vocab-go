package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyBody
)

var reqSeq atomic.Uint64

// NextRequestID 与 Fastify 默认的 genReqId 同形：req-<递增序号的 36 进制>。
func NextRequestID() string { return "req-" + strconv.FormatUint(reqSeq.Add(1), 36) }

// RequestID 当前请求的 id（未经过 Prepare 时现生成一个）。
func RequestID(r *http.Request) string {
	if id, ok := r.Context().Value(keyRequestID).(string); ok {
		return id
	}
	return NextRequestID()
}

// BodyLimit 请求体上限（Fastify 默认 1 MiB）。
const BodyLimit = 1 << 20

type bodyKind int

const (
	bodyNone bodyKind = iota // 无请求体（或 GET/HEAD 不解析）
	bodyJSON                 // application/json，已校验为合法 JSON
	bodyText                 // text/plain，按字符串处理
)

type parsedBody struct {
	kind bodyKind
	raw  []byte
}

// Prepare 每个 API 请求最先经过的处理（对应 Fastify 的 genReqId 与内容类型解析，先于路由匹配）：
//   - 分配 requestId；
//   - 除 GET/HEAD 外读取请求体并按 content-type 解析：application/json 为空 → 400、非法 JSON → 400，
//     text/plain 按字符串，其他类型（或有体无类型）→ 415，超过 1 MiB → 413。错误 code 均为 VALIDATION，message 为 Fastify 原文。
//
// 解析失败时直接写错误包络并返回 nil；成功时返回带上下文的新请求。
func Prepare(w http.ResponseWriter, r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), keyRequestID, NextRequestID())
	r = r.WithContext(ctx)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return r
	}
	pb, err := readBody(r)
	if err != nil {
		Fail(w, r, err)
		return nil
	}
	return r.WithContext(context.WithValue(r.Context(), keyBody, pb))
}

func readBody(r *http.Request) (*parsedBody, *Error) {
	var raw []byte
	if r.Body != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, BodyLimit+1))
		if err != nil {
			return nil, Validation(err.Error())
		}
		raw = b
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	if len(raw) > BodyLimit {
		return nil, Validation("Request body is too large").WithStatus(http.StatusRequestEntityTooLarge)
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		if len(raw) == 0 {
			return &parsedBody{kind: bodyNone}, nil
		}
		return nil, Validation("Unsupported Media Type").WithStatus(http.StatusUnsupportedMediaType)
	}
	mt, _, _ := mime.ParseMediaType(ct)
	switch {
	case mt == "application/json":
		if len(raw) == 0 {
			return nil, Validation("Body cannot be empty when content-type is set to 'application/json'")
		}
		if !json.Valid(raw) {
			return nil, Validation("Body is not valid JSON but content-type is set to 'application/json'")
		}
		return &parsedBody{kind: bodyJSON, raw: raw}, nil
	case mt == "text/plain":
		return &parsedBody{kind: bodyText, raw: raw}, nil
	case len(raw) == 0 && strings.HasPrefix(mt, "multipart/"):
		return &parsedBody{kind: bodyNone}, nil
	}
	return nil, Validation("Unsupported Media Type").WithStatus(http.StatusUnsupportedMediaType)
}

func bodyOf(r *http.Request) *parsedBody {
	if pb, ok := r.Context().Value(keyBody).(*parsedBody); ok {
		return pb
	}
	// 未经过 Prepare（如单测直接调用处理函数）：现读现判
	pb, err := readBody(r)
	if err != nil {
		return &parsedBody{kind: bodyNone}
	}
	return pb
}

// RawBody 请求体原文（已通过 Prepare 的内容类型检查）。
func RawBody(r *http.Request) []byte { return bodyOf(r).raw }

var errNotObject = errors.New("not object")
