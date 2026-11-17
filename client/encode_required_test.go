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

// TestRequiredQueryFieldsKeepZeroValues pins the encoder half of the shared
// required-field rule: a non-pointer field without omitempty or default reaches
// the server even when it is zero, so the query and the docgen required list
// agree. PageRequest fields carry a default tag and stay optional.
// TestRequiredQueryFieldsKeepZeroValues 固定必填判定在编码器一侧的行为：没有
// omitempty 或 default 的非指针字段即使为零值也发给服务端，从而与 docgen 的
// required 列表一致；PageRequest 字段带 default 标签，仍然可省略。
func TestRequiredQueryFieldsKeepZeroValues(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"total":1,"list":[{"id":3}]}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.ListItems, &testcontract.ListRequest{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if values := got["user_id"]; len(values) != 1 || values[0] != "0" {
		t.Fatalf("user_id = %v, want [0]", values)
	}
	if values, ok := got["status"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("status = %v (present %v), want one empty value", values, ok)
	}
	if values, ok := got["page"]; ok {
		t.Fatalf("page = %v, want omitted because the default tag makes it optional", values)
	}
}

// TestPathParameterIsNotRepeatedInQuery covers the dual uri+form tag: the field
// fills /items/{id} and must not be sent again as a query parameter.
// TestPathParameterIsNotRepeatedInQuery 覆盖 uri+form 双标签：字段用于填充
// /items/{id}，不得再作为 query 参数重复发送。
func TestPathParameterIsNotRepeatedInQuery(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"item":{"id":7}}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 7}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotPath != "/api/test/items/7" {
		t.Fatalf("path = %q, want /api/test/items/7", gotPath)
	}
	if gotQuery != "" {
		t.Fatalf("raw query = %q, want empty", gotQuery)
	}
}

// TestDescriptorPrefixJoinsRelativePath checks the deployment-prefix form of
// Group.Prefix: the descriptor carries the prefix and the client prepends it to
// the relative Spec.Path without the caller's help.
// TestDescriptorPrefixJoinsRelativePath 覆盖 Group.Prefix 的部署前缀形态：描述符
// 自身携带前缀，客户端把它拼到相对的 Spec.Path 前，调用方无需传前缀。
func TestDescriptorPrefixJoinsRelativePath(t *testing.T) {
	group := endpoint.NewRouteGroup("/api/v1/order", func(g *endpoint.Group) {
		g.Add(endpoint.Get[testcontract.ItemRequest, testcontract.ItemResponse]("/orders/{id}", func(spec *endpoint.Spec) {
			spec.OperationID = "GetPrefixedOrder"
		}))
	})
	route, ok := group.Routes[0].(*endpoint.Route[testcontract.ItemRequest, testcontract.ItemResponse])
	if !ok {
		t.Fatalf("route type = %T", group.Routes[0])
	}

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"item":{"id":7}}}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	if _, err := client.Call(context.Background(), c, route, &testcontract.ItemRequest{ID: 7}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotPath != "/api/v1/order/orders/7" {
		t.Fatalf("path = %q, want /api/v1/order/orders/7", gotPath)
	}
}
