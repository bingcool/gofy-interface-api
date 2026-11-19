// Package testcontract supplies the smallest route descriptors used by library
// tests. It never mirrors a project's DTOs, so library tests stay independent
// of any project contract module.
// 包 testcontract 提供库内测试使用的最小路由描述符。它不复制任何项目的 DTO，
// 保证库内测试不依赖项目契约模块。
package testcontract

import (
	"net/http"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/contracts/common/paging"
)

const (
	// ServiceName tags every fixture route for docgen and client tests.
	// ServiceName 是 fixture 路由的标签，供 docgen 与客户端测试使用。
	ServiceName = "fixture"

	// ErrCodeNotFound and ErrCodeClosed are fixture business error codes.
	// ErrCodeNotFound 与 ErrCodeClosed 是 fixture 的业务错误码。
	ErrCodeNotFound = 40401
	ErrCodeClosed   = 40901

	// StatusPaid is one fixture enum value for query binding tests.
	// StatusPaid 是 fixture 的枚举值，用于 query 绑定测试。
	StatusPaid = "paid"
)

// ItemDTO is the fixture response payload.
// ItemDTO 是 fixture 的响应载荷。
type ItemDTO struct {
	ID     uint64  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`
}

// ItemRequest binds a path parameter.
// ItemRequest 绑定路径参数。
type ItemRequest struct {
	ID uint64 `uri:"id" form:"id" json:"id"`
}

// ListRequest binds paging and filter query parameters.
// ListRequest 绑定分页与过滤 query 参数。
type ListRequest struct {
	paging.PageRequest
	UserID uint64 `form:"user_id" json:"user_id"`
	Status string `form:"status" json:"status"`
}

// ItemResponse wraps one item.
// ItemResponse 包装单个条目。
type ItemResponse struct {
	Item *ItemDTO `json:"item"`
}

// CreateRequest is a JSON body request.
// CreateRequest 是 JSON 请求体。
type CreateRequest struct {
	UserID uint64  `json:"user_id"`
	Amount float64 `json:"amount"`
}

// CreateResponse wraps the created item.
// CreateResponse 包装新建的条目。
type CreateResponse struct {
	Item *ItemDTO `json:"item"`
}

// CancelRequest binds a path parameter for a write route.
// CancelRequest 为写路由绑定路径参数。
type CancelRequest struct {
	ID uint64 `uri:"id" json:"id"`
}

// ConfigRequest has no parameters.
// ConfigRequest 没有任何参数。
type ConfigRequest struct{}

// ConfigResponse is a flat response without nested DTOs.
// ConfigResponse 是不含嵌套 DTO 的扁平响应。
type ConfigResponse struct {
	AuthRequired bool `json:"auth_required"`
}

var (
	// GetItem covers path parameters and declared errors.
	// GetItem 覆盖路径参数与错误声明。
	GetItem = endpoint.New[ItemRequest, ItemResponse](endpoint.Spec{
		OperationID: "GetItem",
		Method:      http.MethodGet,
		Path:        "/api/test/items/{id}",
		Summary:     "get one item",
		Tags:        []string{ServiceName},
		Errors: []endpoint.ErrorSpec{
			{Code: ErrCodeNotFound, Status: http.StatusNotFound, Message: "item not found"},
		},
	})

	// ListItems covers paging.ListData schema generation and query binding.
	// ListItems 覆盖 paging.ListData 的 schema 生成与 query 绑定。
	ListItems = endpoint.New[ListRequest, paging.ListData[ItemDTO]](endpoint.Spec{
		OperationID: "ListItems",
		Method:      http.MethodGet,
		Path:        "/api/test/items",
		Summary:     "list items",
		Tags:        []string{ServiceName},
	})

	// CreateItem covers a JSON request body and a write method.
	// CreateItem 覆盖 JSON 请求体与写方法。
	CreateItem = endpoint.New[CreateRequest, CreateResponse](endpoint.Spec{
		OperationID: "CreateItem",
		Method:      http.MethodPost,
		Path:        "/api/test/items",
		Summary:     "create an item",
		Tags:        []string{ServiceName},
		RateLimit: &endpoint.RateLimit{
			Limit:         60,
			WindowSeconds: 60,
			Key:           endpoint.RateKeyUser,
		},
	})

	// CancelItem covers a NoBody response.
	// CancelItem 覆盖无响应体的路由。
	CancelItem = endpoint.New[CancelRequest, endpoint.NoBody](endpoint.Spec{
		OperationID: "CancelItem",
		Method:      http.MethodPost,
		Path:        "/api/test/items/{id}/cancel",
		Summary:     "cancel an item",
		Tags:        []string{ServiceName},
		Errors: []endpoint.ErrorSpec{
			{Code: ErrCodeClosed, Status: http.StatusConflict, Message: "item closed"},
		},
	})

	// GetConfig is a route in a group without middleware: the group below
	// declares none, so the adapter installs nothing for it.
	// GetConfig 是不带中间件的分组里的路由：下面的分组没有声明中间件，
	// 因此 adapter 不会为它安装任何中间件。
	GetConfig = endpoint.New[ConfigRequest, ConfigResponse](endpoint.Spec{
		OperationID: "GetConfig",
		Method:      http.MethodGet,
		Path:        "/api/test/config",
		Summary:     "read config",
		Tags:        []string{ServiceName},
	})

	// Items declares group middleware names and keeps route declaration order.
	// Items 声明分组中间件名，并保持路由声明顺序。
	Items = endpoint.NewRouteGroup("/api/test", func(g *endpoint.Group) {
		g.Use("audit", "auth")
		g.Add(GetItem, ListItems, CreateItem, CancelItem)
	})

	// Config is a second group without middleware.
	// Config 是第二个不带中间件的分组。
	Config = endpoint.NewRouteGroup("/api/test/config", func(g *endpoint.Group) {
		g.Add(GetConfig)
	})
)

// Routes returns every fixture descriptor in declaration order.
// Routes 按声明顺序返回全部 fixture 描述符。
func Routes() []endpoint.Descriptor {
	out := make([]endpoint.Descriptor, 0, len(Items.Routes)+len(Config.Routes))
	out = append(out, Items.Routes...)
	out = append(out, Config.Routes...)
	return out
}
