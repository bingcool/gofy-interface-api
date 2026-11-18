package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

// openLimitedStream starts StreamSSE against a handler with a small event cap.
func openLimitedStream(t *testing.T, maxEvent int64, handler http.HandlerFunc) (<-chan client.Event, <-chan error, context.CancelFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := newFixtureClient(t, server.URL, client.Config{MaxEventBytes: maxEvent})
	ctx, cancel := context.WithCancel(context.Background())
	events, errs, err := client.StreamSSE(ctx, c, streamRoute, &testcontract.ItemRequest{ID: 1})
	if err != nil {
		cancel()
		t.Fatalf("StreamSSE: %v", err)
	}
	return events, errs, cancel
}

// TestStreamSSERejectsOversizedLine is the memory guard: a server that sends
// bytes without ever ending the line must not be able to grow the client's
// buffer without bound.
// TestStreamSSERejectsOversizedLine 是内存保护：服务端一直不发换行时，不能让它无限
// 撑大客户端的缓冲区。
func TestStreamSSERejectsOversizedLine(t *testing.T) {
	_, errs, cancel := openLimitedStream(t, 1024, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + strings.Repeat("a", 4096)))
		w.(http.Flusher).Flush()
	})
	defer cancel()

	select {
	case err := <-errs:
		if !errors.Is(err, client.ErrEventTooLarge) {
			t.Fatalf("error = %v, want ErrEventTooLarge", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no error: an oversized line was buffered without a limit")
	}
}

// TestStreamSSERejectsOversizedEvent covers the second half: each line stays
// under the cap while the event never ends, so only the accumulated size can
// catch it.
// TestStreamSSERejectsOversizedEvent 覆盖另一半：每行都在上限内，但事件永不结束，
// 只有累计大小能拦住它。
func TestStreamSSERejectsOversizedEvent(t *testing.T) {
	_, errs, cancel := openLimitedStream(t, 1024, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		line := "data: " + strings.Repeat("b", 100) + "\n"
		for i := 0; i < 32; i++ { // 32 × ~107 bytes, no dispatching blank line
			_, _ = w.Write([]byte(line))
		}
		w.(http.Flusher).Flush()
	})
	defer cancel()

	select {
	case err := <-errs:
		if !errors.Is(err, client.ErrEventTooLarge) {
			t.Fatalf("error = %v, want ErrEventTooLarge", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no error: an unbounded event was accumulated")
	}
}

// TestStreamSSEAcceptsEventWithinLimit keeps the cap honest: an event below the
// limit still arrives intact, so the guard cannot be "reject everything".
// TestStreamSSEAcceptsEventWithinLimit 保证上限没有误伤：小于上限的事件照常完整
// 到达，保护不能变成"什么都拒绝"。
func TestStreamSSEAcceptsEventWithinLimit(t *testing.T) {
	events, errs, cancel := openLimitedStream(t, 1024, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id: 7\nevent: message\ndata: hello\ndata: world\n\n"))
		w.(http.Flusher).Flush()
		<-time.After(300 * time.Millisecond) // keep the stream open so the test reads it
	})
	defer cancel()

	select {
	case ev := <-events:
		if ev.ID != "7" || ev.Event != "message" || string(ev.Data) != "hello\nworld" {
			t.Fatalf("event = %+v", ev)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived")
	}
}

// TestNewRejectsNegativeMaxEventBytes keeps the config rule consistent with the
// other size limits.
// TestNewRejectsNegativeMaxEventBytes 让配置规则与其他大小上限保持一致。
func TestNewRejectsNegativeMaxEventBytes(t *testing.T) {
	if _, err := client.New(client.Config{BaseURL: "http://127.0.0.1:1", MaxEventBytes: -1}); err == nil {
		t.Fatal("New must reject a negative MaxEventBytes")
	}
}
