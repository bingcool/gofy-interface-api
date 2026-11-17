package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

// streamRoute is the fixture route StreamSSE is called with. A stream has no
// response body type, so the second type parameter is endpoint.NoBody.
// streamRoute 是 StreamSSE 使用的 fixture 路由。流没有响应体类型，因此第二个类型
// 参数是 endpoint.NoBody。
var streamRoute = endpoint.New[testcontract.ItemRequest, endpoint.NoBody](endpoint.Spec{
	OperationID:  "StreamItem",
	Method:       http.MethodGet,
	Path:         "/api/test/stream/{id}",
	Summary:      "stream one item",
	Tags:         []string{testcontract.ServiceName},
	ResponseKind: endpoint.KindSSE,
})

// flipResolver returns the first address once, then the second forever after —
// the shape of a Resolver whose instance list changed while a stream was open.
// flipResolver 第一次返回第一个地址，之后一直返回第二个 —— 也就是流开着的时候
// 实例列表发生了变化。
type flipResolver struct {
	first  string
	second string
	calls  atomic.Int32
}

func (r *flipResolver) Resolve(context.Context, string) (string, error) {
	if r.calls.Add(1) == 1 {
		return r.first, nil
	}
	return r.second, nil
}

// TestStreamSSEReResolvesOnReconnect is the P2-02 acceptance test: the initial
// connection goes to A, A drops the connection after one event, and the
// reconnect must resolve again (landing on B) instead of retrying the dead
// address. The Last-Event-ID has to survive the switch, otherwise the new
// instance replays from the start.
// TestStreamSSEReResolvesOnReconnect 是 P2-02 的验收测试：首个连接打到 A，A 在发出
// 一个事件后断开，重连必须重新解析（落到 B），而不是对着死地址重试。Last-Event-ID
// 必须跨过这次切换，否则新实例会从头重放。
func TestStreamSSEReResolvesOnReconnect(t *testing.T) {
	var dropped atomic.Int32
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: 1\ndata: first\n\n"))
		w.(http.Flusher).Flush()
		dropped.Add(1)
		// Abort the connection instead of returning: a clean return reads as
		// EOF, which ends the stream without a reconnect.
		// 中断连接而不是正常返回：正常返回读到的是 EOF，流会直接结束而不重连。
		panic(http.ErrAbortHandler)
	}))
	defer serverA.Close()

	var reachedB atomic.Bool
	var lastEventID atomic.Value
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedB.Store(true)
		lastEventID.Store(r.Header.Get("Last-Event-ID"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: 2\ndata: second\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer serverB.Close()

	resolver := &flipResolver{first: serverA.URL, second: serverB.URL}
	c, err := client.New(client.Config{
		Service:  testcontract.ServiceName,
		Resolver: resolver,
		Retry:    3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, streamRoute, &testcontract.ItemRequest{ID: 1})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	var seen []string
	for len(seen) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed after %v; err = %v", seen, <-errs)
			}
			seen = append(seen, string(ev.Data))
		case err := <-errs:
			t.Fatalf("stream error after %v: %v", seen, err)
		case <-ctx.Done():
			t.Fatalf("timed out after %v", seen)
		}
	}

	if seen[0] != "first" || seen[1] != "second" {
		t.Fatalf("events = %v, want [first second]", seen)
	}
	if dropped.Load() != 1 {
		t.Fatalf("server A handled %d connections, want 1", dropped.Load())
	}
	if !reachedB.Load() {
		t.Fatal("reconnect did not reach the second instance")
	}
	if got, _ := lastEventID.Load().(string); got != "1" {
		t.Fatalf("Last-Event-ID on reconnect = %q, want 1", got)
	}
	if got := resolver.calls.Load(); got < 2 {
		t.Fatalf("Resolve calls = %d, want at least 2 (initial + reconnect)", got)
	}
}

// TestStreamSSEFixedBaseURLReconnectsToIt pinpoints the other half of P2-02: a
// fixed BaseURL must keep working, because base() returns it unchanged.
// TestStreamSSEFixedBaseURLReconnectsToIt 覆盖 P2-02 的另一半：固定 BaseURL 必须
// 继续可用，因为 base() 原样返回它。
func TestStreamSSEFixedBaseURLReconnectsToIt(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte("data: one\n\n"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write([]byte("data: two\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	c, err := client.New(client.Config{BaseURL: server.URL, Retry: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, streamRoute, &testcontract.ItemRequest{ID: 1})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}
	for want, data := range []string{"one", "two"} {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed early; err = %v", <-errs)
			}
			if string(ev.Data) != data {
				t.Fatalf("event %d = %q, want %q", want, ev.Data, data)
			}
		case err := <-errs:
			t.Fatalf("stream error: %v", err)
		case <-ctx.Done():
			t.Fatal("timed out")
		}
	}
	if hits.Load() < 2 {
		t.Fatalf("server handled %d connections, want at least 2", hits.Load())
	}
}
