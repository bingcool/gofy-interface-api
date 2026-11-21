package docgen

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

var (
	timeType         = reflect.TypeOf(time.Time{})
	noBodyType       = reflect.TypeOf(endpoint.NoBody{})
	openAPITypeIface = reflect.TypeOf((*openAPITyper)(nil)).Elem()
)

type openAPITyper interface {
	OpenAPIType() (typ, format string)
}

type buildContext struct {
	cfg        Config
	components map[string]any
	names      map[reflect.Type]string
	used       map[string]reflect.Type
}

func newContext(cfg Config) *buildContext {
	return &buildContext{
		cfg:        cfg,
		components: make(map[string]any),
		names:      make(map[reflect.Type]string),
		used:       make(map[string]reflect.Type),
	}
}

func (c *buildContext) nameFor(t reflect.Type) string {
	if name, ok := c.names[t]; ok {
		return name
	}
	name := StableName(t)
	if existing, ok := c.used[name]; ok && existing != t {
		name += "_" + hashType(t)
	}
	c.names[t] = name
	c.used[name] = t
	return name
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func (c *buildContext) schemaFor(t reflect.Type) (map[string]any, error) {
	if t.Kind() == reflect.Ptr {
		inner, err := c.schemaFor(t.Elem())
		if err != nil {
			return nil, err
		}
		if reference, ok := inner["$ref"]; ok {
			return map[string]any{
				"allOf":    []any{map[string]any{"$ref": reference}},
				"nullable": true,
			}, nil
		}
		inner["nullable"] = true
		return inner, nil
	}
	if t == noBodyType {
		return nil, nil
	}
	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}
	if typ, format, ok := openAPITypeOf(t); ok {
		schema := map[string]any{"type": typ}
		if format != "" {
			schema["format"] = format
		}
		return schema, nil
	}

	// Widths that depend on the platform are documented as 32-bit. int and uint
	// are 64-bit on the platforms gofy targets, but every contract field using
	// them today (envelope codes, paging numbers) fits in 32 bits, and int32 is
	// the type client generators handle without surprises. A contract that
	// needs the full range must say int64 explicitly; there is no automatic
	// widening here, because the document has to describe the wire contract,
	// not the host word size.
	// 与平台相关的宽度统一按 32 位描述。gofy 目标平台上的 int/uint 是 64 位，但现有
	// 使用它们的契约字段（响应码、分页数字）都在 32 位内，而 int32 是客户端生成器处理
	// 起来最不会有意外的类型。需要完整范围时应由契约显式声明 int64；这里不做自动放宽，
	// 因为文档描述的是线上契约，而不是宿主字长。
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}, nil
	case reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "format": "int32", "minimum": 0}, nil
	case reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer", "format": "int64", "minimum": 0}, nil
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}, nil
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}, nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "binary"}, nil
		}
		item, err := c.schemaFor(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": nullableSchema(item)}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("docgen: map key must be string, got %s", t.Key())
		}
		value, err := c.schemaFor(t.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "object", "additionalProperties": nullableSchema(value)}, nil
	case reflect.Struct:
		if t.Name() == "" {
			return nil, fmt.Errorf("docgen: anonymous struct %s is not supported; declare a named type", t)
		}
		return c.structComponent(t)
	case reflect.Interface:
		return nil, fmt.Errorf("docgen: interface type %s is not supported; use a concrete type", t)
	default:
		return nil, fmt.Errorf("docgen: unsupported type %s (%s)", t, t.Kind())
	}
}

func nullableSchema(schema map[string]any) any {
	if schema == nil {
		return map[string]any{}
	}
	return schema
}

func (c *buildContext) structComponent(t reflect.Type) (map[string]any, error) {
	name := c.nameFor(t)
	if existing, ok := c.components[name]; ok {
		if existing == nil {
			return ref(name), nil
		}
		return ref(name), nil
	}
	// Reserve the name before walking fields so recursive types become $refs.
	// 先占位登记组件名再遍历字段，递归类型因此退化为 $ref，不会无限展开。
	c.components[name] = nil
	schema, err := c.structSchema(t)
	if err != nil {
		delete(c.components, name)
		delete(c.names, t)
		delete(c.used, name)
		return nil, err
	}
	c.components[name] = schema
	return ref(name), nil
}

func (c *buildContext) structSchema(t reflect.Type) (map[string]any, error) {
	properties := make(map[string]any)
	required := make([]string, 0, t.NumField())
	if err := c.appendFields(t, properties, &required); err != nil {
		return nil, err
	}
	schema := map[string]any{"type": "object"}
	if len(properties) > 0 {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		sortStrings(required)
		schema["required"] = dedupe(required)
	}
	return schema, nil
}

func (c *buildContext) appendFields(t reflect.Type, properties map[string]any, required *[]string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		name, _ := parseTag(field.Tag.Get("json"))
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			inner := field.Type
			for inner.Kind() == reflect.Ptr {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				if err := c.appendFields(inner, properties, required); err != nil {
					return err
				}
				continue
			}
		}
		if name == "" {
			name = field.Name
		}
		fieldSchema, err := c.schemaFor(field.Type)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), field.Name, err)
		}
		if fieldSchema == nil {
			continue
		}
		applyFieldTags(fieldSchema, field)
		properties[name] = fieldSchema
		if endpoint.FieldRequired(field, "json") {
			*required = append(*required, name)
		}
	}
	return nil
}

func applyFieldTags(schema map[string]any, field reflect.StructField) {
	doc := strings.TrimSpace(field.Tag.Get("doc"))
	if doc == "" {
		doc = field.Name
	}
	if doc != "" {
		schema["description"] = doc
	}
	if format := strings.TrimSpace(field.Tag.Get("format")); format != "" {
		schema["format"] = format
	}
	if raw := strings.TrimSpace(field.Tag.Get("default")); raw != "" {
		schema["default"] = parseForSchema(schema, field.Type, raw)
	}
	if raw := strings.TrimSpace(field.Tag.Get("example")); raw != "" {
		schema["example"] = parseForSchema(schema, field.Type, raw)
	}
	validate := field.Tag.Get("validate")
	if values, ok := enumFor(field, validate); ok {
		schema["enum"] = values
	}
	applyConstraints(schema, field.Type, validate)
}

func enumFor(field reflect.StructField, validate string) ([]any, bool) {
	raw := strings.TrimSpace(field.Tag.Get("enum"))
	if raw == "" {
		for _, rule := range strings.Split(validate, ",") {
			if value, ok := strings.CutPrefix(rule, "oneof="); ok {
				raw = strings.ReplaceAll(value, " ", ",")
				break
			}
		}
	}
	if raw == "" {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	values := make([]any, 0, len(parts))
	fieldType := field.Type
	for fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}
	kind := fieldType.Kind()
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch kind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n, err := strconv.ParseInt(part, 10, 64); err == nil {
				values = append(values, n)
				continue
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if n, err := strconv.ParseUint(part, 10, 64); err == nil {
				values = append(values, n)
				continue
			}
		}
		values = append(values, part)
	}
	return values, len(values) > 0
}

func applyConstraints(schema map[string]any, t reflect.Type, validate string) {
	if validate == "" {
		return
	}
	// Constraints describe the element, not the pointer: min on *string is a
	// minLength exactly like it is on string. enumFor and parseScalar already
	// dereference; this path was the one that forgot, so *string produced a
	// numeric minimum instead.
	// 约束描述的是元素而不是指针：*string 上的 min 与 string 上一样是 minLength。
	// enumFor 与 parseScalar 都已解引用，只有这条路径漏了，导致 *string 生成数字
	// minimum。
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	isString := t.Kind() == reflect.String
	isArray := t.Kind() == reflect.Slice || t.Kind() == reflect.Array
	for _, rule := range strings.Split(validate, ",") {
		key, value, has := strings.Cut(rule, "=")
		if !has {
			switch rule {
			case "uuid":
				schema["format"] = "uuid"
			case "email":
				schema["format"] = "email"
			}
			continue
		}
		switch key {
		case "min", "gte":
			if isArray {
				if n, err := strconv.Atoi(value); err == nil {
					schema["minItems"] = n
				}
			} else if isString {
				if n, err := strconv.Atoi(value); err == nil {
					schema["minLength"] = n
				}
			} else if n, err := strconv.ParseFloat(value, 64); err == nil {
				schema["minimum"] = n
			}
		case "gt":
			// validator's gt is exclusive. OpenAPI 3.0 spells that as
			// minimum + exclusiveMinimum for numbers; strings and arrays have no
			// exclusive counterpart, so the closed bound shifts by one.
			// validator 的 gt 是排他的。OpenAPI 3.0 对数字写成 minimum +
			// exclusiveMinimum；字符串与数组没有排他关键字，闭区间因此平移一。
			if isArray {
				if n, err := strconv.Atoi(value); err == nil {
					schema["minItems"] = n + 1
				}
			} else if isString {
				if n, err := strconv.Atoi(value); err == nil {
					schema["minLength"] = n + 1
				}
			} else if n, err := strconv.ParseFloat(value, 64); err == nil {
				schema["minimum"] = n
				schema["exclusiveMinimum"] = true
			}
		case "max", "lte":
			if isArray {
				if n, err := strconv.Atoi(value); err == nil {
					schema["maxItems"] = n
				}
			} else if isString {
				if n, err := strconv.Atoi(value); err == nil {
					schema["maxLength"] = n
				}
			} else if n, err := strconv.ParseFloat(value, 64); err == nil {
				schema["maximum"] = n
			}
		case "lt":
			// Same shift as gt. A bound below zero describes a value that can
			// never exist; emitting a negative maxLength/maxItems would be
			// invalid, so the constraint is left out instead.
			// 与 gt 同样的平移。小于零的边界描述的是不可能存在的值；写出负的
			// maxLength/maxItems 是非法的，因此这里不生成约束。
			if isArray {
				if n, err := strconv.Atoi(value); err == nil && n > 0 {
					schema["maxItems"] = n - 1
				}
			} else if isString {
				if n, err := strconv.Atoi(value); err == nil && n > 0 {
					schema["maxLength"] = n - 1
				}
			} else if n, err := strconv.ParseFloat(value, 64); err == nil {
				schema["maximum"] = n
				schema["exclusiveMaximum"] = true
			}
		case "minlength":
			if n, err := strconv.Atoi(value); err == nil {
				schema["minLength"] = n
			}
		case "maxlength":
			if n, err := strconv.Atoi(value); err == nil {
				schema["maxLength"] = n
			}
		case "datetime":
			schema["format"] = "date-time"
		}
	}
}

func parseForSchema(schema map[string]any, t reflect.Type, raw string) any {
	switch schema["type"] {
	case "string":
		return raw
	case "boolean":
		if value, err := strconv.ParseBool(raw); err == nil {
			return value
		}
	case "integer":
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return value
		}
	case "number":
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			return value
		}
	}
	if value, ok := parseScalar(t, raw); ok {
		return value
	}
	return raw
}

func parseScalar(t reflect.Type, raw string) (any, bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return raw, true
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		return value, err == nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, 64)
		return value, err == nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, 64)
		return value, err == nil
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, 64)
		return value, err == nil
	}
	return nil, false
}

func openAPITypeOf(t reflect.Type) (string, string, bool) {
	ptr := reflect.PointerTo(t)
	if !ptr.Implements(openAPITypeIface) && !t.Implements(openAPITypeIface) {
		return "", "", false
	}
	var value any
	if ptr.Implements(openAPITypeIface) {
		value = reflect.New(t).Interface()
	} else {
		value = reflect.Zero(t).Interface()
	}
	typ, format := value.(openAPITyper).OpenAPIType()
	if typ == "" {
		return "", "", false
	}
	return typ, format, true
}

func parseTag(tag string) (string, map[string]bool) {
	options := make(map[string]bool)
	if tag == "" {
		return "", options
	}
	parts := strings.Split(tag, ",")
	for _, option := range parts[1:] {
		if option != "" {
			options[option] = true
		}
	}
	return parts[0], options
}

func dedupe(values []string) []string {
	out := values[:0]
	for i, value := range values {
		if i == 0 || values[i-1] != value {
			out = append(out, value)
		}
	}
	return out
}
