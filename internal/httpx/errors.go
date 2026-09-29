// Package httpx HTTP 基础件：统一响应包络、业务错误与错误码表、请求体解析与 zod 风格校验、requestId。
// 与业务无关；路由与守卫在 internal/api 与 internal/auth。
package httpx

import "net/http"

// 业务错误码全集（照抄 packages/shared/src/errors.ts）。
const (
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeForbidden       = "FORBIDDEN"
	CodeValidation      = "VALIDATION"
	CodeNotFound        = "NOT_FOUND"
	CodeInvalidJSON     = "INVALID_JSON"
	CodeMissingFields   = "MISSING_FIELDS"
	CodeInvalidEmail    = "INVALID_EMAIL"
	CodeEmailExists     = "EMAIL_EXISTS"
	CodeInvalidAction   = "INVALID_ACTION"
	CodeInvalidStatus   = "INVALID_STATUS"
	CodeDuplicate       = "DUPLICATE"
	CodeDuplicateName   = "DUPLICATE_NAME"
	CodeNoData          = "NO_DATA"
	CodeTooManyRequests = "TOO_MANY_REQUESTS"
	CodeServer          = "SERVER"
	CodeUnknown         = "UNKNOWN"
)

// ErrorHTTPStatus 错误码 → HTTP 状态。
var ErrorHTTPStatus = map[string]int{
	CodeUnauthorized:    401,
	CodeForbidden:       403,
	CodeValidation:      400,
	CodeNotFound:        404,
	CodeInvalidJSON:     400,
	CodeMissingFields:   400,
	CodeInvalidEmail:    400,
	CodeEmailExists:     409,
	CodeInvalidAction:   400,
	CodeInvalidStatus:   400,
	CodeDuplicate:       409,
	CodeDuplicateName:   409,
	CodeNoData:          404,
	CodeTooManyRequests: 429,
	CodeServer:          500,
	CodeUnknown:         500,
}

// Error 业务异常（对应旧 ApiError）：业务错误码、中文 message、HTTP 状态、可选字段级 details。
type Error struct {
	Code    string
	Message string
	Status  int
	Details map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// NewError 按错误码表取 HTTP 状态；details 为 nil 时包络里不出现 details。
func NewError(code, message string, details map[string]any) *Error {
	status, ok := ErrorHTTPStatus[code]
	if !ok {
		status = 500
	}
	return &Error{Code: code, Message: message, Status: status, Details: details}
}

// WithStatus 覆盖 HTTP 状态（旧版 new ApiError(code, msg, { statusCode })，以及 Fastify 自身 4xx 映射）。
func (e *Error) WithStatus(status int) *Error {
	e.Status = status
	return e
}

// 常用构造。
func Unauthorized(msg string) *Error { return NewError(CodeUnauthorized, msg, nil) }
func Forbidden(msg string) *Error    { return NewError(CodeForbidden, msg, nil) }
func NotFound(msg string) *Error     { return NewError(CodeNotFound, msg, nil) }
func Validation(msg string) *Error   { return NewError(CodeValidation, msg, nil) }

// ErrRouteNotFound 未知路由（旧 setNotFoundHandler）。
func ErrRouteNotFound() *Error { return NotFound("接口不存在") }

// HandlerFunc API 处理函数：返回的错误由路由统一交给 Fail 输出错误包络。
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Middleware 包装 HandlerFunc（守卫、功能开关等）。
type Middleware func(HandlerFunc) HandlerFunc
