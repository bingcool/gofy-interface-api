package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	timeout := options.timeout
	if timeout == 0 {
		timeout = c.timeout
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Client) send(ctx context.Context, spec endpoint.Spec, req any, opts callOptions, accept string) (*http.Response, error) {
	httpClient, err := c.httpClientFor(opts)
	if err != nil {
		return nil, err
	}
	attempts := 1 + c.retryFor(spec.Method, opts.headers)
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

func (c *Client) retryFor(method string, headers http.Header) int {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodDelete:
		return c.retry
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		// A caller-provided idempotency key makes the request replayable.
		// 调用方提供幂等键时，请求可以安全重放。
		if headers.Get("Idempotency-Key") != "" {
			return c.retry
		}
	}
	return 0
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
			return nil, nil
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
