package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type zeroQueryReq struct {
	// Pointers are how a contract says "optional but meaningful when zero":
	// false and 0 are values here, not absences.
	IncludeDeleted *bool  `form:"include_deleted"`
	Page           *int   `form:"page"`
	Absent         *int   `form:"absent"`
	Name           string `form:"name,omitempty"`
}

type zeroProbeRes struct {
	OK bool `json:"ok"`
}

var zeroQueryRoute = endpoint.New[zeroQueryReq, zeroProbeRes](endpoint.Spec{
	OperationID: "ZeroQuery",
	Method:      http.MethodGet,
	Path:        "/api/probe/zero",
})

type zeroPathReq struct {
	ID   int  `uri:"id"`
	Flag bool `uri:"flag"`
}

var zeroPathRoute = endpoint.New[zeroPathReq, zeroProbeRes](endpoint.Spec{
	OperationID: "ZeroPath",
	Method:      http.MethodGet,
	Path:        "/api/probe/items/{id}/{flag}",
})

type nilPathReq struct {
	ID *int `uri:"id"`
}

var nilPathRoute = endpoint.New[nilPathReq, zeroProbeRes](endpoint.Spec{
	OperationID: "NilPath",
	Method:      http.MethodGet,
	Path:        "/api/probe/items/{id}",
})

func newProbeServer(t *testing.T, seen func(*http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			seen(r)
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"ok":true},"req_id":"r"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestExplicitZeroPointersAreEncoded pins the difference between "explicitly
// zero" and "not provided": a non-nil pointer must reach the wire even when it
// points at the zero value, and a nil pointer must still be omitted.
// TestExplicitZeroPointersAreEncoded 钉住"显式零值"与"没提供"的区别：非 nil 指针
// 即使指向零值也必须出现在请求里，nil 指针则仍然省略。
func TestExplicitZeroPointersAreEncoded(t *testing.T) {
	var query string
	server := newProbeServer(t, func(r *http.Request) { query = r.URL.RawQuery })
	c := newFixtureClient(t, server.URL, client.Config{})

	no, zero := false, 0
	_, err := client.Call(context.Background(), c, zeroQueryRoute, &zeroQueryReq{
		IncludeDeleted: &no,
		Page:           &zero,
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("parse query %q: %v", query, err)
	}
	if got := values.Get("include_deleted"); got != "false" {
		t.Fatalf("include_deleted = %q, want false (query=%q)", got, query)
	}
	if got := values.Get("page"); got != "0" {
		t.Fatalf("page = %q, want 0 (query=%q)", got, query)
	}
	if _, ok := values["absent"]; ok {
		t.Fatalf("nil pointer must stay omitted (query=%q)", query)
	}
}

// TestZeroPathParametersAreValues is the same rule for path parameters, where a
// rejected zero used to surface as the misleading "path parameter is missing".
// TestZeroPathParametersAreValues 是路径参数上的同一条规则：过去零值会被判成
// "path parameter is missing"，而它其实是一个合法取值。
func TestZeroPathParametersAreValues(t *testing.T) {
	var path string
	server := newProbeServer(t, func(r *http.Request) { path = r.URL.Path })
	c := newFixtureClient(t, server.URL, client.Config{})

	_, err := client.Call(context.Background(), c, zeroPathRoute, &zeroPathReq{ID: 0, Flag: false})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if want := "/api/probe/items/0/false"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

// TestNilPathParameterIsStillMissing keeps the guard: absence is an error, and
// the message names the placeholder.
// TestNilPathParameterIsStillMissing 保留原来的保护：真的没提供仍然报错，且错误里
// 带占位符名字。
func TestNilPathParameterIsStillMissing(t *testing.T) {
	c := newFixtureClient(t, "http://127.0.0.1:1", client.Config{})
	_, err := client.Call(context.Background(), c, nilPathRoute, &nilPathReq{})
	if err == nil {
		t.Fatal("Call must fail when a path parameter was not provided")
	}
	if !strings.Contains(err.Error(), `path parameter "id" is missing`) {
		t.Fatalf("error = %v, want a missing-parameter message", err)
	}
}
