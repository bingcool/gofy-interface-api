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

// StreamSSE starts an SSE request. Events are delivered until the context is
// cancelled or the stream ends. The error channel receives at most one error.
// StreamSSE 发起 SSE 请求。事件持续投递，直到 context 取消或流结束；错误
// channel 最多收到一个错误（成功结束时直接关闭）。
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
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, "text/event-stream", "")
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
			if err == nil || ctx.Err() != nil {
				if err != nil && !errors.Is(err, context.Canceled) {
					errs <- err
				}
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
				errs <- err
				return
			}
			resp, err = c.openStream(ctx, base, rt.Spec(), req, options, "text/event-stream", lastID)
			if err != nil {
				errs <- err
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
	accept string,
	lastEventID string,
) (*http.Response, error) {
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
	return resp, nil
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
				return lastID, nil
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

// DownloadFile starts a download and returns the open body. The caller closes
// Download.Body.
// DownloadFile 发起下载并返回已打开的响应体，调用方负责关闭 Download.Body。
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
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, accept, "")
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

// StreamChunked starts a chunked response and returns its body.
// StreamChunked 发起分块响应并返回其响应体，调用方负责关闭。
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
	resp, err := c.openStream(ctx, base, rt.Spec(), req, options, accept, "")
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
