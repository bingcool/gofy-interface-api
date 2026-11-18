package endpoint

import (
	"reflect"
	"slices"
	"strings"
)

// FieldRequired reports whether a request field is required for one transport
// tag ("json", "form", "header" or "cookie"). Docgen and the client encoders
// share this rule, so the document and the wire request cannot drift: a required
// field is emitted even when it holds its zero value, while an optional field is
// skipped when zero.
//
// The rule keeps the historical docgen precedence: an omitempty option on the
// tag or a validate:"omitempty" makes the field optional; validate:"required"
// makes it required; a default tag makes it optional because the server can fill
// the value in; otherwise non-pointer fields are required and pointers are
// optional.
// FieldRequired 判断请求字段在某个传输 tag（"json"、"form"、"header" 或
// "cookie"）下是否为必填。docgen 与客户端编码器共用这一规则，文档与线上请求
// 因此不会漂移：必填字段即使取零值也会发送，可选字段为零值时跳过。
//
// 规则沿用 docgen 既有优先级：tag 带 omitempty 选项或 validate:"omitempty"
// 时可选；validate:"required" 时必填；带 default 时为可选，因为服务端可以
// 补默认值；其余情况下非指针字段必填、指针字段可选。
func FieldRequired(field reflect.StructField, tag string) bool {
	if hasTagOption(field.Tag.Get(tag), "omitempty") {
		return false
	}
	if strings.TrimSpace(field.Tag.Get("default")) != "" {
		return false
	}
	for _, rule := range strings.Split(field.Tag.Get("validate"), ",") {
		switch rule {
		case "required":
			return true
		case "omitempty":
			return false
		}
	}
	return field.Type.Kind() != reflect.Ptr
}

// PathParamName returns the path placeholder a request field binds to, or ""
// when the field is not a path parameter. `uri:"-"` explicitly opts out, and a
// field carrying a path placeholder is never sent as query/form parameter as
// well: the path already carries the value.
// PathParamName 返回请求字段绑定的路径占位符，字段不是路径参数时返回空串。
// `uri:"-"` 表示显式退出；绑定路径占位符的字段不会再作为 query/form 参数发送，
// 值已经由路径携带。
func PathParamName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("uri"), ",")
	if name == "-" {
		return ""
	}
	return name
}

func hasTagOption(tag, option string) bool {
	parts := strings.Split(tag, ",")
	return slices.Contains(parts[1:], option)
}
