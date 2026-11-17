// Package docgen builds OpenAPI 3.0.3 documents from contract route
// descriptors.
//
// The generator is a pure function of its inputs: the same routes and config
// always produce byte-identical YAML and JSON.
// 包 docgen 由契约路由描述符生成 OpenAPI 3.0.3 文档。生成器是其输入的纯函数：
// 相同的路由与配置总是产出逐字节一致的 YAML 与 JSON，便于纳入版本管理与比对。
package docgen

import "strings"

const (
	// DefaultSecurityScheme is used when a route declares Auth but Config does
	// not define any scheme.
	// DefaultSecurityScheme 在路由声明了 Auth、但 Config 未定义任何 scheme 时使用。
	DefaultSecurityScheme = "bearerAuth"
)

// Server is one OpenAPI server entry.
// Server 是 OpenAPI servers 数组中的一项。
type Server struct {
	URL         string
	Description string
}

// SecurityScheme describes an OpenAPI security scheme. Only the common fields
// are modelled; unknown fields can be added by editing the generated document.
// SecurityScheme 描述一个 OpenAPI security scheme。这里只建模常用字段，其余
// 字段只能手工改生成后的文档，重新生成会被覆盖。
type SecurityScheme struct {
	Type         string
	Scheme       string
	BearerFormat string
	Name         string
	In           string
}

// Config configures Build.
// Config 是 Build 的配置，零值即全部使用默认值。
type Config struct {
	Title       string
	Version     string
	Description string
	Servers     []Server

	// SecuritySchemes maps a scheme name to its definition. When empty,
	// bearerAuth (HTTP bearer) is emitted if any route requires auth.
	// SecuritySchemes 把 scheme 名映射到定义。为空时，只要存在需要认证的路由，
	// 就生成 bearerAuth（HTTP bearer）。
	SecuritySchemes map[string]SecurityScheme

	// Prefixes maps a service key such as "demoservice" to a deployment prefix
	// such as "/api/v1/order". A route uses the prefix when one of its tags
	// matches the key or the key before the first dot. When a service mounts
	// several prefixes, declare them as "demoservice" and a more specific
	// "demoservice.product"; routes carrying the specific tag get that prefix,
	// and exact tag matches beat the service-level fallback.
	//
	// Routes added to an endpoint.Group carry their group prefix in the
	// descriptor already, and that prefix is the default. An entry here replaces
	// it while keeping the group-relative remainder, which is how the docgen CLI
	// -prefix override publishes the same routes under another base.
	// Prefixes 把服务 key（如 "demoservice"）映射到部署前缀（如 "/api/v1/order"）。
	// 路由的 tag 等于 key、或等于 key 第一个点之前的部分时使用该前缀。一个服务
	// 挂多个前缀时，同时声明 "demoservice" 与更具体的 "demoservice.product"：
	// 带具体 tag 的路由落到具体前缀，精确 tag 命中优先于服务级兜底。
	//
	// 加入 endpoint.Group 的路由已在描述符里携带分组前缀，默认以它为准；这里的
	// 同名条目会替换该前缀并保留分组内剩余路径，docgen CLI 的 -prefix 覆盖据此
	// 把同一组路由发布到另一个基址。
	Prefixes map[string]string
}

func (c Config) withDefaults() Config {
	if strings.TrimSpace(c.Title) == "" {
		c.Title = "Interface Service API"
	}
	if strings.TrimSpace(c.Version) == "" {
		c.Version = "1.0.0"
	}
	return c
}

func (c Config) securityScheme() string {
	if len(c.SecuritySchemes) == 0 {
		return DefaultSecurityScheme
	}
	// Deterministic choice: prefer bearerAuth, otherwise the first key in
	// sorted order.
	// 选择必须可复现：优先 bearerAuth，否则取字典序第一个 key。
	if _, ok := c.SecuritySchemes[DefaultSecurityScheme]; ok {
		return DefaultSecurityScheme
	}
	names := make([]string, 0, len(c.SecuritySchemes))
	for name := range c.SecuritySchemes {
		names = append(names, name)
	}
	sortStrings(names)
	return names[0]
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}
