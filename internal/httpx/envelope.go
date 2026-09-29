package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Now 包络 timestamp 用的时钟（测试可替换）。
var Now = time.Now

func timestamp() string { return Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// Paginated 列表分页（旧 PaginatedData<T>）；Page / Limit 为 nil 时不输出。
type Paginated[T any] struct {
	Items []T  `json:"items"`
	Total int  `json:"total"`
	Page  *int `json:"page,omitempty"`
	Limit *int `json:"limit,omitempty"`
}

// Page 构造带 page/limit 的分页结果；items 为 nil 时输出 []。
func Page[T any](items []T, total, page, limit int) Paginated[T] {
	if items == nil {
		items = []T{}
	}
	return Paginated[T]{Items: items, Total: total, Page: &page, Limit: &limit}
}

// List 构造只有 items/total 的列表（旧版 { items, total: items.length }）。
func List[T any](items []T) Paginated[T] {
	if items == nil {
		items = []T{}
	}
	return Paginated[T]{Items: items, Total: len(items)}
}

type okEnvelope struct {
	Success   bool   `json:"success"`
	Data      any    `json:"data,omitempty"`
	Timestamp string `json:"timestamp"`
}

type errShape struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"requestId"`
}

type errEnvelope struct {
	Success   bool     `json:"success"`
	Error     errShape `json:"error"`
	Timestamp string   `json:"timestamp"`
}

// WriteJSON 写 JSON（不转义 HTML 字符，与 JSON.stringify 一致）。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		slog.Error("JSON 序列化失败", "err", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(500)
		w.Write([]byte(`{"success":false,"error":{"code":"SERVER","message":"服务器内部错误"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(bytes.TrimRight(buf.Bytes(), "\n"))
}

// OK 成功包络 { success: true, data, timestamp }；data 为 nil 时不输出 data（旧 ok()）。
func OK(w http.ResponseWriter, data any) { Write(w, http.StatusOK, data) }

// Created 201 成功包络。
func Created(w http.ResponseWriter, data any) { Write(w, http.StatusCreated, data) }

// Write 指定状态码的成功包络。
func Write(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, okEnvelope{Success: true, Data: data, Timestamp: timestamp()})
}

// Fail 把错误翻译成错误包络：*Error 原样输出；唯一约束冲突 → 409 DUPLICATE；其余 → 500 SERVER（并记日志）。
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	switch {
	case errors.As(err, &e):
	case isUniqueViolation(err):
		e = NewError(CodeDuplicate, "数据已存在，唯一性冲突", nil)
	default:
		slog.Error("未捕获异常", "err", err, "method", r.Method, "path", r.URL.Path, "requestId", RequestID(r))
		e = NewError(CodeServer, "服务器内部错误", nil)
	}
	WriteJSON(w, e.Status, errEnvelope{
		Success:   false,
		Error:     errShape{Code: e.Code, Message: e.Message, Details: e.Details, RequestID: RequestID(r)},
		Timestamp: timestamp(),
	})
}

// isUniqueViolation 与 store.IsUniqueViolation 相同判断（httpx 不依赖 store）。
func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}
