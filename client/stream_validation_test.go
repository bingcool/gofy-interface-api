package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type streamProbeReq struct{}

func streamProbeRoute(kind endpoint.ResponseKind, contentType string) *endpoint.Route[streamProbeReq, endpoint.NoBody] {
	return endpoint.New[streamProbeReq, endpoint.NoBody](endpoint.Spec{
		OperationID:         "StreamProbe",
		Method:              http.MethodGet,
		Path:                "/stream/probe",
		ResponseKind:        kind,
		ResponseContentType: contentType,
	})
}

// TestStreamSSEReconnectsAfterCleanEOF is the D3 acceptance test: the server
// returns normally after one event, which used to end the stream silently. The
// client must treat that EOF as a disconnect, resolve again and resume from
// Last-Event-ID instead of handing the caller a closed channel.
// TestStreamSSEReconnectsAfterCleanEOF 是 D3 的验收测试：服务端发出一个事件后正常
// 返回，过去会让流静默结束。客户端必须把这次 EOF 当作断连，重新解析地址并从
// Last-Event-ID 续传，而不是把已关闭的 channel 交给调用方。
func TestStreamSSEReconnectsAfterCleanEOF(t *testing.T) {
	var hits atomic.Int32
	var lastEventID atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch hits.Add(1) {
		case 1:
			lastEventID.Store("")
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			_, _ = io.WriteString(w, "id: 1\ndata: one\n\n")
			w.(http.Flusher).Flush()
		default:
			lastEventID.Store(r.Header.Get("Last-Event-ID"))
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: 2\ndata: two\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL, Retry: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, streamProbeRoute(endpoint.KindSSE, ""), &streamProbeReq{})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	var seen []string
	for len(seen) < 2 {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream closed after %v; err = %v", seen, <-errs)
			}
			seen = append(seen, string(event.Data))
		case streamErr := <-errs:
			t.Fatalf("stream error after %v: %v", seen, streamErr)
		case <-ctx.Done():
			t.Fatalf("timed out after %v", seen)
		}
	}
	if strings.Join(seen, ",") != "one,two" {
		t.Fatalf("events = %v, want [one two]", seen)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server handled %d connections, want 2", got)
	}
	if got, _ := lastEventID.Load().(string); got != "1" {
		t.Fatalf("Last-Event-ID on reconnect = %q, want 1", got)
	}
}

// TestStreamSSEReportsErrStreamEndedAfterRetries pins the other half of D3: an
// endless clean-EOF loop must not look like a successful zero-event stream. Once
// Retry is spent, the caller receives ErrStreamEnded.
// TestStreamSSEReportsErrStreamEndedAfterRetries 固定 D3 的另一半：反复正常 EOF
// 不能表现成成功的零事件流。Retry 用尽后调用方会收到 ErrStreamEnded。
func TestStreamSSEReportsErrStreamEndedAfterRetries(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: transient\n\n")
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL, Retry: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, streamProbeRoute(endpoint.KindSSE, ""), &streamProbeReq{})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	for range events {
	}
	select {
	case streamErr := <-errs:
		if !errors.Is(streamErr, client.ErrStreamEnded) {
			t.Fatalf("stream error = %v, want ErrStreamEnded", streamErr)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the exhausted reconnect budget")
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server handled %d connections, want 2", got)
	}
}

// TestStreamingHelpersRejectJSONResponses is the D2 acceptance test: a 2xx
// gofy error envelope has to surface as an APIError on every streaming helper,
// not as file bytes or a silent empty stream.
// TestStreamingHelpersRejectJSONResponses 是 D2 的验收测试：2xx 的 gofy 错误信封
// 在每种流式 helper 上都必须变成 APIError，而不是文件字节或静默空流。
func TestStreamingHelpersRejectJSONResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":503,"msg":"backend unavailable","req_id":"r-1"}`)
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	assertAPIError := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s accepted a JSON error envelope", name)
		}
		if code := client.CodeOf(err); code != 503 {
			t.Fatalf("%s error = %v, code = %d, want 503", name, err, code)
		}
	}

	_, _, err = client.StreamSSE(context.Background(), c, streamProbeRoute(endpoint.KindSSE, ""), &streamProbeReq{})
	assertAPIError("StreamSSE", err)

	_, err = client.DownloadFile(context.Background(), c, streamProbeRoute(endpoint.KindDownload, ""), &streamProbeReq{})
	assertAPIError("DownloadFile", err)

	_, err = client.StreamChunked(context.Background(), c, streamProbeRoute(endpoint.KindChunked, ""), &streamProbeReq{})
	assertAPIError("StreamChunked", err)
}

// TestStreamingHelpersRejectWrongResponseKind pins the pre-flight check: a
// route declared for one shape cannot be consumed by another helper, and the
// request is never sent.
// TestStreamingHelpersRejectWrongResponseKind 固定发送前检查：声明为一种形态的路由
// 不能被另一个 helper 消费，且请求不会被发出。
func TestStreamingHelpersRejectWrongResponseKind(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	_, _, err = client.StreamSSE(context.Background(), c, streamProbeRoute(endpoint.KindDownload, ""), &streamProbeReq{})
	if !errors.Is(err, client.ErrResponseKindMismatch) {
		t.Fatalf("StreamSSE error = %v, want ErrResponseKindMismatch", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("server handled %d requests, want 0", got)
	}
}

// TestDownloadFileEnforcesDeclaredContentType covers the declared half of D2:
// when the contract names a response type, a different one is a protocol error
// even though the status is 2xx.
// TestDownloadFileEnforcesDeclaredContentType 覆盖 D2 的声明侧：契约写明响应类型时，
// 即使状态码是 2xx，返回别的类型也算协议错误。
func TestDownloadFileEnforcesDeclaredContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = io.WriteString(w, "not a csv")
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	_, err = client.DownloadFile(context.Background(), c, streamProbeRoute(endpoint.KindDownload, "text/csv"), &streamProbeReq{})
	var protocolErr *client.ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("DownloadFile error = %v, want ProtocolError", err)
	}
	if !strings.Contains(protocolErr.Error(), "text/csv") {
		t.Fatalf("error = %v, want the declared Content-Type in the message", protocolErr)
	}
}

// TestDownloadFileAcceptsWildcardContentType keeps one download route usable
// for several file formats: "*/*" opts out of the exact match.
// TestDownloadFileAcceptsWildcardContentType 让一个下载路由服务多种文件格式：声明
// "*/*" 即退出精确匹配。
func TestDownloadFileAcceptsWildcardContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "a,b\n")
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	download, err := client.DownloadFile(context.Background(), c, streamProbeRoute(endpoint.KindDownload, "*/*"), &streamProbeReq{})
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	defer func() { _ = download.Body.Close() }()
	data, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "a,b\n" {
		t.Fatalf("body = %q", data)
	}
}

// TestStreamChunkedRejectsNonJSONMismatch checks the fallback error shape: a
// declared chunked type that does not match produces ProtocolError, not an
// HTTPError whose status is 200.
// TestStreamChunkedRejectsNonJSONMismatch 检查兜底错误形态：声明为分块流但类型不符时
// 返回 ProtocolError，而不是状态码为 200 的 HTTPError。
func TestStreamChunkedRejectsNonJSONMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "plain body")
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	_, err = client.StreamChunked(context.Background(), c, streamProbeRoute(endpoint.KindChunked, "application/octet-stream"), &streamProbeReq{})
	var protocolErr *client.ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("StreamChunked error = %v (%T), want ProtocolError", err, err)
	}
	if !strings.Contains(protocolErr.Error(), "application/octet-stream") {
		t.Fatalf("error = %v, want the declared Content-Type in the message", protocolErr)
	}
}
