package client

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TestDerivedClientCacheIsBounded is the P2-03 acceptance test: WithConnectTimeout
// derives a client per distinct duration, and an unbounded cache would keep one
// transport — and its connection pool — per value for the life of the process.
// Entries beyond the cap still work, they just do not get kept.
// TestDerivedClientCacheIsBounded 是 P2-03 的验收测试：WithConnectTimeout 会为每个
// 不同的 duration 派生一个 Client，无界缓存会让每个取值都长期占着一个 transport 与
// 连接池。超过上限的条目照常可用，只是不被保留。
func TestDerivedClientCacheIsBounded(t *testing.T) {
	c, err := New(Config{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 1; i <= maxDerivedClients+8; i++ {
		hc, err := c.httpClientFor(callOptions{connectTimeout: time.Duration(i) * time.Millisecond})
		if err != nil {
			t.Fatalf("httpClientFor(%dms): %v", i, err)
		}
		if hc == nil {
			t.Fatalf("httpClientFor(%dms) returned nil", i)
		}
	}

	c.callMu.Lock()
	cached := len(c.callClients)
	c.callMu.Unlock()
	if cached != maxDerivedClients {
		t.Fatalf("cached clients = %d, want %d", cached, maxDerivedClients)
	}

	// A value that was stored must come back as the same client, otherwise the
	// cap silently broke pooling for the settings that do fit.
	// 被缓存的取值必须返回同一个 client，否则上限就悄悄破坏了本该复用的连接池。
	first, err := c.httpClientFor(callOptions{connectTimeout: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("httpClientFor: %v", err)
	}
	again, err := c.httpClientFor(callOptions{connectTimeout: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("httpClientFor: %v", err)
	}
	if first != again {
		t.Fatal("a cached connect timeout produced a different client")
	}
}

// TestCloseDrainsDerivedClients covers the lifecycle half of P2-03: Close drops
// the cache and closes idle connections, and calling it twice is safe.
// TestCloseDrainsDerivedClients 覆盖 P2-03 的生命周期部分：Close 清空缓存并关闭空闲
// 连接，重复调用安全。
func TestCloseDrainsDerivedClients(t *testing.T) {
	c, err := New(Config{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.ownHTTPClient {
		t.Fatal("a Client built without Config.HTTPClient must own its http.Client")
	}
	if _, err := c.httpClientFor(callOptions{connectTimeout: time.Second}); err != nil {
		t.Fatalf("httpClientFor: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	c.callMu.Lock()
	cached := len(c.callClients)
	c.callMu.Unlock()
	if cached != 0 {
		t.Fatalf("cached clients after Close = %d, want 0", cached)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// recordingTransport counts CloseIdleConnections calls, which is the only
// observable effect Close has on a client.
// recordingTransport 统计 CloseIdleConnections 的调用次数，这是 Close 对 client
// 唯一可观察的影响。
type recordingTransport struct {
	base  http.RoundTripper
	calls atomic.Int32
}

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r)
}

func (t *recordingTransport) CloseIdleConnections() { t.calls.Add(1) }

// TestCloseLeavesCallerHTTPClientAlone makes the ownership boundary explicit: a
// client supplied through Config is still in use elsewhere, so Close must not
// release its idle connections.
// TestCloseLeavesCallerHTTPClientAlone 把所有权边界写清楚：通过 Config 传入的
// client 在别处仍在使用，Close 不得释放它的空闲连接。
func TestCloseLeavesCallerHTTPClientAlone(t *testing.T) {
	tr := &recordingTransport{base: http.DefaultTransport}
	supplied := &http.Client{Transport: tr}
	c, err := New(Config{BaseURL: "http://127.0.0.1:1", HTTPClient: supplied})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.ownHTTPClient {
		t.Fatal("a supplied HTTPClient must not be treated as owned")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if c.httpClient != supplied {
		t.Fatal("Close replaced the supplied client")
	}
	if got := tr.calls.Load(); got != 0 {
		t.Fatalf("Close called CloseIdleConnections on the caller's transport %d times, want 0", got)
	}
}
