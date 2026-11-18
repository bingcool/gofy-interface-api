package client

import (
	"context"
	"testing"
	"time"
)

// TestNoTimeoutDisablesTheDeadline pins D6's timeout half: NoTimeout must not
// degrade into the default; callContext then leaves the deadline to the caller.
// TestNoTimeoutDisablesTheDeadline 固定 D6 的超时一侧：NoTimeout 不能退化成默认值，
// callContext 应把 deadline 留给调用方。
func TestNoTimeoutDisablesTheDeadline(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid", Timeout: NoTimeout})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := c.callContext(context.Background(), newCallOptions(nil))
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		t.Fatalf("call context has deadline %s, want none", deadline)
	}
}

// TestNoRetryDisablesRetries pins the retry half: NoRetry yields zero retries,
// while the zero value still means the default.
// TestNoRetryDisablesRetries 固定重试一侧：NoRetry 得到零次重试，零值仍然表示默认值。
func TestNoRetryDisablesRetries(t *testing.T) {
	off, err := New(Config{BaseURL: "http://example.invalid", Retry: NoRetry})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if off.retry != 0 {
		t.Fatalf("retry = %d, want 0", off.retry)
	}
	def, err := New(Config{BaseURL: "http://example.invalid"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if def.retry != defaultRetry {
		t.Fatalf("default retry = %d, want %d", def.retry, defaultRetry)
	}
}

// TestRejectsMeaninglessNegatives keeps the negative space honest: only the
// exported sentinels are negative, and anything else fails at construction.
// TestRejectsMeaninglessNegatives 收紧负值空间：只有导出的哨兵可以是负数，其余负值
// 在构造期报错。
func TestRejectsMeaninglessNegatives(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://example.invalid", Timeout: -2}); err == nil {
		t.Fatal("New accepted Timeout=-2")
	}
	if _, err := New(Config{BaseURL: "http://example.invalid", Retry: -2}); err == nil {
		t.Fatal("New accepted Retry=-2")
	}
}

// TestConnectTimeoutCacheStaysBounded covers D8: past the cache limit the new
// pool replaces an entry instead of being handed out unmanaged.
// TestConnectTimeoutCacheStaysBounded 覆盖 D8：超过缓存上限后，新连接池顶替一条旧
// 条目，而不是作为无人管理的池被交出。
func TestConnectTimeoutCacheStaysBounded(t *testing.T) {
	c, err := New(Config{BaseURL: "http://example.invalid"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	last := time.Duration(maxDerivedClients+1) * time.Millisecond
	for i := 1; i <= maxDerivedClients+1; i++ {
		if _, err := c.httpClientFor(callOptions{connectTimeout: time.Duration(i) * time.Millisecond}); err != nil {
			t.Fatalf("httpClientFor: %v", err)
		}
	}
	c.callMu.Lock()
	size := len(c.callClients)
	_, newestCached := c.callClients[last]
	c.callMu.Unlock()
	if size != maxDerivedClients {
		t.Fatalf("cache holds %d entries, want %d", size, maxDerivedClients)
	}
	if !newestCached {
		t.Fatal("the newest connect timeout was not stored")
	}
}
