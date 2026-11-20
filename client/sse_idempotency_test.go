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
)

type sseWriteReq struct {
	// The key is a contract field, so retryFor must see it through the struct
	// tag just like it does for an ordinary POST.
	// 幂等键声明在契约字段上：retryFor 必须像普通 POST 一样通过结构体标签看到它。
	Key string `header:"Idempotency-Key"`
	ID  int    `json:"id"`
}

var sseWriteRoute = endpoint.New[sseWriteReq, endpoint.NoBody](endpoint.Spec{
	OperationID:  "StreamWrite",
	Method:       http.MethodPost,
	Path:         "/api/test/stream-write",
	ResponseKind: endpoint.KindSSE,
})

// abortedWriteStream sends one event, flushes it, then aborts the connection so
// StreamSSE sees a disconnect. hits counts how many times the POST is sent.
// abortedWriteStream 发一个事件并 flush，然后中断连接，让 StreamSSE 看到一次断连；
// hits 记录 POST 实际发出几次。
func abortedWriteStream(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte("id: 1\ndata: first\n\n"))
		}
		// Flush headers on every request so a buggy reconnect still gets a valid
		// 200 response; replays carry no event so the test does not block on an
		// undrained event channel before seeing the disconnect.
		// 每个请求都 flush 响应头，让错误重连也能拿到合法的 200 响应；重放请求不带
		// 事件，避免测试卡在没人读的事件 channel 上，从而干净地观察断连。
		// Abort, not a clean return: a clean EOF also reconnects, but aborting
		// makes the disconnect unambiguous.
		// 用中断而不是正常返回：正常 EOF 也会触发重连，但中断让断连语义更明确。
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// TestStreamSSENoReconnectForPostWithoutIdempotencyKey is the P2-04 regression.
// A POST stream has already reached the server and may have consumed events;
// without a key the client must not replay it on disconnect, even though Retry
// is positive. Old code reconnected on every method and sent the POST again.
// TestStreamSSENoReconnectForPostWithoutIdempotencyKey 是 P2-04 的回归点。POST 流
// 已经到达服务端、事件也可能已被消费：没有幂等键时，即便 Retry 大于零，断连后也
// 不得重放。旧代码对所有方法一概重连，会把 POST 再发一次。
func TestStreamSSENoReconnectForPostWithoutIdempotencyKey(t *testing.T) {
	server, hits := abortedWriteStream(t)
	c := newFixtureClient(t, server.URL, client.Config{Retry: 3})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, sseWriteRoute, &sseWriteReq{ID: 1})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("stream closed before the first event; err = %v", <-errs)
		}
		if string(ev.Data) != "first" {
			t.Fatalf("event = %q, want first", ev.Data)
		}
	case err := <-errs:
		t.Fatalf("stream error before first event: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for the first event")
	}

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("want a disconnect error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the disconnect error")
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("POST sent %d times, want 1 (no replay without idempotency key)", got)
	}
}

// TestStreamSSEReconnectsForPostWithIdempotencyKey guards against over-correcting
// P2-04: a keyed write is replayable just like an ordinary keyed POST, so the
// stream may reconnect with Last-Event-ID after a drop. The key must be present
// on both the initial request and the replay.
// TestStreamSSEReconnectsForPostWithIdempotencyKey 防止 P2-04 被过度修复：带幂等键的
// 写请求和普通带键 POST 一样可重放，因此断连后允许带 Last-Event-ID 重连；首请求与
// 重放请求都必须带上幂等键。
func TestStreamSSEReconnectsForPostWithIdempotencyKey(t *testing.T) {
	var hits atomic.Int32
	var lastKey atomic.Value
	var lastEventID atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		lastKey.Store(r.Header.Get(client.IdempotencyKeyHeader))
		lastEventID.Store(r.Header.Get("Last-Event-ID"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			_, _ = w.Write([]byte("id: 1\ndata: first\n\n"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write([]byte("id: 2\ndata: second\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{Retry: 3})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, sseWriteRoute, &sseWriteReq{ID: 1, Key: "key-from-contract"})
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	for want, data := range []string{"first", "second"} {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed after %d events; err = %v", want, <-errs)
			}
			if string(ev.Data) != data {
				t.Fatalf("event %d = %q, want %q", want, ev.Data, data)
			}
		case err := <-errs:
			t.Fatalf("stream error after %d events: %v", want, err)
		case <-ctx.Done():
			t.Fatalf("timed out after %d events", want)
		}
	}

	if got := hits.Load(); got < 2 {
		t.Fatalf("POST sent %d times, want at least 2 (reconnect with a key)", got)
	}
	if got, _ := lastKey.Load().(string); got != "key-from-contract" {
		t.Fatalf("Idempotency-Key on replay = %q, want key-from-contract", got)
	}
	if got, _ := lastEventID.Load().(string); got != "1" {
		t.Fatalf("Last-Event-ID on replay = %q, want 1", got)
	}
}
