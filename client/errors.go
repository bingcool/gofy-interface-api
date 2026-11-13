package client

import (
	"errors"
	"fmt"
)

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
