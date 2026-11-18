package client

import (
	"errors"
	"fmt"
)

// ErrMultipartTooLarge is returned instead of building a body larger than
// Config.MaxMultipartBytes. It fires before any bytes are sent, and the wrapped
// message carries the limit, so a caller can tell "file too big" apart from a
// transport failure.
// ErrMultipartTooLarge 在请求体会超过 Config.MaxMultipartBytes 时返回，替代继续构建
// 请求体。它在发出任何字节之前触发，包装信息里带着上限，调用方因此能把"文件太大"与
// 传输失败区分开。
var ErrMultipartTooLarge = errors.New("client: multipart body exceeds limit")

// ErrResponseKindMismatch is returned when a streaming helper is used with a
// route whose Spec.ResponseKind names a different shape. The check runs before
// the request is sent, so a miswired route cannot consume a stream as a file
// or vice versa.
// ErrResponseKindMismatch 在流式 helper 与 Spec.ResponseKind 声明的形态不一致时
// 返回。检查在发送请求之前执行，接错 helper 的路由不会把流当成文件消费，反之亦然。
var ErrResponseKindMismatch = errors.New("client: route response kind does not match the helper")

// ErrEventTooLarge is returned when one SSE event exceeds Config.MaxEventBytes:
// either a single line without a newline, or the lines accumulated for one
// event. A stream has no total response size to bound it, so this is the only
// thing standing between a misbehaving server and unbounded client memory. The
// stream is not retried after this error — the server would do it again.
// ErrEventTooLarge 在单个 SSE 事件超过 Config.MaxEventBytes 时返回：可能是一行不含
// 换行的超长内容，也可能是一个事件累计的多行。流没有响应体总大小可以兜底，因此这是
// 异常服务端与客户端内存之间唯一的约束。这个错误不会触发重连——重连只会重来一次。
var ErrEventTooLarge = errors.New("client: sse event exceeds limit")

// APIError is a business error returned by the server envelope.
// APIError 是服务端信封返回的业务错误。
type APIError struct {
	Status  int
	Code    int
	Message string
	ReqID   string
	Details map[string]string

	// Raw is the response body, kept for diagnostics. It never contains
	// credentials because the server strips internal causes.
	// Raw 保留响应体用于诊断。服务端已剥离内部原因，因此其中不会含凭证。
	Raw []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = "api error"
	}
	if e.Code != 0 {
		return fmt.Sprintf("%s (status=%d, code=%d)", msg, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (status=%d)", msg, e.Status)
}

// HTTPError is returned when a non-2xx response is not a valid envelope.
// HTTPError 在非 2xx 响应且响应体不是合法信封时返回。
type HTTPError struct {
	Status     int
	BodyPrefix string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.BodyPrefix == "" {
		return fmt.Sprintf("unexpected http status %d", e.Status)
	}
	return fmt.Sprintf("unexpected http status %d: %s", e.Status, e.BodyPrefix)
}

// ProtocolError is returned when a response cannot be decoded as the expected
// envelope.
// ProtocolError 在响应无法按预期信封解码时返回。
type ProtocolError struct {
	Message string
	Err     error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("protocol error: %s: %v", e.Message, e.Err)
	}
	return "protocol error: " + e.Message
}

func (e *ProtocolError) Unwrap() error { return e.Err }

// CodeOf returns the business code carried by err, or 0.
// CodeOf 返回 err 携带的业务码，没有时返回 0。
func CodeOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return 0
}

// RequestIDOf returns the request id carried by err, or "".
// RequestIDOf 返回 err 携带的请求 ID，没有时返回 ""。
func RequestIDOf(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.ReqID
	}
	return ""
}

// DetailsOf returns field-level validation details carried by err, or nil.
// DetailsOf 返回 err 携带的字段级校验明细，没有时返回 nil。
func DetailsOf(err error) map[string]string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Details
	}
	return nil
}
