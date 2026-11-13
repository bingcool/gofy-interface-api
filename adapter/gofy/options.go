package gofyadapter

import (
	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy/support"
)

// MiddlewareRegistry is the "middleware alias -> instance" lookup interface
// injected by the server and implemented by support/middleware.Registry. Only
// the methods the adapter needs are declared here, so the adapter does not
// depend on a concrete registry type and tests can inject their own.
// MiddlewareRegistry 是服务端注入的「中间件别名 → 实例」查询接口，由
// support/middleware.Registry 实现。这里只声明需要的方法，adapter 因此不依赖
// 具体注册表类型，测试也可以注入自己的实现。
type MiddlewareRegistry interface {
	Lookup(name string) (support.Middleware, bool)
}

// AuthValidator checks, after middleware declarations have resolved, whether
// Spec.Auth matches the auth policy that is actually installed.
//
// spec is the route metadata (including OperationID and Auth);
// declaredMiddleware holds the logical names through which this route may
// declare auth, group names (Group.Use, before BindGroup resolves them) first
// and route names (Spec.Middleware / Route.Use) second. BindOptions.Global and
// BindOptions.Routes carry middleware instances rather than names, so they
// never appear in declaredMiddleware.
//
// It is called only when Spec.Auth is AuthRequired or AuthOptional; AuthNone
// (the zero value) imposes no auth requirement and is skipped, so the callback
// is never triggered by mistake. A non-nil return makes BindGroup / BindAll
// fail at startup (Reload follows the same path), and the error names the
// offending route instead of surfacing at runtime.
//
// The adapter interprets no auth alias and bundles no auth middleware: the
// callback applies the application's own policy, e.g. Required demands "auth"
// while Optional demands a middleware name that admits anonymous requests; the
// exact names are the application's convention. A nil validator skips the
// check entirely, which matches the behaviour before this field existed.
// AuthValidator 在中间件声明解析完成后校验 Spec.Auth 与真实安装的认证策略
// 是否一致。
//
// spec 是路由元数据（含 OperationID 与 Auth）；declaredMiddleware 是这条路由
// 可声明认证的逻辑名，顺序为分组名（Group.Use，BindGroup 解析前）在前、
// 路由名（Spec.Middleware / Route.Use）在后。BindOptions.Global 与
// BindOptions.Routes 直接传入的是中间件实例而不是名字，因此不会出现在
// declaredMiddleware 里。
//
// 只有 Spec.Auth 为 AuthRequired 或 AuthOptional 时才会调用；AuthNone
// （零值）不产生认证要求，adapter 直接跳过，回调不会被误触发。回调返回
// 非 nil 会让 BindGroup / BindAll 启动失败（Reload 也走同一路径），错误会
// 带上具体路由，而不是留到运行期才发现。
//
// adapter 不解释任何认证别名，也不内置认证中间件：回调按应用自己的策略判断，
// 例如 Required 要求存在 "auth"、Optional 要求存在一个支持匿名放行的
// 中间件名，具体名字由应用约定。为 nil 时跳过校验，行为与引入该字段之前
// 完全一致。
type AuthValidator func(spec endpoint.Spec, declaredMiddleware []string) error

// BindOptions configures BindAll middleware and validation.
//
// 所有中间件都必须显式给出：要么是服务端直接传入的实例
// （Global/Routes/RateLimit），要么是契约里声明的逻辑名
// （Group.Middleware、Spec.Middleware）。逻辑名先查 Middleware 映射，再查
// Registry，最后查 Router 携带的 App 注册表（App 启动时统一安装，
// support.RouterConfig.Middleware）；三处都没有就启动失败，不存在默认
// 中间件或内置兜底。通常只依赖 App 注册表即可，无需在每个模块里注入
// Registry。也可以按 Gin 习惯自己 router.Group(prefix, mw...) 后调用
// BindAll，此时分组名字不会被解析，只有路由级名字生效。
type BindOptions struct {
	// Global applies to every contract route.
	// Global 作用于每一条契约路由。
	Global []support.Middleware

	// Routes adds middleware per OperationID.
	// Routes 按 OperationID 追加路由级中间件。
	Routes map[string][]support.Middleware

	// Middleware is the explicit alias map of this binding and has the highest
	// priority, so it can override same-named middleware in a registry (e.g. a
	// test stub or a module-specific implementation). Group names come from
	// endpoint.Group.Use (resolved by BindGroup) and route names from
	// Spec.Middleware / Route.Use (resolved by BindAll). A missing name or a
	// nil value fails at startup.
	// Middleware 是本次绑定的显式别名映射，优先级最高，用于覆盖注册表里的
	// 同名中间件（例如测试桩或某个模块的定制实现）。分组名来自
	// endpoint.Group.Use（由 BindGroup 解析），路由名来自 Spec.Middleware /
	// Route.Use（由 BindAll 解析）。名字缺失或值为 nil 时在启动期报错。
	Middleware map[string]support.Middleware

	// Registry is an optional server-side registry. When nil, only the
	// Middleware map and the App-level registry carried by the Router are used.
	// Registry 是可选的服务端注册表。为 nil 时只使用 Middleware 映射和
	// Router 携带的 App 注册表。
	Registry MiddlewareRegistry

	// RateLimit builds the limiter middleware from the contract declaration. It
	// is called only for routes whose Spec.RateLimit is non-nil, and the
	// returned middleware is installed first in the route chain (after group
	// middleware, before Global, route middleware names and
	// Routes[OperationID]); nil means the route gets no limiter. The callback
	// may read every declared field, including RedisAlias, which is only a hint
	// for the server to resolve the counter store, so the adapter interprets
	// none of it. The callback may defer external initialization: route binding
	// can happen before dependencies such as Redis are ready, so actual I/O can
	// wait until the middleware handles a request.
	// RateLimit 按契约声明构造限流中间件，只对 Spec.RateLimit 非 nil 的路由
	// 调用，返回值安装在路由中间件链最前面（分组中间件之后，Global、路由
	// 中间件名和 Routes[OperationID] 之前）；返回 nil 表示该路由不挂限流。
	// 回调可以读取声明的全部字段，包括 RedisAlias——它只是服务端解析计数
	// 存储的线索，adapter 不做解释。回调允许延迟初始化外部资源：路由绑定
	// 可能早于 Redis 等依赖就绪，实际 I/O 可以放到中间件处理请求时。
	RateLimit func(endpoint.Spec) support.Middleware

	// AuthValidator checks, after the middleware names of the route have
	// resolved, whether the auth declaration matches the installed auth policy;
	// see the AuthValidator type. nil means no check (the default), so the
	// startup behaviour of existing projects is unchanged; production
	// deployments should configure strict validation explicitly.
	// AuthValidator 在路由的中间件名字解析完成后校验认证声明是否与真实
	// 安装的认证策略一致，见 AuthValidator 类型说明。nil 表示不校验（默认），
	// 现有项目的启动行为不变；生产环境建议显式配置严格校验。
	AuthValidator AuthValidator

	// groupMiddleware holds the group middleware logical names passed in by
	// BindGroup; it is read only by AuthValidator, takes no part in resolution
	// and is not exposed.
	// groupMiddleware 是 BindGroup 传入的分组中间件逻辑名，仅供
	// AuthValidator 读取；它不参与解析，也不对外暴露。
	groupMiddleware []string

	// Strict is reserved. Signature mismatches are always reported.
	// Strict 为保留字段；签名不匹配始终会被上报。
	Strict bool
}
