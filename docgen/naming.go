package docgen

import (
	"fmt"
	"hash/fnv"
	"reflect"
	"strings"
	"unicode"
)

// StableName returns a deterministic component name for t.
//
// Package paths are reduced to their tail after "appinterface/" (business
// services) or "contracts/" (shared primitives under apicore/), and generic arguments are
// expanded recursively, so
//
//	paging.ListData[demoservice.OrderDTO]
//
// becomes CommonPagingListDataOfDemoserviceOrderDTO.
// StableName 返回 t 的确定性组件名：包路径被裁剪到 "appinterface/"（业务服务）
// 或 "contracts/"（apicore/ 下的共享原语）之后的部分，泛型实参递归展开，因此
// paging.ListData[demoservice.OrderDTO] 得到
// CommonPagingListDataOfDemoserviceOrderDTO。名字必须稳定，否则每次生成都会
// 改动 OpenAPI 文档里的 $ref。
func StableName(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return typeName(t)
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return typeName(t.Elem())
	case reflect.Slice, reflect.Array:
		return "ArrayOf" + typeName(t.Elem())
	case reflect.Map:
		return "MapOf" + typeName(t.Key()) + "And" + typeName(t.Elem())
	}

	name := t.Name()
	if name == "" {
		return sanitize(strings.ToLower(t.Kind().String()))
	}
	base, args, ok := splitGeneric(name)
	if !ok {
		return sanitize(packagePrefix(t.PkgPath()) + Pascal(base))
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, parseTypeExpr(arg))
	}
	return sanitize(packagePrefix(t.PkgPath()) + Pascal(base) + "Of" + strings.Join(parts, "And"))
}

// parseTypeExpr names a type expression as it appears inside reflect's
// generic instantiation string.
// parseTypeExpr 为 reflect 泛型实例化字符串中出现的类型表达式命名。
func parseTypeExpr(expr string) string {
	expr = strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(expr, "*"):
		return parseTypeExpr(expr[1:])
	case strings.HasPrefix(expr, "[]"):
		return "ArrayOf" + parseTypeExpr(expr[2:])
	case strings.HasPrefix(expr, "map["):
		end := strings.Index(expr, "]")
		if end < 0 {
			return "Map"
		}
		key := parseTypeExpr(expr[4:end])
		value := parseTypeExpr(expr[end+1:])
		return "MapOf" + key + "And" + value
	}
	if strings.Contains(expr, "[") {
		// Nested generic; recurse through the same rules.
		// 嵌套泛型，按同一套规则递归处理。
		base, args, ok := splitGeneric(expr)
		if ok {
			parts := make([]string, 0, len(args))
			for _, arg := range args {
				parts = append(parts, parseTypeExpr(arg))
			}
			return parseTypeExpr(base) + "Of" + strings.Join(parts, "And")
		}
	}
	if i := strings.LastIndexByte(expr, '.'); i >= 0 {
		pkg := expr[:i]
		typ := expr[i+1:]
		return packagePrefix(pkg) + Pascal(typ)
	}
	return Pascal(expr)
}

func splitGeneric(name string) (base string, args []string, ok bool) {
	open := strings.IndexByte(name, '[')
	if open < 0 || !strings.HasSuffix(name, "]") {
		return name, nil, false
	}
	base = name[:open]
	inner := name[open+1 : len(name)-1]
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	if start <= len(inner) {
		if last := strings.TrimSpace(inner[start:]); last != "" {
			args = append(args, last)
		}
	}
	return base, args, true
}

// packagePrefix maps a package path to a name prefix. Business service
// packages are anchored at appinterface/<service>/, so payload subpackages such
// as orderdto keep the same component names as a flat service package.
// Shared primitives keep their full suffix under apicore/contracts/.
// packagePrefix 把包路径映射为名字前缀。业务服务包以 appinterface/<service>/ 为
// 锚点，因此 orderdto 这类载荷子包与扁平服务包得到相同的组件名；apicore/
// contracts/ 下的共享原语保留完整后缀，避免不同模块的同名类型互相覆盖。
func packagePrefix(pkgPath string) string {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return ""
	}
	if i := strings.LastIndex(pkgPath, "appinterface/"); i >= 0 {
		pkgPath = pkgPath[i+len("appinterface/"):]
		if j := strings.IndexByte(pkgPath, '/'); j >= 0 {
			pkgPath = pkgPath[:j]
		}
	} else if i := strings.LastIndex(pkgPath, "contracts/"); i >= 0 {
		pkgPath = pkgPath[i+len("contracts/"):]
	} else if i := strings.LastIndexByte(pkgPath, '/'); i >= 0 {
		pkgPath = pkgPath[i+1:]
	}
	segments := strings.FieldsFunc(pkgPath, func(r rune) bool {
		return r == '/' || r == '.' || r == '-' || r == '_'
	})
	out := strings.Builder{}
	for _, segment := range segments {
		out.WriteString(Pascal(segment))
	}
	return out.String()
}

// Pascal converts a single identifier segment to PascalCase. It preserves
// existing camel humps and upper-cases the first rune.
// Pascal 把单个标识符片段转为 PascalCase，保留已有的驼峰，只把首字母大写。
func Pascal(value string) string {
	if value == "" {
		return ""
	}
	var out []rune
	upperNext := true
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upperNext = true
			continue
		}
		if upperNext {
			out = append(out, unicode.ToUpper(r))
			upperNext = false
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func sanitize(name string) string {
	if name == "" {
		return "Type"
	}
	out := strings.Builder{}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			out.WriteRune(r)
			continue
		}
		out.WriteByte('_')
	}
	result := out.String()
	if result == "" {
		return "Type"
	}
	first := rune(result[0])
	if !unicode.IsLetter(first) {
		result = "T" + result
	}
	return result
}

func hashType(t reflect.Type) string {
	h := fnv.New32a()
	_, _ = fmt.Fprint(h, t.String())
	return fmt.Sprintf("%08x", h.Sum32())
}
