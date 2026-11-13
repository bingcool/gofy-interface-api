// Package gofyadapter binds interface-service contracts onto gofy routes.
//
// It is the only package that depends on github.com/bingcool/gofy; the
// contracts and client layers stay framework-agnostic.
// Package gofyadapter 把 interface-service 契约绑定到 gofy 路由。
//
// 它是唯一依赖 github.com/bingcool/gofy 的包；契约层和客户端保持与框架无关。
package gofyadapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/contracts/common/errcode"
	"github.com/bingcool/gofy/support"
	"github.com/gin-gonic/gin"
)

var allowedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// Bind registers a typed contract route. The group prefix may either carry the
// full deployment prefix (e.g. "/api/v1/order", with spec.Path holding the
// relative path "/orders/{id}") or only the group name (e.g. "/orders", with
// spec.Path holding the full path and bindPath trimming the group prefix).
// Both forms keep a single copy of the path.
//
// Bind installs only the explicitly passed mw and does not resolve logical
// names from Spec.Middleware; use BindAll / BindGroup when names must be
// resolved.
// Bind 注册一条类型化契约路由。分组前缀既可以携带完整部署前缀
// （如 "/api/v1/order"，此时 spec.Path 写相对路径 "/orders/{id}"），也可以
// 只写分组名（如 "/orders"，此时 spec.Path 写完整路径并由 bindPath 裁掉分组
// 前缀）。两种形态都只需维护一份路径。
//
// Bind 只安装显式传入的 mw，不解析 Spec.Middleware 里的逻辑名；需要按名字
// 解析时改用 BindAll / BindGroup。
func Bind[Req, Res any](
	r *support.Router,
	rt *endpoint.Route[Req, Res],
	h support.Handler[Req, Res],
	mw ...support.Middleware,
) {
	spec := rt.Spec()
	path := ginPath(bindPath(r, spec.Path))
	if spec.NoContent {
		support.NoContent(r, spec.Method, path, func(ctx context.Context, req *Req) error {
			_, err := h(ctx, req)
			return err
		}, mw...)
		return
	}
	support.Any(r, spec.Method, path, h, mw...)
}

// BindGroup registers an endpoint.Group: it first resolves the middleware names
// declared by group.Use against opts.Middleware, opts.Registry and the
// App-level registry carried by the Router, creates a sub-router on
// group.Prefix with those instances installed, then binds group.Routes using
// the group-relative paths. A missing middleware name fails at startup.
// BindGroup 注册一个 endpoint.Group：先用 opts.Middleware、opts.Registry 和
// Router 携带的 App 级注册表解析 group.Use 声明的中间件名，在 group.Prefix
// 上创建子 Router 并安装这些实例，再用组内相对路径绑定 group.Routes。
// 中间件名缺失会在启动期报错。
func BindGroup(
	r *support.Router,
	group *endpoint.Group,
	impl any,
	opts BindOptions,
) error {
	if r == nil {
		return errors.New("gofyadapter: router is required")
	}
	if group == nil {
		return errors.New("gofyadapter: route group is required")
	}
	mw, err := resolveGroupMiddleware(r, group, opts)
	if err != nil {
		return err
	}
	// The group name is also handed to AuthValidator: auth is usually declared
	// at the group level, and route names alone would make Required routes look
	// like no auth middleware is installed.
	// 分组名要传给 AuthValidator：认证通常声明在分组层，只给路由名会让
	// Required 路由被误判为“没有安装认证”。
	opts.groupMiddleware = group.Middleware
	return BindAll(r.Group(group.Prefix, mw...), group.Routes, impl, opts)
}

// resolveGroupMiddleware maps the logical names declared by the group onto
// concrete middleware, preserving their order.
//
// resolveGroupMiddleware 把分组声明的逻辑名映射成具体中间件，顺序保持不变。
func resolveGroupMiddleware(r *support.Router, group *endpoint.Group, opts BindOptions) ([]support.Middleware, error) {
	resolved, err := resolveMiddlewareNames(r, group.Middleware, opts)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", group.Prefix, err)
	}
	return resolved, nil
}

// BindAll validates the route set and binds every descriptor to impl.
// Signature and metadata problems are aggregated: the error lists every failed
// route instead of stopping at the first one.
// BindAll 校验路由集合并把每个描述符绑定到 impl。
// 签名与元数据问题会聚合返回：错误里列出所有失败的路由，而不是遇到第一个
// 就退出。
func BindAll(
	r *support.Router,
	routes []endpoint.Descriptor,
	impl any,
	opts BindOptions,
) error {
	if r == nil {
		return errors.New("gofyadapter: router is required")
	}
	if impl == nil {
		return errors.New("gofyadapter: implementation is required")
	}
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	seenOp := make(map[string]bool, len(routes))
	seenRoute := make(map[string]bool, len(routes))
	value := reflect.ValueOf(impl)
	responder := r.Responder()

	for _, desc := range routes {
		spec := desc.Spec()
		name := spec.OperationID
		if name == "" {
			name = spec.Method + " " + spec.Path
		}
		if spec.OperationID == "" {
			add("%s: operation id is empty", name)
		} else if seenOp[spec.OperationID] {
			add("%s: duplicate operation id", name)
		} else {
			seenOp[spec.OperationID] = true
		}
		if !allowedMethods[spec.Method] {
			add("%s: unsupported method %q", name, spec.Method)
		}
		if !strings.HasPrefix(spec.Path, "/") {
			add("%s: path must start with /", name)
		}
		if placeholders, err := pathPlaceholders(spec.Path); err != nil {
			add("%s: %v", name, err)
		} else if err := requireURIFields(desc.RequestType(), placeholders); err != nil {
			add("%s: %v", name, err)
		}
		key := spec.Method + " " + spec.Path
		if seenRoute[key] {
			add("%s: duplicate route %s", name, key)
		}
		seenRoute[key] = true

		if spec.NoContent && spec.ResponseKind != endpoint.KindJSON {
			add("%s: NoContent and ResponseKind are mutually exclusive", name)
		}
		if spec.ResponseKind != endpoint.KindJSON {
			if desc.ResponseType() != reflect.TypeOf(endpoint.NoBody{}) {
				add("%s: %s routes must use endpoint.NoBody", name, spec.ResponseKind)
			}
			add("%s: response kind %q is not supported by the gofy adapter", name, spec.ResponseKind)
			continue
		}

		method := value.MethodByName(spec.OperationID)
		if !method.IsValid() {
			add("%s: implementation has no method %s", name, spec.OperationID)
			continue
		}
		if spec.NoContent {
			if err := checkSignature(method, desc.RequestType(), nil, true); err != nil {
				add("%s: %v", name, err)
				continue
			}
		} else if err := checkSignature(method, desc.RequestType(), desc.ResponseType(), false); err != nil {
			add("%s: %v", name, err)
			continue
		}

		mw, err := routeMiddleware(r, spec, opts)
		if err != nil {
			add("%s: %v", name, err)
			continue
		}
		// Validate auth declarations only after the middleware names of the route
		// have resolved: the callback then sees exactly the group and route names
		// that get installed, so a mismatch fails at startup.
		// 中间件名解析完成后再校验认证声明：回调看到的分组名与路由名就是
		// 实际会被安装的那一组，声明与实现不一致时启动直接失败。
		if err := validateAuth(spec, opts); err != nil {
			add("%s: %v", name, err)
			continue
		}
		r.Handle(spec.Method, ginPath(bindPath(r, spec.Path)), dynamicHandler(r, responder, method, desc, spec), mw...)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New("gofyadapter: invalid routes:\n  - " + strings.Join(problems, "\n  - "))
	}
	return nil
}

// routeMiddleware assembles the middleware chain of one route in this order:
// RateLimit (when declared by the contract) -> BindOptions.Global ->
// Spec.Middleware (logical names resolved) -> Routes[OperationID].
//
// The limiter comes first in the route chain: excess requests are rejected
// before the remaining route middleware and the handler run. Group middleware
// is installed by router.Group and still runs before everything here; auth is
// usually declared at the group level (group.Use("auth")), so identity is ready
// when the limiter counts per user. Routes without Spec.RateLimit get no
// limiter.
//
// Every middleware other than auth comes from explicit declarations: Spec.Auth
// only drives OpenAPI marking, and the adapter never attaches auth middleware
// based on AuthRequired; routes that need auth must declare group.Use("auth")
// or spec.Middleware = []string{"auth"} in the contract.
// routeMiddleware 组装单条路由的中间件链，顺序为：
// RateLimit（契约声明时）→ BindOptions.Global → Spec.Middleware（逻辑名解析）
// → Routes[OperationID]。
//
// 限流放在路由链最前面：超额请求在进入其余路由中间件和 handler 之前就被拒绝。
// 分组中间件由 router.Group 安装，仍然先于这里的所有中间件执行；认证通常声明
// 在分组层（group.Use("auth")），所以按用户计数时身份已经就绪。
// 契约没有声明 Spec.RateLimit 的路由不挂限流。
//
// 认证以外的中间件全部来自显式声明：Spec.Auth 只用于 OpenAPI 标记，
// adapter 不会再按 AuthRequired 自动附加任何认证中间件；需要认证的路由在
// 契约里用 group.Use("auth") 或 spec.Middleware = []string{"auth"} 声明。
func routeMiddleware(r *support.Router, spec endpoint.Spec, opts BindOptions) ([]support.Middleware, error) {
	var mw []support.Middleware
	if spec.RateLimit != nil && opts.RateLimit != nil {
		if limiter := opts.RateLimit(spec); limiter != nil {
			mw = append(mw, limiter)
		}
	}
	mw = append(mw, opts.Global...)
	routeNamed, err := resolveMiddlewareNames(r, spec.Middleware, opts)
	if err != nil {
		return nil, err
	}
	mw = append(mw, routeNamed...)
	mw = append(mw, opts.Routes[spec.OperationID]...)
	return mw, nil
}

// validateAuth checks the auth declarations of non-AuthNone routes with
// BindOptions.AuthValidator.
//
// The callback runs only after the route's middleware names resolve
// successfully; AuthNone imposes no auth requirement and is skipped, so "no
// auth" is not mistaken for "auth middleware missing". Declared names are
// merged group-first, route-second, matching execution order;
// BindOptions.Global / Routes are instances and cannot be reported by name to
// the callback.
// validateAuth 用 BindOptions.AuthValidator 校验非 AuthNone 路由的认证声明。
//
// 回调只在路由的中间件名字解析成功后调用；AuthNone 不产生认证要求，直接
// 跳过，避免把“没有认证”误判成“未安装认证”。声明名按分组在前、路由在后
// 合并，与执行顺序一致；BindOptions.Global / Routes 是实例，无法以名字形式
// 提供给回调。
func validateAuth(spec endpoint.Spec, opts BindOptions) error {
	if opts.AuthValidator == nil {
		return nil
	}
	if spec.Auth != endpoint.AuthRequired && spec.Auth != endpoint.AuthOptional {
		return nil
	}
	declared := make([]string, 0, len(opts.groupMiddleware)+len(spec.Middleware))
	declared = append(declared, opts.groupMiddleware...)
	declared = append(declared, spec.Middleware...)
	return opts.AuthValidator(spec, declared)
}

// resolveMiddlewareNames maps logical names onto concrete middleware, keeping
// their order. A name found in neither the Middleware map, the Registry, nor
// the Router's App-level registry yields an error reported at startup by
// BindGroup / BindAll, so no route goes live with missing middleware.
// resolveMiddlewareNames 把逻辑名映射成具体中间件，顺序保持不变。
// 名字在 Middleware 映射、Registry 注册表和 Router 的 App 注册表里都找不到
// 时返回错误，由 BindGroup / BindAll 在启动期上报，不会让路由带着缺失的
// 中间件上线。
func resolveMiddlewareNames(r *support.Router, names []string, opts BindOptions) ([]support.Middleware, error) {
	if len(names) == 0 {
		return nil, nil
	}
	resolved := make([]support.Middleware, 0, len(names))
	for _, name := range names {
		mw, ok := lookupMiddleware(r, name, opts)
		if !ok {
			return nil, fmt.Errorf("middleware %q is not registered in BindOptions.Middleware, BindOptions.Registry or the app middleware registry", name)
		}
		resolved = append(resolved, mw)
	}
	return resolved, nil
}

// lookupMiddleware consults the explicit map of this binding first, then the
// injected registry, and finally the App-level registry carried by the Router;
// a single binding can therefore override a same-named middleware from the
// registry.
// lookupMiddleware 先查本次绑定的显式映射，再查注入的注册表，最后查 Router
// 携带的 App 级注册表；因此单次绑定可以覆盖注册表里的同名中间件。
func lookupMiddleware(r *support.Router, name string, opts BindOptions) (support.Middleware, bool) {
	if mw, ok := opts.Middleware[name]; ok && mw != nil {
		return mw, true
	}
	if opts.Registry != nil {
		if mw, ok := opts.Registry.Lookup(name); ok && mw != nil {
			return mw, true
		}
	}
	if r != nil {
		if mw, ok := r.Middleware(name); ok && mw != nil {
			return mw, true
		}
	}
	return nil, false
}

// bindPath trims the contract path to a path relative to the current group:
//
//	Root Router:              /orders/{id} -> /orders/{id}
//	Group("/orders"):         /orders/{id} -> /{id}
//	Group("/orders"):         /orders      -> "" (the group root path)
//	Group("/api/v1/order"):   /orders/{id} -> /orders/{id} (relative already, kept as-is)
//
// When the group prefix carries the full deployment prefix, Spec.Path is
// already relative and trimming is a no-op; when the prefix is only the group
// name, Spec.Path holds the full path and this function removes the group
// level.
// bindPath 把契约中的路径裁剪成当前分组内的相对路径：
//
//	根 Router：                    /orders/{id} -> /orders/{id}
//	Group("/orders")：             /orders/{id} -> /{id}
//	Group("/orders")：             /orders      -> ""（组根路径）
//	Group("/api/v1/order")：       /orders/{id} -> /orders/{id}（相对路径，保持原样）
//
// 分组前缀携带完整部署前缀时，Spec.Path 已是相对路径，裁剪不生效；
// 分组前缀只是分组名时，Spec.Path 写完整路径，由这里裁掉分组这一层。
func bindPath(r *support.Router, specPath string) string {
	group := strings.Trim(r.GroupPrefix(), "/")
	if group == "" {
		return specPath
	}
	trimmed := strings.TrimPrefix(specPath, "/"+group)
	if trimmed == specPath || (trimmed != "" && !strings.HasPrefix(trimmed, "/")) {
		return specPath
	}
	return trimmed
}

func dynamicHandler(r *support.Router, responder support.Responder, method reflect.Value, desc endpoint.Descriptor, spec endpoint.Spec) gin.HandlerFunc {
	reqType := desc.RequestType()
	return func(c *gin.Context) {
		req := reflect.New(reqType)
		if err := r.BindAndValidate(c, req.Interface()); err != nil {
			responder.Error(c, err)
			return
		}
		out := method.Call([]reflect.Value{
			reflect.ValueOf(c.Request.Context()),
			req,
		})
		if last := out[len(out)-1]; !last.IsNil() {
			renderError(responder, c, last.Interface().(error))
			return
		}
		if spec.NoContent {
			c.Status(http.StatusNoContent)
			return
		}
		if len(out) == 0 || out[0].IsNil() {
			responder.Success(c, nil)
			return
		}
		responder.Success(c, out[0].Interface())
	}
}

func renderError(responder support.Responder, c *gin.Context, err error) {
	var fault errcode.Fault
	if errors.As(err, &fault) {
		responder.Error(c, Error(fault))
		return
	}
	responder.Error(c, err)
}

func checkSignature(method reflect.Value, reqType, resType reflect.Type, noContent bool) error {
	mt := method.Type()
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	reqPtr := reflect.PointerTo(reqType)
	if mt.NumIn() != 2 || mt.In(0) != ctxType || mt.In(1) != reqPtr {
		return fmt.Errorf("signature must be func(context.Context, *%s, ...)", reqType.Name())
	}
	if noContent {
		if mt.NumOut() != 1 || mt.Out(0) != errType {
			return errors.New("no-content signature must be func(context.Context, *Req) error")
		}
		return nil
	}
	resPtr := reflect.PointerTo(resType)
	if mt.NumOut() != 2 || mt.Out(0) != resPtr || mt.Out(1) != errType {
		return fmt.Errorf("signature must be func(context.Context, *%s) (*%s, error)", reqType.Name(), resType.Name())
	}
	return nil
}

func pathPlaceholders(path string) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	rest := path
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unclosed placeholder in %q", path)
		}
		name := rest[start+1 : start+end]
		if name == "" {
			return nil, fmt.Errorf("empty placeholder in %q", path)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate placeholder %q", name)
		}
		seen[name] = true
		out = append(out, name)
		rest = rest[start+end+1:]
	}
	return out, nil
}

func requireURIFields(reqType reflect.Type, placeholders []string) error {
	if len(placeholders) == 0 {
		return nil
	}
	fields := uriFields(reqType, make(map[reflect.Type]bool))
	for _, name := range placeholders {
		if !fields[name] {
			return fmt.Errorf("path placeholder %q has no uri-tagged field in %s", name, reqType.Name())
		}
	}
	return nil
}

func uriFields(t reflect.Type, seen map[reflect.Type]bool) map[string]bool {
	out := make(map[string]bool)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return out
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if tag := f.Tag.Get("uri"); tag != "" {
			out[strings.Split(tag, ",")[0]] = true
			continue
		}
		if f.Anonymous {
			for name := range uriFields(f.Type, seen) {
				out[name] = true
			}
		}
	}
	return out
}

func ginPath(path string) string {
	rest := path
	out := strings.Builder{}
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			out.WriteString(rest)
			break
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		out.WriteByte(':')
		out.WriteString(rest[start+1 : start+end])
		rest = rest[start+end+1:]
	}
	return out.String()
}
