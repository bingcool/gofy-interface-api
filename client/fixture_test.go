// Package client_test verifies the typed client using only library fixtures.
// Real-contract client tests live in the project module (§4.7).
// 包 client_test 只用库内 fixture 验证类型化客户端；真实契约测试归项目模块（§4.7）。
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
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

func newFixtureClient(t *testing.T, addr string, cfg client.Config) *client.Client {
	t.Helper()
	cfg.BaseURL = addr
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestCallFixtureRouteSuccess(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"item":{"id":7,"name":"item"}},"req_id":"r1"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{
		TokenSource: func(context.Context) (string, error) { return "token", nil },
	})
	res, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 7})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if res.Item == nil || res.Item.ID != 7 {
		t.Fatalf("response = %+v", res)
	}
	if gotPath != "/api/test/items/7" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestCallFixtureRouteAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":40401,"msg":"item not found","req_id":"r2"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	_, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 7})
	if err == nil {
		t.Fatal("Call must return an API error")
	}
	if got := client.CodeOf(err); got != testcontract.ErrCodeNotFound {
		t.Fatalf("CodeOf = %d, want %d", got, testcontract.ErrCodeNotFound)
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("APIError = %+v", apiErr)
	}
}

func TestGetRetriesServiceUnavailable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","data":{"item":{"id":9}}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{Retry: 2})
	res, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 9})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if res.Item == nil || res.Item.ID != 9 || calls.Load() != 3 {
		t.Fatalf("res = %+v, calls = %d", res, calls.Load())
	}
}

func TestProtocolErrorOnInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	_, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 1})
	var protocolErr *client.ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("err = %v, want ProtocolError", err)
	}
}

func TestTimeoutAndOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = io.WriteString(w, `{"code":0,"data":{"item":{"id":1}}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{Timeout: 20 * time.Millisecond})
	if _, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 1}); err == nil {
		t.Fatal("Call must time out")
	}
	longer := newFixtureClient(t, server.URL, client.Config{Timeout: 20 * time.Millisecond})
	if _, err := client.Call(context.Background(), longer, testcontract.GetItem,
		&testcontract.ItemRequest{ID: 1}, client.WithTimeout(time.Second)); err != nil {
		t.Fatalf("WithTimeout override: %v", err)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"item":{"name":"`+strings.Repeat("x", 4096)+`"}}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{MaxResponseBytes: 128})
	if _, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 1}); err == nil {
		t.Fatal("Call must reject an oversized response")
	}
}

func TestNewRequiresBaseURLOrResolver(t *testing.T) {
	if _, err := client.New(client.Config{}); err == nil {
		t.Fatal("New must reject a config without BaseURL or Resolver")
	}
	if _, err := client.New(client.Config{BaseURL: "ftp://host"}); err == nil {
		t.Fatal("New must reject a non-http scheme")
	}
	if _, err := client.New(client.Config{BaseURL: "http://host"}); err != nil {
		t.Fatalf("New: %v", err)
	}
}

type countingResolver struct {
	base  string
	calls atomic.Int32
}

func (r *countingResolver) Resolve(context.Context, string) (string, error) {
	r.calls.Add(1)
	return r.base, nil
}

func TestResolverIsUsedOnEveryCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"item":{"id":3}}}`)
	}))
	defer server.Close()

	resolver := &countingResolver{base: server.URL}
	c, err := client.New(client.Config{Service: testcontract.ServiceName, Resolver: resolver})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 3}); err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("Resolve calls = %d, want 2", got)
	}
}

func TestCallForwardsPropagatedHeaders(t *testing.T) {
	var tenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant = r.Header.Get("X-Tenant-Id")
		_, _ = io.WriteString(w, `{"code":0,"data":{"item":{"id":1}}}`)
	}))
	defer server.Close()

	ctx := client.ContextWithHeader(context.Background(), "X-Tenant-Id", "tenant-a")
	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(ctx, c, testcontract.GetItem, &testcontract.ItemRequest{ID: 1}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if tenant != "tenant-a" {
		t.Fatalf("X-Tenant-Id = %q", tenant)
	}
}

// A grouped contract keeps the deployment prefix on the group, not in
// Spec.Path, so the caller supplies it per call. The prefix has to compose with
// path parameters and tolerate a missing leading slash or a trailing one.
// 分组契约把部署前缀放在分组上而不是 Spec.Path 里，因此调用方按次给出。前缀必须与
// 路径参数组合，并容忍缺少前导斜杠或多余尾斜杠。
func TestWithPathPrefixComposesWithPathParameters(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "absolute", prefix: "/api/v1/order", want: "/api/v1/order/api/test/items/7"},
		{name: "no leading slash", prefix: "api/v1/order", want: "/api/v1/order/api/test/items/7"},
		{name: "trailing slash", prefix: "/api/v1/order/", want: "/api/v1/order/api/test/items/7"},
		{name: "empty prefix", prefix: "", want: "/api/test/items/7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"item":{"id":7,"name":"item"}}}`)
			}))
			defer server.Close()

			c := newFixtureClient(t, server.URL, client.Config{})
			_, err := client.Call(context.Background(), c, testcontract.GetItem,
				&testcontract.ItemRequest{ID: 7}, client.WithPathPrefix(tc.prefix))
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			if gotPath != tc.want {
				t.Fatalf("path = %q, want %q", gotPath, tc.want)
			}
		})
	}
}
