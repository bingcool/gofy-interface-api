package endpoint

// HTTP method constructors: method and path are passed directly and the
// remaining metadata is set in the build callback, so route files read like Gin
// route registration:
//
// The generic parameters are still the request/response types, which Go cannot
// infer from the path; store the return value in a package-level variable when
// it is reused. A nil build sets only Method and Path.
// HTTP 方法构造器：方法与路径直接作为参数，其余元数据在 build 回调里设置，
// 路由文件读起来与 Gin 注册路由一致：
//
//	var GetOrder = endpoint.Get[orderrequest.GetOrderRequest, orderresponse.GetOrderResponse](
//	    "/orders/{id}", func(spec *endpoint.Spec) {
//	        spec.OperationID = "GetOrder"
//	        spec.Auth = endpoint.AuthRequired
//	    })
//
// 泛型参数仍是请求/响应类型，Go 无法从路径推断；需要复用时照旧把返回值存成
// 包级变量。build 为 nil 时只设置 Method 与 Path。

// Get declares a GET route.
// Get 声明一条 GET 路由。
func Get[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("GET", path, build)
}

// Post declares a POST route.
// Post 声明一条 POST 路由。
func Post[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("POST", path, build)
}

// Put declares a PUT route.
// Put 声明一条 PUT 路由。
func Put[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("PUT", path, build)
}

// Patch declares a PATCH route.
// Patch 声明一条 PATCH 路由。
func Patch[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("PATCH", path, build)
}

// Delete declares a DELETE route.
// Delete 声明一条 DELETE 路由。
func Delete[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("DELETE", path, build)
}

// Head declares a HEAD route.
// Head 声明一条 HEAD 路由。
func Head[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("HEAD", path, build)
}

// Options declares an OPTIONS route.
// Options 声明一条 OPTIONS 路由。
func Options[Req, Res any](path string, build func(*Spec)) *Route[Req, Res] {
	return newRoute[Req, Res]("OPTIONS", path, build)
}

// newRoute fills in method and path first, then lets build override the remaining
// metadata.
// newRoute 先填好方法与路径，再让 build 覆盖其余元数据。
func newRoute[Req, Res any](method, path string, build func(*Spec)) *Route[Req, Res] {
	spec := Spec{Method: method, Path: path}
	if build != nil {
		build(&spec)
	}
	return New[Req, Res](spec)
}
