package client

import (
	"context"
	"net/http"
	"net/textproto"
	"net/url"
	"time"
)

type callOptions struct {
	headers        http.Header
	bearer         string
	requestID      string
	query          url.Values
	timeout        time.Duration
	connectTimeout time.Duration
	httpClient     *http.Client
}

// CallOption customizes a single call. Options apply to Call and to the
// streaming helpers (StreamSSE, DownloadFile, StreamChunked).
// CallOption 定制单次调用，对 Call 与流式接口（StreamSSE、DownloadFile、
// StreamChunked）都生效。
type CallOption func(*callOptions)

// WithHeader sets or replaces a request header.
// WithHeader 设置或替换一个请求 Header。
func WithHeader(key, value string) CallOption {
	return func(o *callOptions) {
		o.headers.Set(key, value)
	}
}

// WithHeaders adds every value of h, replacing existing values for the same
// keys. Use it to forward a set of headers without one WithHeader per key.
// WithHeaders 批量添加 h 中的全部值，并替换同名 key 的已有值；需要透传一组
// Header 时不必逐个调用 WithHeader。
func WithHeaders(h http.Header) CallOption {
	return func(o *callOptions) {
		for key, values := range h {
			o.headers[textproto.CanonicalMIMEHeaderKey(key)] = append([]string(nil), values...)
		}
	}
}

// WithBearerToken overrides the client TokenSource for one call.
// WithBearerToken 为单次调用覆盖 Client 的 TokenSource。
func WithBearerToken(token string) CallOption {
	return func(o *callOptions) { o.bearer = token }
}

// WithRequestID sets the request id header.
// WithRequestID 设置请求 ID Header。
func WithRequestID(id string) CallOption {
	return func(o *callOptions) { o.requestID = id }
}

// WithQuery appends a query parameter.
// WithQuery 追加一个查询参数（同名参数会累加，而不是覆盖）。
func WithQuery(key, value string) CallOption {
	return func(o *callOptions) { o.query.Add(key, value) }
}

// WithTimeout bounds one ordinary call, including retries and the response
// body read. It overrides Client.Timeout. Streaming calls ignore it, because
// their context governs how long the stream stays open; use WithConnectTimeout
// to bound the handshake there.
// WithTimeout 限制单次普通调用的耗时，包含重试和响应体读取，优先级高于
// Client.Timeout。流式调用忽略它（流的存活时长由 context 决定），需要限制
// 握手时改用 WithConnectTimeout。
func WithTimeout(d time.Duration) CallOption {
	return func(o *callOptions) { o.timeout = d }
}

// WithConnectTimeout bounds the TCP dial for one call. Calls that request the
// same duration share a transport, so their connections are pooled together;
// each distinct duration keeps its own pool. The configured *http.Client must
// use an *http.Transport (the default does), otherwise the call fails with an
// error. It does not change the TLS handshake timeout.
// WithConnectTimeout 限制单次调用的 TCP 拨号耗时。相同取值的调用共用 transport
// 与连接池，不同取值各自保留连接池。配置的 *http.Client 必须使用
// *http.Transport（默认实现满足），否则调用直接报错。它不改变 TLS 握手超时。
func WithConnectTimeout(d time.Duration) CallOption {
	return func(o *callOptions) { o.connectTimeout = d }
}

// WithHTTPClient runs one call through cl instead of the configured client. It
// is the escape hatch for per-endpoint TLS, proxy or instrumentation needs and
// takes precedence over WithConnectTimeout.
// WithHTTPClient 让单次调用改用 cl 而不是配置的 Client，用于按 Endpoint 定制
// TLS、代理或埋点，优先级高于 WithConnectTimeout。
func WithHTTPClient(cl *http.Client) CallOption {
	return func(o *callOptions) { o.httpClient = cl }
}

type requestIDKey struct{}

// ContextWithRequestID attaches a request id that the client forwards.
// ContextWithRequestID 把请求 ID 放进 ctx，Client 会将其写入请求 Header。
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request id attached with
// ContextWithRequestID.
// RequestIDFromContext 返回由 ContextWithRequestID 附加的请求 ID。
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
