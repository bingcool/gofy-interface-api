package docgen

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/bingcool/gofy-interface-api/contracts/common/errcode"
)

const sampleRequestID = "6f1c0b1e-7d2a-4b9c-8e1f-2a3b4c5d6e7f"

type parameter struct {
	name     string
	in       string
	required bool
	schema   map[string]any
	doc      string
	example  any
}

func (c *buildContext) operation(desc endpoint.Descriptor, spec endpoint.Spec) (map[string]any, error) {
	op := map[string]any{}
	op["operationId"] = spec.OperationID
	summary := strings.TrimSpace(spec.Summary)
	if summary == "" {
		summary = spec.Method + " " + spec.Path
	}
	op["summary"] = summary
	if spec.Description != "" {
		op["description"] = spec.Description
	}
	if len(spec.Tags) > 0 {
		op["tags"] = spec.Tags
	}
	if spec.Deprecated {
		op["deprecated"] = true
	}
	// No security block is emitted here. Authentication is installed by
	// middleware declared on the group or the route (group.Use("auth")), so the
	// contract has no auth field to translate: a route that installs "auth"
	// should also declare a 401 in Spec.Errors, which is the only place the
	// document learns about it.
	// 这里不生成 security。认证由分组或路由声明的中间件安装（group.Use("auth")），
	// 契约没有可翻译的认证字段：装了 "auth" 的路由应当在 Spec.Errors 里声明 401，
	// 那是文档得知这件事的唯一来源。
	params, err := c.parameters(desc.RequestType(), spec.Method)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.OperationID, err)
	}
	if len(params) > 0 {
		out := make([]any, 0, len(params))
		for _, p := range params {
			out = append(out, parameterObject(p))
		}
		op["parameters"] = out
	}

	body, err := c.requestBody(desc.RequestType(), spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.OperationID, err)
	}
	if body != nil {
		op["requestBody"] = body
	}

	responses, err := c.responses(desc, spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", spec.OperationID, err)
	}
	op["responses"] = responses
	return op, nil
}

func (c *buildContext) parameters(reqType reflect.Type, method string) ([]parameter, error) {
	if reqType == noBodyType {
		return nil, nil
	}
	type tagged struct {
		field reflect.StructField
		value reflect.Type
	}
	groups := map[string][]tagged{}
	err := walkFields(reqType, func(field reflect.StructField, value reflect.Type) error {
		for _, tag := range []string{"uri", "form", "header", "cookie"} {
			name := parseTagName(field.Tag.Get(tag))
			if name == "" || name == "-" {
				continue
			}
			if tag == "form" && endpoint.PathParamName(field) != "" {
				continue
			}
			groups[tag] = append(groups[tag], tagged{field: field, value: value})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []parameter
	for _, item := range groups["uri"] {
		schema, err := c.schemaFor(item.value)
		if err != nil {
			return nil, err
		}
		out = append(out, parameter{
			name:     parseTagName(item.field.Tag.Get("uri")),
			in:       "path",
			required: true,
			schema:   schema,
			doc:      docOf(item.field),
			example:  exampleOf(item.field, schema),
		})
	}
	if wantsQuery(method) {
		for _, item := range groups["form"] {
			schema, err := c.querySchema(item.value, item.field)
			if err != nil {
				return nil, err
			}
			out = append(out, parameter{
				name:     parseTagName(item.field.Tag.Get("form")),
				in:       "query",
				required: endpoint.FieldRequired(item.field, "form"),
				schema:   schema,
				doc:      docOf(item.field),
				example:  exampleOf(item.field, schema),
			})
		}
	}
	for _, item := range groups["header"] {
		schema, err := c.schemaFor(item.value)
		if err != nil {
			return nil, err
		}
		out = append(out, parameter{
			name:     parseTagName(item.field.Tag.Get("header")),
			in:       "header",
			required: endpoint.FieldRequired(item.field, "header"),
			schema:   schema,
			doc:      docOf(item.field),
			example:  exampleOf(item.field, schema),
		})
	}
	for _, item := range groups["cookie"] {
		schema, err := c.schemaFor(item.value)
		if err != nil {
			return nil, err
		}
		out = append(out, parameter{
			name:     parseTagName(item.field.Tag.Get("cookie")),
			in:       "cookie",
			required: endpoint.FieldRequired(item.field, "cookie"),
			schema:   schema,
			doc:      docOf(item.field),
			example:  exampleOf(item.field, schema),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].in != out[j].in {
			return parameterOrder(out[i].in) < parameterOrder(out[j].in)
		}
		return out[i].name < out[j].name
	})
	return out, nil
}

func parameterOrder(in string) int {
	switch in {
	case "path":
		return 0
	case "query":
		return 1
	case "header":
		return 2
	default:
		return 3
	}
}

func (c *buildContext) querySchema(t reflect.Type, field reflect.StructField) (map[string]any, error) {
	base := t
	for base.Kind() == reflect.Ptr {
		base = base.Elem()
	}
	if base.Kind() == reflect.Struct && base != timeType {
		if _, _, ok := openAPITypeOf(base); !ok {
			return nil, fmt.Errorf("query field %s is a nested struct; flatten it into scalars", field.Name)
		}
	}
	return c.schemaFor(t)
}

func responseContentType(spec endpoint.Spec, fallback string) string {
	if value := strings.TrimSpace(spec.ResponseContentType); value != "" {
		return value
	}
	return fallback
}

func parameterObject(p parameter) map[string]any {
	out := map[string]any{
		"name":     p.name,
		"in":       p.in,
		"required": p.required || p.in == "path",
		"schema":   p.schema,
	}
	if p.doc != "" {
		out["description"] = p.doc
	}
	if p.example != nil {
		out["example"] = p.example
	}
	return out
}

func (c *buildContext) requestBody(reqType reflect.Type, spec endpoint.Spec) (map[string]any, error) {
	switch strings.ToUpper(spec.Method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return nil, nil
	}
	if reqType == noBodyType {
		return nil, nil
	}
	schema, err := c.schemaFor(reqType)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, nil
	}
	contentType := strings.TrimSpace(spec.RequestContentType)
	if contentType == "" {
		contentType = "application/json"
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			contentType: map[string]any{"schema": schema},
		},
	}, nil
}

func (c *buildContext) responses(desc endpoint.Descriptor, spec endpoint.Spec) (map[string]any, error) {
	out := map[string]any{}
	var success map[string]any
	var err error
	switch {
	case spec.NoContent:
		success = map[string]any{"description": "No Content"}
	case spec.ResponseKind == endpoint.KindSSE:
		success = streamResponse("text/event-stream", "Server-sent events")
	case spec.ResponseKind == endpoint.KindDownload:
		success = streamResponse(responseContentType(spec, "application/octet-stream"), "File download")
	case spec.ResponseKind == endpoint.KindChunked:
		success = streamResponse(responseContentType(spec, "application/octet-stream"), "Chunked stream")
	default:
		success, err = c.jsonSuccess(desc.ResponseType())
		if err != nil {
			return nil, err
		}
	}
	out["200"] = success
	if spec.NoContent {
		delete(out, "200")
		out["204"] = success
	}

	faults := map[int][]endpoint.ErrorSpec{
		400: {{Code: int(errcode.CodeInvalidArgument), Status: 400, Message: "invalid argument"}},
		500: {{Code: int(errcode.CodeInternal), Status: 500, Message: "internal error"}},
	}
	if spec.RateLimit != nil {
		faults[429] = []endpoint.ErrorSpec{{Code: int(errcode.CodeTooManyRequests), Status: 429, Message: "too many requests"}}
	}
	for _, specErr := range spec.Errors {
		status := specErr.Status
		if status == 0 {
			continue
		}
		faults[status] = appendFault(faults[status], specErr)
	}

	statuses := make([]int, 0, len(faults))
	for status := range faults {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	for _, status := range statuses {
		out[strconv.Itoa(status)] = errorResponse(faults[status])
	}
	return out, nil
}

func (c *buildContext) jsonSuccess(resType reflect.Type) (map[string]any, error) {
	data, err := c.schemaFor(resType)
	if err != nil {
		return nil, err
	}
	inner := map[string]any{"type": "object"}
	if data != nil {
		inner["properties"] = map[string]any{"data": data}
	}
	return map[string]any{
		"description": "OK",
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{
					"allOf": []any{ref("CommonEnvelope"), inner},
				},
			},
		},
	}, nil
}

func streamResponse(contentType, description string) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			contentType: map[string]any{
				"schema": map[string]any{"type": "string", "format": "binary"},
			},
		},
	}
}

func errorResponse(specs []endpoint.ErrorSpec) map[string]any {
	descriptions := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.Message != "" {
			descriptions = append(descriptions, spec.Message)
		}
	}
	description := strings.Join(unique(descriptions), "; ")
	if description == "" {
		description = "error"
	}
	content := map[string]any{
		"schema": ref("CommonErrorEnvelope"),
	}
	if len(specs) == 1 {
		content["example"] = errorExample(specs[0])
	} else if len(specs) > 1 {
		examples := make(map[string]any, len(specs))
		for _, spec := range specs {
			examples[strconv.Itoa(spec.Code)] = map[string]any{
				"summary": spec.Message,
				"value":   errorExample(spec),
			}
		}
		content["examples"] = examples
	}
	return map[string]any{
		"description": description,
		"content":     map[string]any{"application/json": content},
	}
}

func errorExample(spec endpoint.ErrorSpec) map[string]any {
	return map[string]any{
		"code":   spec.Code,
		"msg":    spec.Message,
		"data":   nil,
		"req_id": sampleRequestID,
	}
}

func appendFault(specs []endpoint.ErrorSpec, spec endpoint.ErrorSpec) []endpoint.ErrorSpec {
	for _, existing := range specs {
		if existing.Code == spec.Code {
			return specs
		}
	}
	return append(specs, spec)
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := values[:0]
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func wantsQuery(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

func walkFields(t reflect.Type, fn func(reflect.StructField, reflect.Type) error) error {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("request type %s must be a struct", t)
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		if field.Anonymous {
			inner := field.Type
			for inner.Kind() == reflect.Ptr {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct && parseTagName(field.Tag.Get("form")) == "" {
				if err := walkFields(inner, fn); err != nil {
					return err
				}
				continue
			}
		}
		if err := fn(field, field.Type); err != nil {
			return err
		}
	}
	return nil
}

func parseTagName(tag string) string {
	if tag == "" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}

func docOf(field reflect.StructField) string {
	return strings.TrimSpace(field.Tag.Get("doc"))
}

func exampleOf(field reflect.StructField, schema map[string]any) any {
	raw := strings.TrimSpace(field.Tag.Get("example"))
	if raw == "" {
		return nil
	}
	return parseForSchema(schema, field.Type, raw)
}
