// Package endpoint defines framework-neutral typed route descriptors.
//
// The contract layer uses Route[Req, Res] to declare what a route is: HTTP
// method, path, request/response types, rate-limit metadata, error declarations
// and so on. One declaration is consumed in four places:
//
//   - appinterface: stores Routes as package-level variables, declaring the
//     business contract;
//   - apicore/adapter/gofy: BindGroup / BindAll bind descriptors onto gofy
//     routes and find server implementations by OperationID via reflection;
//   - apicore/client: Call / StreamSSE / download helpers build requests and
//     parse responses from the descriptor;
//   - docgen: generates OpenAPI documents from the descriptor.
//
// The package depends only on the standard library and imports neither gin,
// gofy nor any business package: the contract layer can be reused by different
// server implementations, and the client never drags a server framework into
// its dependency tree.
//
// Usage: a Route is normally declared as a package-level variable during init
// and read-only afterwards. Spec() returns a value copy, but the slices it
// contains (Tags / Middleware / Errors) are shared with the caller and must not
// be modified at runtime; append during declaration through Route.Use /
// Group.Use / Group.Add.
// Package endpoint 定义与框架无关的类型化路由描述符。
//
// 契约层用 Route[Req, Res] 声明一条路由“是什么”：HTTP 方法、路径、请求/响应
// 类型、限流元数据、错误声明等。同一份声明被四处消费：
//
//   - appinterface：把 Route 存成包级变量，声明业务契约；
//   - apicore/adapter/gofy：BindGroup / BindAll 把描述符绑定到 gofy 路由，
//     并按 OperationID 反射查找服务端实现方法；
//   - apicore/client：Call / StreamSSE / 下载等按描述符构造请求、解析响应；
//   - docgen：按描述符生成 OpenAPI 文档。
//
// 本包只依赖标准库，不 import gin、gofy 或任何业务包：这样契约层可以被不同
// 服务端实现复用，也保证客户端不会把服务端框架带进依赖树。
//
// 使用约定：Route 通常在 init 阶段声明为包级变量，之后只读。Spec() 返回的是
// 值拷贝，但其中的切片（Tags / Middleware / Errors）与调用方共享，不要在运行时
// 修改；声明阶段的追加请统一走 Route.Use / Group.Use / Group.Add。
package endpoint

import (
	"reflect"
	"strings"
)

// RateKey is the counting subject of rate limiting; the server limiter uses it
// to choose the bucketing dimension.
// RateKey 是限流的计数主体，服务端限流中间件按它决定分桶维度。
type RateKey string

const (
	// RateKeyIP counts per client IP, suitable for unauthenticated public
	// endpoints.
	// RateKeyIP 按客户端 IP 计数，适合未认证的公共接口。
	RateKeyIP RateKey = "ip"
	// RateKeyUser counts per authenticated user ID.
	// RateKeyUser 按已认证用户 ID 计数。
	RateKeyUser RateKey = "user"
	// RateKeyAPIKey counts per caller API Key.
	// RateKeyAPIKey 按调用方 API Key 计数。
	RateKeyAPIKey RateKey = "api_key"
	// RateKeyRoute counts per route as a whole, regardless of caller, which
	// suits protecting a single downstream resource.
	// RateKeyRoute 按路由整体计数，不区分调用方，适合保护单个下游资源。
	RateKeyRoute RateKey = "route"
)

// RateLimit is a route-level rate-limit declaration: at most Limit requests
// are allowed within WindowSeconds seconds.
//
// The contract layer only describes "how many, counted by whom, on which
// store"; it implements no algorithm and decides no over-limit response. The
// server translates the declaration into concrete middleware in the
// BindOptions.RateLimit callback (e.g. gofy's support/ratelimit), and the
// returned middleware is installed first in the route chain; a nil return means
// no limiter. Without a configured callback the field only feeds OpenAPI
// documentation (the generated 429 response description).
// RateLimit 是路由级限流声明：WindowSeconds 秒内最多允许 Limit 次请求。
//
// 契约层只描述“限多少、按谁计数、用哪个存储”，不实现限流算法，也不决定
// 超限时的响应。服务端在 BindOptions.RateLimit 回调里把声明翻译成具体中间件
// （例如 gofy 的 support/ratelimit），返回的中间件安装在路由中间件链最前面；
// 回调返回 nil 表示该路由不挂限流。没有配置回调时，该字段仅参与 OpenAPI
// 文档（生成 429 响应说明）。
//
// 示例：
//
//	&endpoint.RateLimit{
//		Limit:         120,
//		WindowSeconds: 60,
//		Key:           endpoint.RateKeyUser,
//		RedisAlias:    "ratelimit",
//	}
type RateLimit struct {
	// Limit is the number of requests allowed per window and must be positive;
	// a non-positive value makes the declaration invalid, which the server
	// treats as "rate limiting is broken" (the demo answers 503) rather than
	// letting requests through silently.
	// Limit 是窗口内允许的请求数，必须为正数；非正数视为声明无效，由服务端
	// 按“限流失效”处理（demo 返回 503），不会静默放行。
	Limit int
	// WindowSeconds is the window length in seconds and must be positive;
	// servers usually count over a sliding window.
	// WindowSeconds 是统计窗口长度，单位秒，必须为正数；服务端通常按滑动
	// 窗口统计。
	WindowSeconds int
	// Key is the counting subject; see RateKey for the values, while the exact
	// bucketing is up to the server. An unknown or empty value makes the server
	// count per client IP, matching the framework default.
	// Key 是计数主体；取值语义见 RateKey，具体分桶由服务端实现。未知或空值
	// 时服务端按客户端 IP 计数，与框架默认一致。
	Key RateKey
	// RedisAlias is the Redis named-instance alias used as the counter store,
	// e.g. "ratelimit" in the demo config. The contract only names the
	// instance; address, password, database number and client creation are
	// resolved by the server at runtime. Empty means the server default alias
	// ("ratelimit" in the demo). docgen and generated clients ignore the field.
	// RedisAlias 是计数存储使用的 Redis 命名实例别名，例如 demo 配置里的
	// "ratelimit"。契约只声明“用哪个实例”：地址、密码、库号和客户端创建都
	// 由服务端在运行期按别名解析；为空时使用服务端默认别名（demo 为
	// "ratelimit"）。docgen 与生成的客户端不使用该字段。
	RedisAlias string
}

// ErrorSpec declares one expected error for the OpenAPI responses.
//
// It only drives documentation and takes no part in runtime rendering: the
// actual response comes from business errors (errcode.Fault / apperr.Error).
// Entries with Status 0 are ignored by docgen; 400 / 500 plus the 429 derived
// from RateLimit are added automatically, so usually only business-specific
// errors need declaring here (e.g. 404 order not found). A 401 belongs here
// once the route installs an auth middleware.
// ErrorSpec 声明一种预期错误，用于 OpenAPI 的 responses 文档。
//
// 它只描述文档，不参与运行时响应渲染：真实返回由业务错误（errcode.Fault /
// apperr.Error）决定。Status 为 0 的条目会被 docgen 忽略；400 / 500 以及
// 按 RateLimit 推导出的 429 会自动补充，这里通常只需声明业务
// 特有的错误（如 404 订单不存在）。
type ErrorSpec struct {
	// Code is the business error code, matching the code field of the response
	// body.
	// Code 是业务错误码，对应响应体的 code 字段。
	Code int
	// Status is the HTTP status code; ErrorSpecs with the same Status merge into
	// one response.
	// Status 是 HTTP 状态码，相同 Status 的多个 ErrorSpec 会合并成一个响应。
	Status int
	// Message is the default text shown in the documentation.
	// Message 是文档中展示的默认文案。
	Message string
}

// ResponseKind describes special response shapes; the zero value uses the
// standard JSON envelope.
//
// The gofy adapter currently implements only KindJSON; declaring another shape
// fails at BindGroup / BindAll startup, and the server must register the
// streaming route itself, while the contract remains usable for docs and
// clients.
// ResponseKind 描述响应的特殊形态，默认零值走标准 JSON 信封。
//
// 当前 gofy adapter 只实现了 KindJSON；声明其他形态会在 BindGroup / BindAll
// 启动期报错，需要服务端自行注册流式路由，契约仍可用于文档与客户端。
type ResponseKind string

const (
	// KindJSON is the default shape: an application/json {code,msg,data,req_id}
	// envelope.
	// KindJSON 是默认形态：application/json 的 {code,msg,data,req_id} 信封。
	KindJSON ResponseKind = ""
	// KindSSE marks a Server-Sent Events stream; clients consume it with
	// client.StreamSSE and the response type must be endpoint.NoBody.
	// KindSSE 表示 Server-Sent Events 流；客户端用 client.StreamSSE 消费，
	// 响应类型必须声明为 endpoint.NoBody。
	KindSSE ResponseKind = "sse"
	// KindChunked marks a chunked streaming response; the response type must be
	// endpoint.NoBody.
	// KindChunked 表示分块流式响应；响应类型必须声明为 endpoint.NoBody。
	KindChunked ResponseKind = "chunked"
	// KindDownload marks a file download; the response type must be
	// endpoint.NoBody and clients obtain the stream plus filename through
	// client.OpenDownload.
	// KindDownload 表示文件下载；响应类型必须声明为 endpoint.NoBody，
	// 客户端用 client.OpenDownload 拿到流和文件名。
	KindDownload ResponseKind = "download"
)

// NoBody is the placeholder response type for routes without a JSON body:
// NoContent, SSE, chunked and download routes usually fill the Res generic
// parameter with it; a non-JSON shape paired with a concrete response type is
// rejected by the adapter at bind time.
// NoBody 是“没有 JSON 响应体”的路由占位响应类型：NoContent、SSE、chunked
// 和下载路由的 Res 泛型参数通常填它；非 JSON 形态使用具体响应类型会被
// adapter 在绑定阶段拒绝。
type NoBody struct{}

// Spec is the complete metadata of one route.
//
// It serves both runtime adaptation (binding, middleware, implementation
// lookup) and document generation; a new field must consider all three
// consumers: adapter, client and docgen.
// Spec 是一条路由的完整元数据。
//
// 它既服务于运行期适配（绑定、中间件、实现方法查找），也服务于文档生成；
// 新增字段时应同时考虑 adapter、client 与 docgen 三处消费者。
type Spec struct {
	// OperationID is the unique route identifier, the server implementation
	// method name (the adapter looks it up with reflect MethodByName) and the
	// OpenAPI operationId. Required and unique within one service; an empty,
	// duplicate or missing implementation fails at bind startup.
	// OperationID 是路由的唯一标识，同时是服务端实现的方法名（adapter 用
	// reflect MethodByName 查找），也是 OpenAPI 的 operationId。
	// 必填，且在同一服务内不能重复；为空、重复或实现缺失都会在绑定启动期报错。
	OperationID string
	// Method is the HTTP method. The adapter accepts only GET/POST/PUT/PATCH/
	// DELETE/HEAD/OPTIONS.
	// Method 是 HTTP 方法。adapter 只接受 GET/POST/PUT/PATCH/DELETE/
	// HEAD/OPTIONS。
	Method string
	// Path is the route path with {name} placeholders (converted to gin's :name
	// before binding) and must start with "/". Every placeholder requires a
	// matching `uri:"name"` field in the request struct, otherwise binding fails
	// at startup. The path may be group-relative ("/orders/{id}") or carry the
	// full deployment prefix ("/api/v1/order/orders/{id}"); the full route is
	// the group prefix combined with the path.
	// Path 是路由路径，占位符写成 {name}（adapter 绑定前会转成 gin 的 :name），
	// 必须以 "/" 开头。每个占位符都要求请求结构体里有对应的 `uri:"name"` 字段，
	// 否则绑定启动期报错。路径可以是相对分组的 "/orders/{id}"，也可以携带完整
	// 部署前缀 "/api/v1/order/orders/{id}"，完整路由由分组前缀与路径组合得出。
	Path string
	// Prefix is the deployment prefix of the group the route belongs to. Group.Add
	// stamps it from Group.Prefix at declaration time, so contract authors never
	// set it by hand; clients and docgen read it and build the same full path
	// through JoinPath. It stays empty for ungrouped routes.
	// Prefix 是路由所属分组的部署前缀。Group.Add 在声明阶段从 Group.Prefix
	// 写入，契约作者无需手工设置；客户端与 docgen 读取它，并通过 JoinPath 生成
	// 同一条完整路径。未分组的路由保持为空。
	Prefix string
	// Summary is the short OpenAPI description; when empty docgen uses
	// "METHOD Path".
	// Summary 是 OpenAPI 的简短说明；为空时 docgen 使用 "METHOD Path"。
	Summary string
	// Description is the detailed OpenAPI description.
	// Description 是 OpenAPI 的详细说明。
	Description string
	// Tags are business/service tags: docgen uses them for service filtering,
	// path-prefix inference and Swagger grouping.
	// Tags 是业务/服务标签：docgen 用它做服务过滤、路径前缀推导和 Swagger 分组。
	Tags []string
	// Middleware holds route-level middleware logical names executed in
	// declaration order. The server registers the instances in the middleware
	// registry and injects it via gofyadapter.BindOptions.Registry (or overrides
	// once with BindOptions.Middleware); an unregistered name fails at startup.
	// Group middleware always runs before these route middleware.
	// Middleware 是路由级中间件逻辑名，按声明顺序执行。服务端把实例注册到
	// 中间件注册表后经 gofyadapter.BindOptions.Registry 注入（也可用
	// BindOptions.Middleware 做单次覆盖），未注册的名字启动期报错。
	// 分组中间件总是先于这里的路由中间件执行。
	Middleware []string
	// RateLimit nil means no rate limit is declared; a non-nil value is
	// translated by the server's RateLimit callback into concrete middleware and
	// makes docgen emit a 429 response.
	// RateLimit 为 nil 表示不声明限流；非 nil 时由服务端 RateLimit 回调
	// 转成具体中间件，并让 docgen 输出 429 响应。
	RateLimit *RateLimit
	// NoContent means success returns an empty 204 response (no JSON envelope).
	// The Res generic parameter then takes no part in binding and Call returns
	// (nil, nil). It is mutually exclusive with ResponseKind.
	// NoContent 表示成功时返回 204 空响应（不渲染 JSON 信封）。此时 Res 泛型
	// 不参与绑定，客户端 Call 返回 (nil, nil)。它与 ResponseKind 互斥。
	NoContent bool
	// ResponseKind describes the response shape, KindJSON by default; non-JSON
	// shapes require Res to be NoBody, see ResponseKind.
	// ResponseKind 描述响应形态，默认 KindJSON；非 JSON 形态要求 Res 为
	// NoBody，见 ResponseKind 的说明。
	ResponseKind ResponseKind
	// RequestContentType is the Content-Type of POST/PUT/PATCH request bodies,
	// application/json by default; the client also supports multipart/form-data
	// encoding. It also feeds the OpenAPI requestBody.
	// RequestContentType 是 POST/PUT/PATCH 请求体的 Content-Type，默认
	// application/json；客户端另支持 multipart/form-data 的编码。
	// 同时用于 OpenAPI requestBody。
	RequestContentType string
	// ResponseContentType is the response Content-Type; streaming/download
	// routes use it in docgen and fall back to the shape default when unset.
	// ResponseContentType 是响应 Content-Type；流式/下载路由在 docgen 中使用，
	// 没有声明时按形态取默认值。
	ResponseContentType string
	// Errors are additional expected errors for OpenAPI only; see ErrorSpec.
	// Errors 是额外声明的预期错误，仅用于 OpenAPI；见 ErrorSpec。
	Errors []ErrorSpec
	// Deprecated marks the route as deprecated and is written to the OpenAPI
	// deprecated field.
	// Deprecated 标记路由已废弃，写入 OpenAPI 的 deprecated 字段。
	Deprecated bool
}

// Route is a strongly typed route descriptor that binds request/response
// structs and metadata at compile time.
//
// It implements Descriptor and can be mixed with routes of other Req/Res types
// in Group.Routes. spec is unexported: it must not be modified from outside
// after declaration; declaration-time changes go through Use.
// Route 是强类型路由描述符，把请求/响应结构与元数据在编译期绑定在一起。
//
// 它实现 Descriptor，可以和其他 Req/Res 不同的路由一起放进 Group.Routes。
// spec 不导出：声明完成后不应被外部修改，声明阶段的变更走 Use。
type Route[Req, Res any] struct {
	spec Spec
}

// New creates a Route from a complete Spec.
//
// Regular contracts should use the Get / Post / Delete constructors in http.go,
// which fill in method and path first and let the build callback add the
// remaining metadata; calling New directly suits tests, client examples or
// scenarios that need a fully custom Spec.
// New 用完整 Spec 创建 Route。
//
// 常规契约推荐使用 http.go 里的 Get / Post / Delete 等构造器，它们先填好
// 方法与路径，再让 build 回调补充其余元数据；直接调用 New 适合测试、客户端
// 示例或需要完全自定义 Spec 的场景。
func New[Req, Res any](spec Spec) *Route[Req, Res] {
	return &Route[Req, Res]{spec: spec}
}

// Spec returns a copy of the route metadata. Slice fields are shared with the
// internal value and should be treated as read-only.
// Spec 返回路由元数据的拷贝。切片字段与内部共享，调用方应只读。
func (r *Route[Req, Res]) Spec() Spec { return r.spec }

// Use appends route-level middleware names and returns the route for chained
// declarations:
//
// Names are trimmed of surrounding whitespace and empty names are ignored; call
// it only during declaration. A nil route returns nil so expression chains
// expand safely on test doubles.
// Use 追加路由级中间件名字，返回路由本身以便链式声明：
//
//	GetOrder = endpoint.Get[Req, Res](path, build).Use("order-audit")
//
// 名字会去掉首尾空白，空名字被忽略；只应在声明阶段调用。nil 路由返回 nil，
// 方便表达式链在测试替身上安全展开。
func (r *Route[Req, Res]) Use(names ...string) *Route[Req, Res] {
	if r == nil {
		return nil
	}
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			r.spec.Middleware = append(r.spec.Middleware, name)
		}
	}
	return r
}

// RequestType returns the request struct type for adapter binding and docgen
// parameter/body generation. (*Req)(nil) is taken and then dereferenced, so no
// zero-value allocation is needed and interface types work too.
// RequestType 返回请求结构体的类型，供 adapter 绑定和 docgen 生成参数/请求体。
// 用 (*Req)(nil) 取指针再 Elem，不需要零值分配，也适用于接口类型。
func (r *Route[Req, Res]) RequestType() reflect.Type {
	return reflect.TypeOf((*Req)(nil)).Elem()
}

// ResponseType returns the response struct type for adapter signature checks
// and docgen data payloads; NoBody means there is no JSON response body.
// ResponseType 返回响应结构体的类型，供 adapter 校验签名和 docgen 生成 data
// 载荷；NoBody 表示没有 JSON 响应体。
func (r *Route[Req, Res]) ResponseType() reflect.Type {
	return reflect.TypeOf((*Res)(nil)).Elem()
}

// Descriptor is the element of heterogeneous route sets: Routes with different
// Req/Res types are stored and iterated uniformly through it. *Route[Req, Res]
// is the only implementation; adapter and docgen depend only on these three
// methods and never on the concrete generic parameters.
// Descriptor 是异构路由集合的元素：不同 Req/Res 的 Route 通过它统一存放和
// 遍历。*Route[Req, Res] 是唯一实现；adapter 与 docgen 只依赖这三个方法，
// 不关心具体泛型参数。
type Descriptor interface {
	Spec() Spec
	RequestType() reflect.Type
	ResponseType() reflect.Type
}

// Group is a route group: Prefix is handed to the server router.Group, and
// Routes are the routes of the group.
//
// Prefix has two forms, and both keep a single copy of the path:
//   - a full deployment prefix such as "/api/v1/order": Spec.Path holds only the
//     relative path "/orders/{id}" and the full route is Prefix + Spec.Path;
//   - a plain group name such as "/orders": Spec.Path holds the full path, and
//     the adapter trims the part overlapping the Router group prefix before
//     binding.
//
// Add also stamps Prefix onto every added descriptor (Spec.Prefix), so clients
// and docgen build the full path from the same declaration instead of a second
// configured table. A route belongs to one group: adding it to another group
// overwrites the stamped prefix, which binding detects at startup.
//
// The contract layer only declares group information and does not depend on
// gofy/gin; group and route middleware are registered by name on the server and
// then applied. Routes keep declaration order, and both binding and document
// generation follow it.
// Group 是路由分组：Prefix 交给服务端 router.Group，Routes 是该组的路由。
//
// Prefix 有两种写法，两种都只需维护一份路径：
//   - 完整部署前缀，如 "/api/v1/order"：组内 Spec.Path 只写相对路径
//     "/orders/{id}"，完整路由 = Prefix + Spec.Path；
//   - 分组名，如 "/orders"：Spec.Path 写完整路径，adapter 在绑定前会把
//     与 Router 分组前缀重复的部分裁掉。
//
// Add 还会把 Prefix 写入每个加入的路由（Spec.Prefix），客户端与 docgen
// 因此从同一份声明生成完整路径，不再依赖第二份配置表。一条路由只属于一个
// 分组：加入另一个分组会覆盖已写入的前缀，绑定阶段会检测出这种不一致。
//
// 契约层只声明分组信息，不依赖 gofy/gin；分组中间件和路由中间件都由服务端
// 按名字注册后应用。Routes 保持声明顺序，绑定和文档生成都按这个顺序进行。
type Group struct {
	Prefix string
	Routes []Descriptor
	// Middleware holds group middleware logical names executed in declaration
	// order; the server registers the instances in the registry and injects them
	// via gofyadapter.BindOptions.Registry (or overrides once with
	// BindOptions.Middleware), and an unregistered name fails at BindGroup
	// startup. Group middleware runs before route middleware of the group.
	// Middleware 是分组中间件的逻辑名，按声明顺序执行；具体实现由服务端注册到
	// 中间件注册表后经 gofyadapter.BindOptions.Registry 注入（也可用
	// BindOptions.Middleware 做单次覆盖），未注册的 name 在 BindGroup 启动期报错。
	// 分组中间件先于组内路由中间件执行。
	Middleware []string
}

// NewRouteGroup declares a route group whose routes are registered with Add in
// the build callback:
//
// A nil build returns an empty group, which is handy for declaring only the
// prefix or for filling it step by step in tests.
// NewRouteGroup 声明一个路由分组，分组内的路由在 build 回调里用 Add 注册：
//
//	var Orders = endpoint.NewRouteGroup("/api/v1/order", func(group *endpoint.Group) {
//	    group.Add(GetOrder, ListOrders, CreateOrder, CancelOrder)
//	})
//
// build 为 nil 时返回空分组，便于只声明前缀或在测试里逐步填充。
func NewRouteGroup(prefix string, build func(*Group)) *Group {
	group := &Group{Prefix: prefix}
	if build != nil {
		build(group)
	}
	return group
}

// Use declares group-level middleware names; their order is the execution
// order. Empty names are ignored; call it only during declaration. A nil group
// returns nil so chained expressions expand safely.
// Use 声明分组级中间件名字，顺序即执行顺序。空名字会被忽略；只应在声明阶段
// 调用。nil 分组返回 nil，方便链式表达式安全展开。
func (g *Group) Use(names ...string) *Group {
	if g == nil {
		return nil
	}
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			g.Middleware = append(g.Middleware, name)
		}
	}
	return g
}

// Add appends routes to the group and returns the group for chained
// declarations. Append order determines binding order and document order.
//
// Every added route receives the group prefix in Spec.Prefix, so the client and
// docgen build the full path from the descriptor itself. A nil group returns
// nil and stamps nothing.
// Add 把路由加入分组，返回分组本身以便链式声明。加入顺序决定绑定顺序和文档
// 顺序。
//
// 每条加入的路由都会在 Spec.Prefix 中记录分组前缀，客户端与 docgen 因此可以
// 直接从描述符生成完整路径。nil 分组返回 nil，不写入任何前缀。
func (g *Group) Add(routes ...Descriptor) *Group {
	if g == nil {
		return nil
	}
	prefix := strings.TrimSpace(g.Prefix)
	for _, route := range routes {
		if setter, ok := route.(prefixSetter); ok {
			setter.setPrefix(prefix)
		}
	}
	g.Routes = append(g.Routes, routes...)
	return g
}

// prefixSetter lets Group.Add stamp the deployment prefix onto the route value
// without exporting a mutable accessor. *Route is the only implementation.
// prefixSetter 让 Group.Add 把部署前缀写进路由值，又不必导出可变访问器。
// 只有 *Route 实现该接口。
type prefixSetter interface {
	setPrefix(prefix string)
}

func (r *Route[Req, Res]) setPrefix(prefix string) {
	if r != nil {
		r.spec.Prefix = prefix
	}
}

// AddRoute declares and registers a strongly typed route inside the group and
// returns the descriptor; for reuse (client, tests, docs) prefer building a
// package-level variable with New or endpoint.Get first and then calling
// group.Add, so the route is not visible only inside the group.
// AddRoute 在分组内直接声明并注册一条强类型路由，返回描述符；需要复用时
// （客户端、测试、文档）仍推荐先用 New 或 endpoint.Get 等建包级变量，再
// group.Add，避免路由只在分组内部可见。
func AddRoute[Req, Res any](g *Group, spec Spec) *Route[Req, Res] {
	route := New[Req, Res](spec)
	g.Add(route)
	return route
}
