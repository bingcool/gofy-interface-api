// Package gofyadapter_test verifies the adapter using only library fixtures.
// The real-contract binding tests live in the project module (§4.7).
// 包 gofyadapter_test 只用库内 fixture 验证 adapter；真实契约的绑定测试归项目模块（§4.7）。
package gofyadapter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gofyadapter "github.com/bingcool/gofy-interface-api/adapter/gofy"
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/contracts/common/paging"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
	"github.com/bingcool/gofy/support"
	"github.com/gin-gonic/gin"
)

type fixtureImpl struct{}

func (fixtureImpl) GetItem(_ context.Context, req *testcontract.ItemRequest) (*testcontract.ItemResponse, error) {
	return &testcontract.ItemResponse{Item: &testcontract.ItemDTO{ID: req.ID, Name: "item"}}, nil
}

func (fixtureImpl) ListItems(_ context.Context, req *testcontract.ListRequest) (*paging.ListData[testcontract.ItemDTO], error) {
	return &paging.ListData[testcontract.ItemDTO]{Total: 1, List: []testcontract.ItemDTO{{ID: req.UserID}}}, nil
}

func (fixtureImpl) CreateItem(_ context.Context, req *testcontract.CreateRequest) (*testcontract.CreateResponse, error) {
	return &testcontract.CreateResponse{Item: &testcontract.ItemDTO{ID: req.UserID, Amount: req.Amount}}, nil
}

func (fixtureImpl) CancelItem(_ context.Context, _ *testcontract.CancelRequest) (*endpoint.NoBody, error) {
	return new(endpoint.NoBody), nil
}

func (fixtureImpl) GetConfig(_ context.Context, _ *testcontract.ConfigRequest) (*testcontract.ConfigResponse, error) {
	return &testcontract.ConfigResponse{AuthRequired: true}, nil
}

func newFixtureRouter() (*gin.Engine, *support.Router) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// Fixture specs carry the full deployment prefix in Spec.Path; the router
	// itself must not add another one (bindPath trims GroupPrefix).
	// fixture 的 Spec.Path 已含完整部署前缀，Router 不能再叠加一层（bindPath 会按
	// GroupPrefix 裁剪），否则请求路径会错位。
	return engine, support.NewRouter(engine, support.RouterConfig{
		Responder: support.NewResponder(support.ResponderConfig{AlwaysOK: false}),
	})
}

func serve(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestBindAllFixtureRoutesSucceed(t *testing.T) {
	engine, r := newFixtureRouter()
	if err := gofyadapter.BindAll(r, testcontract.Routes(), fixtureImpl{}, gofyadapter.BindOptions{}); err != nil {
		t.Fatalf("BindAll: %v", err)
	}
	rec := serve(engine, http.MethodGet, "/api/test/items/7")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":7`) {
		t.Fatalf("response missing item id: %s", rec.Body.String())
	}
}

func TestBindAllRejectsDuplicateOperationID(t *testing.T) {
	_, r := newFixtureRouter()
	// A distinct path keeps gin from panicking on a duplicate registration; only
	// the operation id collides, which is what BindAll must report.
	// 用不同路径避免 gin 重复注册 panic，只让 operation id 冲突。
	dup := endpoint.New[testcontract.ItemRequest, testcontract.ItemResponse](endpoint.Spec{
		OperationID: "GetItem",
		Method:      http.MethodGet,
		Path:        "/dup/{id}",
	})
	routes := []endpoint.Descriptor{testcontract.GetItem, dup}
	err := gofyadapter.BindAll(r, routes, fixtureImpl{}, gofyadapter.BindOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicate operation id") {
		t.Fatalf("BindAll error = %v, want duplicate operation id", err)
	}
}

func TestBindAllAggregatesEveryProblem(t *testing.T) {
	_, r := newFixtureRouter()
	bad := endpoint.New[testcontract.ItemRequest, testcontract.ItemResponse](endpoint.Spec{
		OperationID: "BadAuthProbe",
		Method:      "TRACE",
		Path:        "no-leading-slash",
	})
	err := gofyadapter.BindAll(r, []endpoint.Descriptor{bad, bad, testcontract.GetItem}, fixtureImpl{}, gofyadapter.BindOptions{})
	if err == nil {
		t.Fatal("BindAll must reject invalid descriptors")
	}
	for _, want := range []string{"unsupported method", "path must start with /", "duplicate operation id"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("aggregated error missing %q: %v", want, err)
		}
	}
}

func TestBindAllRejectsMissingDependencies(t *testing.T) {
	_, r := newFixtureRouter()
	if err := gofyadapter.BindAll(nil, testcontract.Routes(), fixtureImpl{}, gofyadapter.BindOptions{}); err == nil {
		t.Fatal("nil router must fail")
	}
	if err := gofyadapter.BindAll(r, testcontract.Routes(), nil, gofyadapter.BindOptions{}); err == nil {
		t.Fatal("nil implementation must fail")
	}
}

func TestBindGroupResolvesNamesInDeclarationOrder(t *testing.T) {
	engine, r := newFixtureRouter()
	var order []string
	mw := func(name string) support.Middleware {
		return func(c *gin.Context) {
			order = append(order, name)
			c.Next()
		}
	}
	opts := gofyadapter.BindOptions{Middleware: map[string]support.Middleware{
		"audit": mw("audit"),
		"auth":  mw("auth"),
	}}
	if err := gofyadapter.BindGroup(r, testcontract.Items, fixtureImpl{}, opts); err != nil {
		t.Fatalf("BindGroup: %v", err)
	}
	rec := serve(engine, http.MethodGet, "/api/test/items/7")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(order) != 2 || order[0] != "audit" || order[1] != "auth" {
		t.Fatalf("middleware order = %v, want [audit auth]", order)
	}
}

func TestBindGroupRejectsUnregisteredName(t *testing.T) {
	_, r := newFixtureRouter()
	err := gofyadapter.BindGroup(r, testcontract.Items, fixtureImpl{}, gofyadapter.BindOptions{})
	if err == nil || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("BindGroup error = %v, want missing name audit", err)
	}
}

func TestAuthValidatorSeesGroupMiddlewareNames(t *testing.T) {
	_, r := newFixtureRouter()
	var seen [][]string
	opts := gofyadapter.BindOptions{
		Middleware: map[string]support.Middleware{
			"audit": func(c *gin.Context) { c.Next() },
			"auth":  func(c *gin.Context) { c.Next() },
		},
		AuthValidator: func(spec endpoint.Spec, declared []string) error {
			if spec.Auth == endpoint.AuthRequired {
				seen = append(seen, append([]string(nil), declared...))
			}
			return nil
		},
	}
	if err := gofyadapter.BindGroup(r, testcontract.Items, fixtureImpl{}, opts); err != nil {
		t.Fatalf("BindGroup: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("AuthValidator was never called for the AuthRequired route")
	}
	joined := strings.Join(seen[0], ",")
	if !strings.Contains(joined, "auth") || !strings.Contains(joined, "audit") {
		t.Fatalf("declared names = %v, want group names audit/auth", seen[0])
	}
}

func TestAuthRequiredAddsNoImplicitMiddleware(t *testing.T) {
	_, r := newFixtureRouter()
	// GetItem declares AuthRequired but no auth middleware name; the adapter must
	// not invent one, and the default (nil) validator keeps startup unchanged.
	// GetItem 声明 AuthRequired 但没有 auth 中间件名；adapter 不得擅自补一个，
	// 默认 nil validator 也保持启动行为不变。
	if err := gofyadapter.BindAll(r, []endpoint.Descriptor{testcontract.GetItem}, fixtureImpl{}, gofyadapter.BindOptions{}); err != nil {
		t.Fatalf("BindAll: %v", err)
	}
}
