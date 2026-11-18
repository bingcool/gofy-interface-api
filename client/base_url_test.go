package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

// TestBaseURLQueryIsMergedNotOverwritten pins D7's query half: a base URL that
// carries a query ("?token=x") must keep it, and the route path must land in the
// path instead of being appended to the query value. Request parameters are
// merged on top rather than replacing the base ones.
// TestBaseURLQueryIsMergedNotOverwritten 固定 D7 的 query 一侧：带 query 的 base
// （"?token=x"）必须保留它，路由路径要落在 path 上而不是被追加进 query 值；请求参数
// 叠加在 base 参数之上，而不是把它们替换掉。
func TestBaseURLQueryIsMergedNotOverwritten(t *testing.T) {
	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"auth_required":false},"req_id":"b1"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL+"?token=abc", client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.GetConfig, &testcontract.ConfigRequest{},
		client.WithQuery("page", "2"),
	); err != nil {
		t.Fatalf("Call: %v", err)
	}

	if gotPath != "/api/test/config" {
		t.Fatalf("path = %q, want /api/test/config", gotPath)
	}
	query, err := url.ParseQuery(gotQuery)
	if err != nil {
		t.Fatalf("parse RawQuery %q: %v", gotQuery, err)
	}
	if token := query.Get("token"); token != "abc" {
		t.Fatalf("token = %q, want abc (base query must survive)", token)
	}
	if page := query.Get("page"); page != "2" {
		t.Fatalf("page = %q, want 2 (request query must merge in)", page)
	}
}

// TestBaseURLPathPrefixIsPreserved pins the path half: a base with a path
// prefix ("/v2") keeps it in front of the route path instead of dropping it.
// TestBaseURLPathPrefixIsPreserved 固定 path 一侧：带路径前缀的 base（"/v2"）会把前缀
// 保留在路由路径之前，而不会丢弃它。
func TestBaseURLPathPrefixIsPreserved(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"item":{"id":7}},"req_id":"b2"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL+"/v2", client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 7}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotPath != "/v2/api/test/items/7" {
		t.Fatalf("path = %q, want /v2/api/test/items/7", gotPath)
	}
}

type slugRequest struct {
	Slug string `uri:"id"`
}

// TestEscapedPathParamIsNotDoubleEscaped guards the path join itself: uri
// parameters arrive url.PathEscape'd, so the join happens on the escaped form.
// Re-parsing the joined text as a plain path would turn "a%2Fb" into "a%252Fb"
// and route the request to the wrong resource.
// TestEscapedPathParamIsNotDoubleEscaped 守住路径拼接本身：uri 参数到达时已被
// url.PathEscape 转义，因此拼接发生在转义形态上。若把拼接结果当普通路径重新解析，
// "a%2Fb" 会变成 "a%252Fb"，请求就被路由到错误的资源。
func TestEscapedPathParamIsNotDoubleEscaped(t *testing.T) {
	var gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{},"req_id":"b3"}`)
	}))
	defer server.Close()

	route := endpoint.New[slugRequest, endpoint.NoBody](endpoint.Spec{
		OperationID: "GetSlug",
		Method:      http.MethodGet,
		Path:        "/api/test/slugs/{id}",
	})
	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, route, &slugRequest{Slug: "a/b"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotURI != "/api/test/slugs/a%2Fb" {
		t.Fatalf("RequestURI = %q, want /api/test/slugs/a%%2Fb", gotURI)
	}
}
