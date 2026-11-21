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

// TestStreamSSEKeepsHeartbeatsOutOfTheEventBudget pins the reset point: a
// comment-only block has nothing to emit, but the bytes it accumulated must not
// carry into the next block. A server that sends ": ping" forever would
// otherwise reach MaxEventBytes and the client would report ErrEventTooLarge on
// a perfectly healthy stream.
// TestStreamSSEKeepsHeartbeatsOutOfTheEventBudget 固定重置时机：仅含注释的块没有内容
// 要发出，但它累计的字节不能带进下一个块。否则一个永远发 ": ping" 的服务端会撞上
// MaxEventBytes，客户端对一条完全健康的流报 ErrEventTooLarge。
func TestStreamSSEKeepsHeartbeatsOutOfTheEventBudget(t *testing.T) {
	events, errs, cancel := openLimitedStream(t, 64, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// 30 × 6 bytes: well past the 64-byte cap if heartbeats accumulate.
		// 30 × 6 字节：心跳若累计，会远超 64 字节上限。
		for i := 0; i < 30; i++ {
			_, _ = w.Write([]byte(": ping\n\n"))
		}
		_, _ = w.Write([]byte("data: ok\n\n"))
		w.(http.Flusher).Flush()
		<-time.After(300 * time.Millisecond)
	})
	defer cancel()

	select {
	case ev := <-events:
		if string(ev.Data) != "ok" {
			t.Fatalf("event = %+v, want data=ok", ev)
		}
	case err := <-errs:
		t.Fatalf("heartbeats consumed the event budget: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived")
	}
}

// TestStreamSSEAcceptsLineAtTheExactLimit pins the shared definition of the
// cap: the event-level accumulator counts content only, so the per-line guard
// must not count the terminator. A line whose content is exactly
// MaxEventBytes used to be rejected by readLine before dispatch ever saw it.
// TestStreamSSEAcceptsLineAtTheExactLimit 固定上限的同一口径：事件级累加只算内容，
// 单行守卫不能把行终止符算进去。内容恰好等于 MaxEventBytes 的行过去会在 readLine
// 就被拒掉，dispatch 根本看不到它。
func TestStreamSSEAcceptsLineAtTheExactLimit(t *testing.T) {
	const limit = 64
	prefix := "data: "
	payload := strings.Repeat("a", limit-len(prefix))
	for _, tc := range []struct{ name, ending string }{
		{"lf", "\n"},
		{"crlf", "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, errs, cancel := openLimitedStream(t, limit, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(prefix + payload + tc.ending + tc.ending))
				w.(http.Flusher).Flush()
				<-time.After(300 * time.Millisecond)
			})
			defer cancel()

			select {
			case ev := <-events:
				if string(ev.Data) != payload {
					t.Fatalf("event data = %q, want %q", ev.Data, payload)
				}
			case err := <-errs:
				t.Fatalf("line at the exact limit was rejected: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("no event arrived")
			}
		})
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
