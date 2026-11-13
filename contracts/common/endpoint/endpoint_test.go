package endpoint_test

import (
	"testing"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type testReq struct{}

type testRes struct{}

func TestHTTPMethodHelpers(t *testing.T) {
	cases := []struct {
		name   string
		method string
		route  *endpoint.Route[testReq, testRes]
	}{
		{"get", "GET", endpoint.Get[testReq, testRes]("/a", nil)},
		{"post", "POST", endpoint.Post[testReq, testRes]("/a", nil)},
		{"put", "PUT", endpoint.Put[testReq, testRes]("/a", nil)},
		{"patch", "PATCH", endpoint.Patch[testReq, testRes]("/a", nil)},
		{"delete", "DELETE", endpoint.Delete[testReq, testRes]("/a", nil)},
		{"head", "HEAD", endpoint.Head[testReq, testRes]("/a", nil)},
		{"options", "OPTIONS", endpoint.Options[testReq, testRes]("/a", nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.route.Spec()
			if spec.Method != tc.method || spec.Path != "/a" {
				t.Fatalf("spec = %s %s, want %s /a", spec.Method, spec.Path, tc.method)
			}
		})
	}
}

func TestHTTPHelperBuildsMetadata(t *testing.T) {
	route := endpoint.Get[testReq, testRes]("/orders/{id}", func(spec *endpoint.Spec) {
		spec.OperationID = "GetOrder"
		spec.Auth = endpoint.AuthRequired
		spec.Path = "/override" // build 可以覆盖默认值与路径
	})
	spec := route.Spec()
	if spec.Method != "GET" || spec.Path != "/override" || spec.OperationID != "GetOrder" ||
		spec.Auth != endpoint.AuthRequired {
		t.Fatalf("spec = %+v", spec)
	}
	if route.RequestType().NumField() != 0 {
		t.Fatalf("request type = %v", route.RequestType())
	}
}

func TestRouteUseAddsMiddlewareNames(t *testing.T) {
	route := endpoint.Get[testReq, testRes]("/orders", func(spec *endpoint.Spec) {
		spec.Middleware = []string{"audit"}
	}).Use(" ", "tenant")
	if got := route.Spec().Middleware; len(got) != 2 || got[0] != "audit" || got[1] != "tenant" {
		t.Fatalf("middleware = %v, want [audit tenant]", got)
	}
}

func TestGroupAddAndAddRoute(t *testing.T) {
	group := endpoint.NewRouteGroup("/orders", func(g *endpoint.Group) {
		g.Use("audit", " ", "auth")
		g.Add(endpoint.Get[testReq, testRes]("/orders", nil))
		endpoint.AddRoute[testReq, testRes](g, endpoint.Spec{OperationID: "List", Method: "GET", Path: "/orders/list"})
	})
	if len(group.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(group.Routes))
	}
	if group.Prefix != "/orders" {
		t.Fatalf("prefix = %q", group.Prefix)
	}
	if len(group.Middleware) != 2 || group.Middleware[0] != "audit" || group.Middleware[1] != "auth" {
		t.Fatalf("middleware = %v, want [audit auth]", group.Middleware)
	}
}
