package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

// TestWithQueryAppendsParameters is the regression test for the nil query map:
// WithQuery used to call url.Values.Add on a nil map and panic before any
// request was sent. Repeated keys must accumulate, not overwrite.
// TestWithQueryAppendsParameters 是 nil query map 的回归测试：WithQuery 曾在发出
// 任何请求之前就对 nil map 调用 url.Values.Add 而 panic。同名参数必须累加而不是覆盖。
func TestWithQueryAppendsParameters(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"auth_required":false},"req_id":"q1"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.GetConfig, &testcontract.ConfigRequest{},
		client.WithQuery("tag", "a"),
		client.WithQuery("tag", "b"),
		client.WithQuery("page", "2"),
	); err != nil {
		t.Fatalf("Call: %v", err)
	}

	if values := got["tag"]; len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("tag = %v, want [a b]", values)
	}
	if page := got.Get("page"); page != "2" {
		t.Fatalf("page = %q, want 2", page)
	}
}

// TestCallWithoutQueryKeepsURLClean checks the other half of the constructor
// fix: a normal call must not gain an empty "?" just because the map exists.
// TestCallWithoutQueryKeepsURLClean 校验构造函数修复的另一半：普通调用不能因为 map
// 存在就多出一个空的 "?"。
func TestCallWithoutQueryKeepsURLClean(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"auth_required":false},"req_id":"q2"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.GetConfig, &testcontract.ConfigRequest{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if rawQuery != "" {
		t.Fatalf("RawQuery = %q, want empty", rawQuery)
	}
}

// TestWithQueryOnStreamingCall covers the shared option on the streaming path:
// StreamSSE goes through the same newCallOptions, so it had the same panic.
// TestWithQueryOnStreamingCall 覆盖流式路径上的同一个选项：StreamSSE 走的是同一个
// newCallOptions，因此也有同一个 panic。
func TestWithQueryOnStreamingCall(t *testing.T) {
	received := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Query()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	route := endpoint.New[testcontract.ConfigRequest, endpoint.NoBody](endpoint.Spec{
		OperationID:  "StreamConfig",
		Method:       http.MethodGet,
		Path:         "/api/test/config/stream",
		Summary:      "stream config",
		ResponseKind: endpoint.KindSSE,
	})
	c := newFixtureClient(t, server.URL, client.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs, err := client.StreamSSE(ctx, c, route, &testcontract.ConfigRequest{},
		client.WithQuery("filter", "paid"),
	)
	if err != nil {
		t.Fatalf("StreamSSE: %v", err)
	}

	select {
	case query := <-received:
		if filter := query.Get("filter"); filter != "paid" {
			t.Fatalf("filter = %q, want paid", filter)
		}
	case streamErr := <-errs:
		t.Fatalf("stream error: %v", streamErr)
	case <-time.After(5 * time.Second):
		t.Fatal("server never received the streaming request")
	}
	cancel()
	for range events {
	}
}
