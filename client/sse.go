package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

// ErrStreamEnded reports that the server closed the SSE stream cleanly. A
// clean EOF is treated like any other disconnect: StreamSSE reconnects with
// Last-Event-ID until Retry is exhausted, then delivers this error. An
// intentional stop is context cancellation, which ends the stream without an
// error.
// ErrStreamEnded 表示服务端正常关闭了 SSE 流。正常 EOF 与其他断连一样处理：
// StreamSSE 带 Last-Event-ID 重连，直到 Retry 用尽后投递这个错误。主动停止应取消
// context，此时流直接结束且不报错。
var ErrStreamEnded = errors.New("client: event stream ended")

// Event is one server-sent event.
// Event 是一条 Server-Sent Event。
type Event struct {
	Event string
	ID    string
	Data  []byte
	Retry int
}

// Download is an open response stream plus metadata.
// Download 是一条已打开的响应流及其元数据。
type Download struct {
	Body        io.ReadCloser
	Filename    string
	ContentType string
	Size        int64
}

// StreamSSE starts an SSE request for a route declared as endpoint.KindSSE.
// Events are delivered until the context is cancelled. A clean EOF counts as a
// disconnect: the stream reconnects with Last-Event-ID until Retry is
// exhausted, then ErrStreamEnded is delivered. The error channel receives at
// most one error.
// StreamSSE 为声明为 endpoint.KindSSE 的路由发起 SSE 请求。事件持续投递，直到
// context 取消。正常 EOF 也算断连：流会带 Last-Event-ID 重连，直到 Retry 用尽后
// 投递 ErrStreamEnded。错误 channel 最多收到一个错误。
func StreamSSE[Req any](
	ctx context.Context,
	c *Client,
	rt *endpoint.Route[Req, endpoint.NoBody],
	req *Req,
	opts ...CallOption,
) (<-chan Event, <-chan error, error) {
	if c == nil {
		return nil, nil, errors.New("client: client is nil")
	}
	if rt == nil {
		return nil, nil, errors.New("client: route is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options := newCallOptions(opts)
	base, err := c.base(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, endpoint.KindSSE, "text/event-stream", "")
	if err != nil {
		return nil, nil, err
	}

	events := make(chan Event)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		defer func() { drainAndClose(resp) }()

		lastID := ""
		for attempt := 0; ; attempt++ {
			lastID, err = readEvents(ctx, resp.Body, events, lastID)
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				return
			}
			if attempt >= c.retry {
				errs <- err
				return
			}
			_ = resp.Body.Close()
			sleep(ctx, backoff(attempt))
			if ctx.Err() != nil {
				return
			}
			// Re-resolve on every reconnect, the same way a normal call resolves
			// on every retry: with a Resolver the instance that just dropped may
			// be gone, and staying on its address would loop against a dead
			// endpoint. A fixed BaseURL simply returns the same address again.
			// 每次重连都重新解析，与普通调用每次重试都解析一致：用 Resolver 时刚断开的
			// 实例可能已经不在了，停在它的地址上只会对着一个死端点重试。固定 BaseURL
			// 只是又返回同一个地址。
			base, err = c.base(ctx)
			if err != nil {
				if ctx.Err() == nil {
					errs <- err
				}
				return
			}
			resp, err = c.openStream(ctx, base, rt.Spec(), req, options, endpoint.KindSSE, "text/event-stream", lastID)
			if err != nil {
				if ctx.Err() == nil {
					errs <- err
				}
				return
			}
		}
	}()
	return events, errs, nil
}

func (c *Client) openStream(
	ctx context.Context,
	base string,
	spec endpoint.Spec,
	req any,
	opts callOptions,
	kind endpoint.ResponseKind,
	accept string,
	lastEventID string,
) (*http.Response, error) {
	if err := requireResponseKind(spec, kind); err != nil {
		return nil, err
	}
	if lastEventID != "" {
		opts.headers = opts.headers.Clone()
		opts.headers.Set("Last-Event-ID", lastEventID)
	}
	httpClient, err := c.httpClientFor(opts)
	if err != nil {
		return nil, err
	}
	httpReq, err := c.buildRequest(ctx, base, spec, req, opts, accept)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("client: %s %s: %w", spec.Method, spec.Path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		body, readErr := readLimited(resp.Body, c.maxResponse)
		if readErr != nil {
			return nil, readErr
		}
		return nil, responseError(resp, body)
	}
	if err := c.validateStreamResponse(resp, spec, kind); err != nil {
		return nil, err
	}
	return resp, nil
}

func requireResponseKind(spec endpoint.Spec, want endpoint.ResponseKind) error {
	if spec.ResponseKind == want {
		return nil
	}
	return fmt.Errorf("%w: route %s declares %q, helper requires %q",
		ErrResponseKindMismatch, spec.OperationID, spec.ResponseKind, want)
}

// validateStreamResponse rejects a 2xx answer whose Content-Type belongs to a
// different shape. Without it a gofy error envelope arrives as file bytes on a
// download or chunked stream, and an SSE client sees a silent zero-event end.
// A declared ResponseContentType must match exactly (a trailing "/*" wildcard
// is honored); when the contract declares none, any non-JSON type is accepted
// so one download route can serve several file formats.
// validateStreamResponse 拒绝 Content-Type 与形态不符的 2xx 响应。没有这道检查时，
// gofy 错误信封会在下载/分块流上变成文件字节，SSE 则表现为静默零事件结束。契约声明的
// ResponseContentType 必须精确匹配（支持尾部 "/*" 通配）；未声明时接受任意非 JSON
// 类型，让一个下载路由可以返回多种文件格式。
func (c *Client) validateStreamResponse(resp *http.Response, spec endpoint.Spec, kind endpoint.ResponseKind) error {
	got := strings.TrimSpace(resp.Header.Get("Content-Type"))
	expected := streamContentType(spec, kind)
	if streamContentTypeMatches(got, expected) {
		return nil
	}

	defer func() { _ = resp.Body.Close() }()
	body, err := readLimited(resp.Body, c.maxResponse)
	if err != nil {
		return err
	}
	var httpErr *HTTPError
	if err := responseError(resp, body); !errors.As(err, &httpErr) {
		return err
	}
	want := "a non-JSON stream"
	if expected != "" {
		want = fmt.Sprintf("%q", expected)
	}
	return &ProtocolError{Message: fmt.Sprintf("unexpected Content-Type %q, want %s", got, want)}
}

func streamContentType(spec endpoint.Spec, kind endpoint.ResponseKind) string {
	if kind == endpoint.KindSSE {
		return "text/event-stream"
	}
	return strings.TrimSpace(spec.ResponseContentType)
}

func streamContentTypeMatches(header, expected string) bool {
	actual, _, err := mime.ParseMediaType(header)
	if err != nil || actual == "" {
		return false
	}
	if expected == "" {
		return !isJSONMediaType(actual)
	}
	want, _, err := mime.ParseMediaType(expected)
	if err != nil || want == "" {
		return false
	}
	if want == "*/*" || want == actual {
		return true
	}
	if strings.HasSuffix(want, "/*") {
		return strings.HasPrefix(actual, strings.TrimSuffix(want, "*"))
	}
	return false
}

func isJSONMediaType(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func readEvents(ctx context.Context, body io.Reader, out chan<- Event, lastID string) (string, error) {
	reader := bufio.NewReaderSize(body, 64<<10)
	var (
		event   string
		data    []string
		retry   int
		hasData bool
	)
	dispatch := func() bool {
		if !hasData && event == "" {
			return true
		}
		ev := Event{Event: event, ID: lastID, Retry: retry}
		if len(data) > 0 {
			ev.Data = []byte(strings.Join(data, "\n"))
		}
		event, data, retry, hasData = "", nil, 0, false
		select {
		case <-ctx.Done():
			return false
		case out <- ev:
			return true
		}
	}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if !dispatch() {
					return lastID, ctx.Err()
				}
			case strings.HasPrefix(line, ":"):
				// Comment/heartbeat.
				// 注释行/心跳：SSE 规范中以 ":" 开头的行只用于保活，忽略即可。
			default:
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					event = value
				case "data":
					data = append(data, value)
					hasData = true
				case "id":
					if !strings.ContainsRune(value, 0) {
						lastID = value
					}
				case "retry":
					if n, convErr := strconv.Atoi(value); convErr == nil && n >= 0 {
						retry = n
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if !dispatch() {
					return lastID, ctx.Err()
				}
				return lastID, ErrStreamEnded
			}
			return lastID, fmt.Errorf("client: read event stream: %w", err)
		}
		select {
		case <-ctx.Done():
			return lastID, ctx.Err()
		default:
		}
	}
}

// DownloadFile starts a download for a route declared as
// endpoint.KindDownload and returns the open body. The caller closes
// Download.Body.
// DownloadFile 为声明为 endpoint.KindDownload 的路由发起下载并返回已打开的响应体，
// 调用方负责关闭 Download.Body。
func DownloadFile[Req any](
	ctx context.Context,
	c *Client,
	rt *endpoint.Route[Req, endpoint.NoBody],
	req *Req,
	opts ...CallOption,
) (*Download, error) {
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
	base, err := c.base(ctx)
	if err != nil {
		return nil, err
	}
	accept := rt.Spec().ResponseContentType
	if accept == "" {
		accept = "*/*"
	}
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, endpoint.KindDownload, accept, "")
	if err != nil {
		return nil, err
	}
	return &Download{
		Body:        resp.Body,
		Filename:    filename(resp.Header.Get("Content-Disposition")),
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
	}, nil
}

// StreamChunked starts a chunked response for a route declared as
// endpoint.KindChunked and returns its body.
// StreamChunked 为声明为 endpoint.KindChunked 的路由发起分块响应并返回其响应体，
// 调用方负责关闭。
func StreamChunked[Req any](
	ctx context.Context,
	c *Client,
	rt *endpoint.Route[Req, endpoint.NoBody],
	req *Req,
	opts ...CallOption,
) (io.ReadCloser, error) {
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
	base, err := c.base(ctx)
	if err != nil {
		return nil, err
	}
	accept := rt.Spec().ResponseContentType
	if accept == "" {
		accept = "application/octet-stream"
	}
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, endpoint.KindChunked, accept, "")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func filename(disposition string) string {
	if disposition == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		if name := params["filename"]; name != "" {
			return name
		}
		if encoded := params["filename*"]; encoded != "" {
			if _, value, ok := strings.Cut(encoded, "''"); ok {
				if decoded, err := url.QueryUnescape(value); err == nil {
					return decoded
				}
			}
		}
	}
	return ""
}

// EscapePath is a helper for callers that build URLs manually.
// EscapePath 供手工拼接 URL 的调用方转义路径片段。
func EscapePath(value string) string { return url.PathEscape(value) }
