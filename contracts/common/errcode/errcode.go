// Package errcode defines transport-neutral business error codes.
//
// Codes must stay aligned with gofy apperr so the same failure has the same
// code on the server and in client SDKs.
// 包 errcode 定义与传输方式无关的业务错误码。码值必须与 gofy apperr 对齐，
// 同一失败在服务端与客户端 SDK 中才会得到相同的结果。
package errcode

import "net/http"

// Code is a business error code.
// Code 是业务错误码；HTTP 状态码由 Fault.Status 单独表达，两者不可互相替代。
type Code int

// Common codes. They mirror the gofy apperr defaults.
// 通用错误码，取值与 gofy apperr 的默认值一致：CodeOK 为 0 表示成功，
// 其余码值按前三位对应的 HTTP 语义段分配（如 40400 对应 404）。
const (
	CodeOK              Code = 0
	CodeBadRequest      Code = 40000
	CodeInvalidArgument Code = 40001
	CodeUnauthorized    Code = 40100
	CodeForbidden       Code = 40300
	CodeNotFound        Code = 40400
	CodeConflict        Code = 40900
	CodePayloadTooLarge Code = 41300
	CodeTooManyRequests Code = 42900
	CodeInternal        Code = 50000
	CodeUnavailable     Code = 50300
)

// Fault is a pure contract error: it carries no runtime dependency.
// Fault 是纯契约错误，不引用框架运行时，因此契约层与服务端可共用。
// Status 为回给调用方的 HTTP 状态码，Message 为对外可见的提示文案。
type Fault struct {
	Code    Code
	Status  int
	Message string
}

// New builds a Fault.
// New 构造一个 Fault；Code 与 Status 由调用方显式给出，便于自定义映射。
func New(code Code, status int, message string) Fault {
	return Fault{Code: code, Status: status, Message: message}
}

// Error implements error so a Fault can flow through ordinary error returns.
// Error 实现 error 接口，使 Fault 可作为普通错误值透传与包装。
func (f Fault) Error() string { return f.Message }

// Common faults.
// 常用 Fault 实例，Code 与 HTTP 状态码已按语义预绑定，可直接返回给调用方。
var (
	ErrBadRequest      = New(CodeBadRequest, http.StatusBadRequest, "bad request")
	ErrInvalidArgument = New(CodeInvalidArgument, http.StatusBadRequest, "invalid argument")
	ErrUnauthorized    = New(CodeUnauthorized, http.StatusUnauthorized, "unauthorized")
	ErrForbidden       = New(CodeForbidden, http.StatusForbidden, "forbidden")
	ErrNotFound        = New(CodeNotFound, http.StatusNotFound, "not found")
	ErrConflict        = New(CodeConflict, http.StatusConflict, "conflict")
	ErrPayloadTooLarge = New(CodePayloadTooLarge, http.StatusRequestEntityTooLarge, "request entity too large")
	ErrTooManyRequests = New(CodeTooManyRequests, http.StatusTooManyRequests, "too many requests")
	ErrInternal        = New(CodeInternal, http.StatusInternalServerError, "internal error")
	ErrUnavailable     = New(CodeUnavailable, http.StatusServiceUnavailable, "service unavailable")
)
