package client

import (
	"context"
	"net/http"
)

const (
	// HeaderTraceID is the distributed trace id for cross-service calls.
	// HeaderTraceID 是跨服务调用的分布式 trace id。
	HeaderTraceID = "X-Trace-Id"
	// HeaderUserID is the authenticated user ID.
	// HeaderUserID 是已认证用户 ID。
	HeaderUserID = "X-User-Id"
	// HeaderUserCode is the business-layer user number.
	// HeaderUserCode 是业务层的用户编号。
	HeaderUserCode = "X-User-Code"
	// HeaderTenantID is the tenant ID the request belongs to.
	// HeaderTenantID 是请求所属租户 ID。
	HeaderTenantID = "X-Tenant-Id"
	// HeaderUserName is the display name of the authenticated user.
	// HeaderUserName 是已认证用户的显示名称。
	HeaderUserName = "X-User-Name"
	// HeaderClientIP is the original caller address.
	// HeaderClientIP 是原始调用方地址。
	HeaderClientIP = "X-Client-Ip"
	// HeaderUserAgent marks the SDK that issues the request.
	// HeaderUserAgent 标记发起请求的 SDK。
	HeaderUserAgent = "X-User-Agent"

	// IdempotencyKeyHeader marks a write request as replayable. A POST, PUT or
	// PATCH carrying it is retried like a GET; without it the client refuses to
	// retry, because a duplicate write cannot be undone.
	// IdempotencyKeyHeader 标记写请求可重放：带它的 POST/PUT/PATCH 与 GET 一样重试；
	// 不带则客户端拒绝重试，因为重复的写无法撤销。
	IdempotencyKeyHeader = "Idempotency-Key"

	// DefaultPropagatedUserAgent is used as the X-User-Agent value when neither
	// the context nor the caller provides one.
	// DefaultPropagatedUserAgent 在 context 和调用方都没有提供时，
	// 作为 X-User-Agent 的默认值。
	DefaultPropagatedUserAgent = "gofy-api-sdk"
)

// ContextKey is the context key that stores propagated headers.
//
// gofy's support/headerpropagation package writes the same key, so a gofy-based
// service can hand the headers captured at its entry plus the request context to
// base(ctx) without either module importing the other. The literal is a
// cross-module API contract and must stay in sync.
// ContextKey 是存放透传 Header 的 context key。
//
// gofy 的 support/headerpropagation 包写入同一个 key，因此基于 gofy 的服务
// 可以直接把入口采集到的 Header 和 request context 交给 base(ctx)，
// 两个模块无需互相 import。该字面量是跨模块的 API 契约，必须保持一致。
const ContextKey = "gofy.header_propagation.headers"

var propagatedHeaders = map[string]bool{
	HeaderTraceID:   true,
	HeaderUserID:    true,
	HeaderUserCode:  true,
	HeaderTenantID:  true,
	HeaderUserName:  true,
	HeaderClientIP:  true,
	HeaderUserAgent: true,
}

// ContextWithHeaders merges headers into the propagated headers already present
// in ctx. Keys outside the allowlist are ignored, so callers cannot smuggle
// Authorization or Cookie downstream through this channel.
// ContextWithHeaders 把 headers 合并到 ctx 中已有的透传 Header。
// 非白名单 key 会被忽略，调用方无法通过这个通道把 Authorization 或 Cookie
// 偷带到下游。
func ContextWithHeaders(ctx context.Context, headers http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := HeadersFromContext(ctx)
	for name, values := range headers {
		canonical := http.CanonicalHeaderKey(name)
		if !propagatedHeaders[canonical] {
			continue
		}
		merged[canonical] = append([]string(nil), values...)
	}
	return context.WithValue(ctx, ContextKey, merged)
}

// ContextWithHeader merges one allowlisted header into ctx.
// ContextWithHeader 把一个白名单 Header 合并到 ctx。
func ContextWithHeader(ctx context.Context, name, value string) context.Context {
	return ContextWithHeaders(ctx, http.Header{name: {value}})
}

// HeadersFromContext returns a copy of the propagated headers in ctx.
// HeadersFromContext 返回 ctx 中透传 Header 的副本。
func HeadersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return make(http.Header)
	}
	stored, _ := ctx.Value(ContextKey).(http.Header)
	out := make(http.Header, len(stored))
	for name, values := range stored {
		canonical := http.CanonicalHeaderKey(name)
		if !propagatedHeaders[canonical] {
			continue
		}
		out[canonical] = append([]string(nil), values...)
	}
	return out
}

// HeaderFromContext returns one propagated header value, or "" when absent.
// HeaderFromContext 返回一个透传 Header 的值，不存在时返回 ""。
func HeaderFromContext(ctx context.Context, name string) string {
	return HeadersFromContext(ctx).Get(name)
}
