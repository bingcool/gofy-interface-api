package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type wireEnvelope struct {
	Code    int               `json:"code"`
	Msg     string            `json:"msg"`
	Data    json.RawMessage   `json:"data"`
	ReqID   string            `json:"req_id"`
	Details map[string]string `json:"details"`
}

// Call performs a typed request against rt.
// Call 按路由描述符 rt 发起一次类型化请求。
func Call[Req, Res any](
	ctx context.Context,
	c *Client,
	rt *endpoint.Route[Req, Res],
	req *Req,
	opts ...CallOption,
) (*Res, error) {
	if c == nil {
		return nil, errors.New("client: client is nil")
	}
	if rt == nil {
		return nil, errors.New("client: route is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options := newCallOptions(opts)
	ctx, cancel := c.callContext(ctx, options)
	defer cancel()

	resp, err := c.send(ctx, rt.Spec(), req, options, "application/json")
	if err != nil {
		return nil, err
	}
	return decodeResponse[Res](resp, rt.Spec(), c.maxResponse)
}

// newCallOptions builds the option set every call starts from. Both the header
// map and the query values are allocated here: the With* options append to them
// (http.Header.Set and url.Values.Add both need a live map), so a nil field
// would turn an ordinary option into a panic on the caller's side.
// newCallOptions 构造每次调用共享的初始选项。header map 与 query 值都在这里分配：
// With* 选项会往里面追加（http.Header.Set 与 url.Values.Add 都需要已初始化的 map），
// 留成 nil 会把一个普通选项变成调用方的 panic。
func newCallOptions(opts []CallOption) callOptions {
	options := callOptions{
		headers: make(http.Header),
		query:   make(url.Values),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

func (c *Client) callContext(ctx context.Context, options callOptions) (context.Context, context.CancelFunc) {
	timeout := c.timeout
	if options.timeoutSet {
		timeout = options.timeout
	}
	if timeout <= 0 {
		// NoTimeout (Config or WithTimeout) means the caller's context is the
		// only deadline, so no derived timer is installed.
		// NoTimeout（配置或 WithTimeout）表示只由调用方 ctx 决定截止时间，因此
		// 不派生计时器。
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Client) send(ctx context.Context, spec endpoint.Spec, req any, opts callOptions, accept string) (*http.Response, error) {
	httpClient, err := c.httpClientFor(opts)
	if err != nil {
		return nil, err
	}
	attempts := 1 + c.retryFor(ctx, spec.Method, req, opts)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		// Resolve on every attempt: the resolver picks an instance per call, so
		// a retry after a transport error or a 502/503/504 is free to land on a
		// different instance instead of repeating the one that just failed.
		// 每次尝试都重新寻址：resolver 每次调用都会选一个实例，因此传输错误或
		// 502/503/504 之后的重试可以落到另一个实例，而不是重复访问刚失败的实例。
		base, err := c.base(ctx)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		httpReq, err := c.buildRequest(ctx, base, spec, req, opts, accept)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			if attempt+1 < attempts && ctx.Err() == nil {
				lastErr = err
				sleep(ctx, backoff(attempt))
				continue
			}
			return nil, fmt.Errorf("client: %s %s: %w", spec.Method, spec.Path, err)
		}
		if attempt+1 < attempts {
			if delay, retry := retryAfter(resp); retry {
				drainAndClose(resp)
				if delay == 0 {
					delay = backoff(attempt)
				}
				sleep(ctx, delay)
				if ctx.Err() != nil {
					return nil, fmt.Errorf("client: %s %s: %w", spec.Method, spec.Path, ctx.Err())
				}
				continue
			}
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("client: retry loop exhausted")
}

// retryFor decides how many retries this request may have. A write method is
// only replayable with an idempotency key, and the key can arrive through any of
// the places a header comes from: CallOptions, the request struct's header tags,
// or the propagated headers in ctx. All of them are checked here because this
// decision is made before buildRequest merges them — looking only at the options
// silently disabled retries for a contract that declares the key as a field.
// retryFor 决定这次请求可以重试几次。写方法只有带幂等键才可重放，而幂等键可以来自
// 三个地方：CallOptions、请求结构体的 header 标签、ctx 中的透传 Header。三者都在
// 这里检查，因为该判定发生在 buildRequest 合并它们之前——只看选项会让"把幂等键
// 声明成契约字段"的调用彻底失去重试。
func (c *Client) retryFor(ctx context.Context, method string, req any, opts callOptions) int {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodDelete:
		return c.retry
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		if carriesIdempotencyKey(ctx, req, opts.headers) {
			return c.retry
		}
	}
	return 0
}

// carriesIdempotencyKey reports whether the outgoing request will carry a
// non-empty Idempotency-Key. Header names are case-insensitive, so each source is
// compared that way, and a present-but-empty value is not a key.
// carriesIdempotencyKey 判断发出去的请求是否会带非空的 Idempotency-Key。Header 名
// 大小写不敏感，因此每个来源都按同样的规则比较；存在但为空的值不算幂等键。
func carriesIdempotencyKey(ctx context.Context, req any, headers http.Header) bool {
	if headerHasValue(headers, IdempotencyKeyHeader) {
		return true
	}
	// Propagated headers go through an allowlist that does not list this key
	// today. Checking it anyway keeps the decision aligned with where headers can
	// come from, so adding the key to the allowlist will not silently disable
	// retries again.
	// 透传 Header 走白名单，当前白名单没有这个键。这里仍然检查，是为了让判定与
	// "header 可能来自哪里"保持一致：以后把该键加进白名单时，不会又悄悄丢掉重试。
	if headerHasValue(HeadersFromContext(ctx), IdempotencyKeyHeader) {
		return true
	}
	found := false
	// walkStruct has no early exit; a request struct is small and this runs once
	// per call, not once per attempt.
	// walkStruct 不支持提前退出；请求结构体很小，而且这里每次调用只跑一次，不是
	// 每次重试跑一次。
	_ = walkStruct(reflect.ValueOf(req), func(field reflect.StructField, value reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("header"))
		if !strings.EqualFold(strings.TrimSpace(name), IdempotencyKeyHeader) {
			return nil
		}
		if text, err := stringify(value); err == nil && strings.TrimSpace(text) != "" {
			found = true
		}
		return nil
	})
	return found
}

// headerHasValue is Header.Get without the canonical-key assumption: a header map
// can hold non-canonical keys (they are copied verbatim from context), and a
// missing canonical key must not hide a real value.
// headerHasValue 相当于不假设 key 已规范化的 Header.Get：header map 里可能有非规范
// 化的 key（它们是从 context 原样复制来的），只查规范化 key 会漏掉真实的值。
func headerHasValue(h http.Header, name string) bool {
	for key, values := range h {
		if !strings.EqualFold(key, name) {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return true
			}
		}
	}
	return false
}

func decodeResponse[Res any](resp *http.Response, spec endpoint.Spec, max int64) (*Res, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := readLimited(resp.Body, max)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if len(body) == 0 {
			if spec.NoContent || resp.StatusCode == http.StatusNoContent {
				return nil, nil
			}
			return nil, &ProtocolError{Message: "empty response body"}
		}
		var env wireEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, &ProtocolError{Message: "invalid envelope", Err: err}
		}
		if env.Code != 0 {
			return nil, apiError(resp.StatusCode, env, body)
		}
		if isEmptyData(env.Data) {
			// Only "no body" routes (NoContent or the NoBody placeholder) and
			// pointer response types can represent "no data". For a value type a
			// missing payload means the server broke the contract; returning
			// (nil, nil) would hand the caller a nil pointer it is about to
			// dereference.
			// 只有"无响应体"路由（NoContent 或 NoBody 占位类型）与指针响应类型能表达
			// "没有数据"。值类型响应缺少 payload 意味着服务端违反了契约；返回
			// (nil, nil) 会让调用方解引用空指针。
			if spec.NoContent || isPointerResponse[Res]() || isNoBodyResponse[Res]() {
				return nil, nil
			}
			return nil, &ProtocolError{Message: "missing data payload"}
		}
		var out Res
		if err := json.Unmarshal(env.Data, &out); err != nil {
			return nil, &ProtocolError{Message: "invalid data payload", Err: err}
		}
		return &out, nil
	}

	return nil, responseError(resp, body)
}

func responseError(resp *http.Response, body []byte) error {
	var env wireEnvelope
	if err := json.Unmarshal(body, &env); err == nil && (env.Code != 0 || env.Msg != "") {
		return apiError(resp.StatusCode, env, body)
	}
	return &HTTPError{Status: resp.StatusCode, BodyPrefix: bodyPrefix(body)}
}

func apiError(status int, env wireEnvelope, body []byte) *APIError {
	return &APIError{
		Status:  status,
		Code:    env.Code,
		Message: env.Msg,
		ReqID:   env.ReqID,
		Details: env.Details,
		Raw:     body,
	}
}

func isEmptyData(data json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(data))
	return trimmed == "" || trimmed == "null"
}

// isPointerResponse reports whether Res can hold a nil, i.e. whether "no data"
// has a natural representation for the caller.
// isPointerResponse 判断 Res 能否为 nil，也就是"没有数据"对调用方是否有自然的
// 表示形式。
func isPointerResponse[Res any]() bool {
	return reflect.TypeOf((*Res)(nil)).Elem().Kind() == reflect.Pointer
}

// isNoBodyResponse reports whether Res is the endpoint.NoBody placeholder that
// marks a route without a JSON response.
// isNoBodyResponse 判断 Res 是否为 endpoint.NoBody 占位类型，它标记没有 JSON 响应
// 的路由。
func isNoBodyResponse[Res any]() bool {
	return reflect.TypeOf((*Res)(nil)).Elem() == noBodyType
}

var noBodyType = reflect.TypeOf(endpoint.NoBody{})

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return io.ReadAll(r)
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("client: read response: %w", err)
	}
	if int64(len(body)) > max {
		return nil, &ProtocolError{Message: fmt.Sprintf("response exceeds %d bytes", max)}
	}
	return body, nil
}

func bodyPrefix(body []byte) string {
	const max = 256
	if len(body) > max {
		return string(body[:max]) + "..."
	}
	return string(body)
}

func drainAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}
