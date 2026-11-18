package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type idempotentWriteReq struct {
	// The key is a contract field here, which is exactly the case the retry
	// decision used to miss: it only looked at CallOptions.
	Key  string `header:"Idempotency-Key"`
	Name string `json:"name"`
}

type idempotentWriteRes struct {
	OK bool `json:"ok"`
}

var idempotentWrite = endpoint.New[idempotentWriteReq, idempotentWriteRes](endpoint.Spec{
	OperationID: "IdempotentWrite",
	Method:      http.MethodPost,
	Path:        "/api/probe/writes",
})

// flakyServer fails the first failures attempts with 503, then succeeds.
func flakyServer(t *testing.T, failures int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"ok":true},"req_id":"r"}`)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestPostRetriesWithStructIdempotencyKey is the regression: a POST whose key is
// declared as a struct field must be retried, because the request that goes out
// carries the header.
// TestPostRetriesWithStructIdempotencyKey 是回归点：幂等键声明成结构体字段的 POST
// 必须重试——发出去的请求确实带了那个 header。
func TestPostRetriesWithStructIdempotencyKey(t *testing.T) {
	server, calls := flakyServer(t, 1)
	c := newFixtureClient(t, server.URL, client.Config{})

	if _, err := client.Call(context.Background(), c, idempotentWrite, &idempotentWriteReq{
		Key:  "key-from-contract",
		Name: "probe",
	}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

// TestPostRetriesWithOptionIdempotencyKey keeps the behaviour that already
// worked: the key may also be supplied at the call site.
// TestPostRetriesWithOptionIdempotencyKey 保留原有能力：幂等键也可以由调用点给出。
func TestPostRetriesWithOptionIdempotencyKey(t *testing.T) {
	server, calls := flakyServer(t, 1)
	c := newFixtureClient(t, server.URL, client.Config{})

	if _, err := client.Call(context.Background(), c, idempotentWrite, &idempotentWriteReq{Name: "probe"},
		client.WithHeader(client.IdempotencyKeyHeader, "key-from-option")); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

// TestPostWithoutIdempotencyKeyIsNotRetried is the guard the other two rely on: a
// write with no key must fail fast instead of risking a duplicate.
// TestPostWithoutIdempotencyKeyIsNotRetried 是前两个测试依赖的保护：没有键的写请求
// 必须立刻失败，而不是冒着重复写的风险重试。
func TestPostWithoutIdempotencyKeyIsNotRetried(t *testing.T) {
	server, calls := flakyServer(t, 1)
	c := newFixtureClient(t, server.URL, client.Config{})

	if _, err := client.Call(context.Background(), c, idempotentWrite, &idempotentWriteReq{Name: "probe"}); err == nil {
		t.Fatal("Call must surface the 503")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

// TestEmptyIdempotencyKeyIsNotAKey pins the value rule: an empty key says nothing,
// so the request is not replayable.
// TestEmptyIdempotencyKeyIsNotAKey 钉住取值规则：空的键什么也没说，请求不可重放。
func TestEmptyIdempotencyKeyIsNotAKey(t *testing.T) {
	server, calls := flakyServer(t, 1)
	c := newFixtureClient(t, server.URL, client.Config{})

	if _, err := client.Call(context.Background(), c, idempotentWrite, &idempotentWriteReq{Name: "probe"},
		client.WithHeader(client.IdempotencyKeyHeader, "   ")); err == nil {
		t.Fatal("Call must surface the 503")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 for a blank key", got)
	}
}
