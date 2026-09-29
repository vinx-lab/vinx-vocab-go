// Package ai AI 生成能力的纯业务规则（对应旧 apps/api/src/lib/ai-*.ts）：出错说明、回复解析、
// 提示词拼装、配置合并与优先级、后台任务的内存存放。不触库、不读环境变量、不发请求。
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/vinx-lab/vinx-vocab-go/internal/core/secretbox"
)

// AI_EMPTY_REPLY / AI_UNPARSEABLE 固定文案（旧 lib/ai-errors.ts）。
const (
	EmptyReply  = "AI 没有返回内容"
	Unparseable = "AI 返回的内容无法解析"
)

var wsRe = regexp.MustCompile(`\s+`)

func collapseWS(s string) string { return wsRe.ReplaceAllString(strings.TrimSpace(s), " ") }

// utf16Slice 按 JS String.slice(0,n)（UTF-16 code unit）截断。
func utf16Slice(s string, n int) string {
	units := utf16.Encode([]rune(s))
	if len(units) <= n {
		return s
	}
	return string(utf16.Decode(units[:n]))
}

// UpstreamHTTPMessage 上游 HTTP 错误：「AI 服务返回 <状态码>：<上游错误信息>」。
func UpstreamHTTPMessage(status int, upstream string, apiKey string) string {
	detail := utf16Slice(collapseWS(upstream), 300)
	msg := fmt.Sprintf("AI 服务返回 %d", status)
	if detail != "" {
		msg += "：" + detail
	}
	return secretbox.RedactSecret(msg, apiKey)
}

// UpstreamErrorText 从上游错误响应体里取错误信息：OpenAI / Anthropic 的 {error:{message}}、{message}、
// {error:"..."}，或者原文开头。
func UpstreamErrorText(body string) string {
	text := strings.TrimSpace(body)
	if text == "" {
		return ""
	}
	if s, ok := extractErrorText(text); ok {
		return s
	}
	return utf16Slice(text, 160)
}

// extractErrorText 尝试从 JSON 错误体里取 error.message / error（字符串）/ message / detail。
func extractErrorText(text string) (string, bool) {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message *string         `json:"message"`
		Detail  *string         `json:"detail"`
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return "", false
	}
	if len(v.Error) > 0 {
		var s string
		if json.Unmarshal(v.Error, &s) == nil {
			return s, true
		}
		var obj struct {
			Message *string `json:"message"`
		}
		if json.Unmarshal(v.Error, &obj) == nil && obj.Message != nil {
			return *obj.Message, true
		}
	}
	if v.Message != nil {
		return *v.Message, true
	}
	if v.Detail != nil {
		return *v.Detail, true
	}
	return "", false
}

// UnparseableMessage 解析失败：附上回复开头 160 字，方便判断模型说了什么。
func UnparseableMessage(reply string) string {
	head := utf16Slice(collapseWS(reply), 160)
	if head == "" {
		return Unparseable
	}
	return Unparseable + "：" + head
}

// ErrorContext 出错说明需要的上下文。
type ErrorContext struct {
	TimeoutMs int
	APIKey    string
}

// APIError 业务错误（对应旧 ApiError）：core 不依赖 httpx，服务层负责转换。
type APIError struct {
	Code    string
	Message string
	Status  int // 0 表示未覆盖默认状态
	Details map[string]any
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// NewAPIError 构造。
func NewAPIError(code, message string) *APIError { return &APIError{Code: code, Message: message} }

// DescribeError 把 HTTP 调用 / 解析抛出的任意错误转成一句具体说明（去掉可能回显的 Key）。
//
// 与旧版的差异：Node 的 fetch 失败会带 ECONNREFUSED / ENOTFOUND 等 code，这里改用 Go 的
// net.Error / net.OpError / net.DNSError 判断，说明文案的具体措辞按 Go 运行时的错误可能不同，
// 但结构一致（超时 / 连接失败 / 上游 HTTP / 解析失败 / 其他）。
func DescribeError(err error, ctx ErrorContext) string {
	var message string
	var apiErr *APIError
	var netErr net.Error
	switch {
	case errors.As(err, &apiErr):
		message = apiErr.Message
	case errors.As(err, &netErr) && netErr.Timeout():
		message = fmt.Sprintf("请求超时（%d 秒），可以在系统设置里调大超时，或换一个更快的模型", int(math.Round(float64(ctx.TimeoutMs)/1000)))
	default:
		if code := dialErrorCode(err); code != "" {
			message = "无法连接 AI 服务：" + code
		} else if err != nil {
			message = "AI 调用失败：" + err.Error()
		} else {
			message = "AI 调用失败：未知错误"
		}
	}
	return secretbox.RedactSecret(message, ctx.APIKey)
}

// dialErrorCode 从拨号 / DNS 错误里取一个简短代码；不是这类错误时返回空串。
func dialErrorCode(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "ENOTFOUND"
		}
		return "EAI_FAIL"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Err != nil {
			return opErr.Err.Error()
		}
		return opErr.Op
	}
	return ""
}

// ToAPIError 转成 *APIError（直接返回结果的接口用；保留业务错误的错误码，其余按 SERVER）。
func ToAPIError(err error, ctx ErrorContext) *APIError {
	message := DescribeError(err, ctx)
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return &APIError{Code: apiErr.Code, Message: message, Status: apiErr.Status, Details: apiErr.Details}
	}
	return &APIError{Code: "SERVER", Message: message}
}
