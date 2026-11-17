package client

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

const (
	contentTypeJSON      = "application/json"
	contentTypeMultipart = "multipart/form-data"
)

var (
	timeType         = reflect.TypeOf(time.Time{})
	fileHeaderType   = reflect.TypeOf((*multipart.FileHeader)(nil))
	uploadFileType   = reflect.TypeOf((*UploadFile)(nil)).Elem()
	textMarshalerTyp = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// UploadFile is implemented by upload types that expose their content. The
// optional FileName/ContentType methods improve multipart metadata.
// UploadFile 由能暴露自身内容的类型实现；可选的 FileName/ContentType 方法用于
// 补充 multipart 元数据（文件名与 Content-Type）。
type UploadFile interface {
	Open() (multipart.File, error)
}

func (c *Client) buildRequest(ctx context.Context, base string, spec endpoint.Spec, req any, opts callOptions, accept string) (*http.Request, error) {
	method := strings.ToUpper(spec.Method)
	path, err := substitutePath(spec.Path, req)
	if err != nil {
		return nil, err
	}
	path = joinPathPrefix(opts.pathPrefix, path)
	target := base
	if path != "" {
		if !strings.HasPrefix(path, "/") {
			target += "/"
		}
		target += path
	}

	query := url.Values{}
	if wantsQuery(method) && req != nil {
		if err := collectForm(req, query); err != nil {
			return nil, err
		}
	}
	for key, values := range opts.query {
		for _, value := range values {
			query.Add(key, value)
		}
	}

	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("client: invalid url %q: %w", target, err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	body, contentType, err := encodeBody(spec, req)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if accept == "" {
		accept = "application/json"
	}
	httpReq.Header.Set("Accept", accept)
	httpReq.Header.Set("User-Agent", c.userAgent)

	// Headers captured at the gofy entry are written first as defaults; header
	// tags on the Request struct and CallOptions can override them afterwards.
	// gofy 在入口采集的 Header 先作为默认值写入；
	// 后面的 Request 结构体 header tag 和 CallOption 可以覆盖。
	for name, values := range HeadersFromContext(ctx) {
		httpReq.Header[name] = append([]string(nil), values...)
	}
	if httpReq.Header.Get(HeaderUserAgent) == "" {
		httpReq.Header.Set(HeaderUserAgent, DefaultPropagatedUserAgent)
	}

	if req != nil {
		if err := collectHeaders(req, httpReq.Header); err != nil {
			return nil, err
		}
		if err := collectCookies(req, httpReq.Header); err != nil {
			return nil, err
		}
	}
	for key, values := range opts.headers {
		httpReq.Header[key] = append([]string(nil), values...)
	}

	token := opts.bearer
	if token == "" && c.tokenSource != nil {
		token, err = c.tokenSource(ctx)
		if err != nil {
			return nil, fmt.Errorf("client: token source: %w", err)
		}
	}
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	requestID := opts.requestID
	if requestID == "" {
		requestID = RequestIDFromContext(ctx)
	}
	if requestID != "" {
		httpReq.Header.Set(c.requestIDHeader, requestID)
	}
	return httpReq, nil
}

// joinPathPrefix joins a caller-supplied group prefix and the route path. An
// empty prefix returns the path untouched, so ungrouped contracts pay nothing.
// joinPathPrefix 拼接调用方给出的分组前缀与路由路径；前缀为空时原样返回，未分组
// 的契约因此没有任何额外处理。
func joinPathPrefix(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if path == "" {
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return prefix + path
}

func wantsQuery(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete, http.MethodOptions:
		return true
	}
	return false
}

func encodeBody(spec endpoint.Spec, req any) ([]byte, string, error) {
	if req == nil {
		return nil, "", nil
	}
	switch strings.ToUpper(spec.Method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return nil, "", nil
	}
	contentType := strings.TrimSpace(spec.RequestContentType)
	if contentType == contentTypeMultipart {
		return encodeMultipart(req)
	}
	if contentType == "" {
		contentType = contentTypeJSON
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("client: marshal request body: %w", err)
	}
	return body, contentType, nil
}

func encodeMultipart(req any) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writeMultipart(writer, req); err != nil {
		_ = writer.Close()
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("client: close multipart: %w", err)
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

func writeMultipart(writer *multipart.Writer, req any) error {
	value := reflect.ValueOf(req)
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return errors.New("client: multipart request must be a struct")
	}
	return walkStruct(value, func(field reflect.StructField, fieldValue reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("form"))
		if name == "" || name == "-" {
			return nil
		}
		if isZero(fieldValue) {
			return nil
		}
		if file, ok := asFileHeader(fieldValue); ok {
			return writeFilePart(writer, name, file)
		}
		if upload, filename, contentType, ok := asUpload(fieldValue); ok {
			return writeUploadPart(writer, name, upload, filename, contentType)
		}
		text, err := stringify(fieldValue)
		if err != nil {
			return fmt.Errorf("multipart field %s: %w", name, err)
		}
		return writer.WriteField(name, text)
	})
}

func asFileHeader(v reflect.Value) (*multipart.FileHeader, bool) {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, false
		}
		if v.Type() == fileHeaderType {
			return v.Interface().(*multipart.FileHeader), true
		}
		v = v.Elem()
	}
	if v.Type() == fileHeaderType {
		return v.Interface().(*multipart.FileHeader), true
	}
	return nil, false
}

func asUpload(v reflect.Value) (UploadFile, string, string, bool) {
	target := v
	if target.Kind() != reflect.Ptr && target.CanAddr() {
		target = target.Addr()
	}
	if !target.Type().Implements(uploadFileType) {
		return nil, "", "", false
	}
	upload := target.Interface().(UploadFile)
	var filename, contentType string
	if named, ok := upload.(interface{ FileName() string }); ok {
		filename = named.FileName()
	}
	if typed, ok := upload.(interface{ ContentType() string }); ok {
		contentType = typed.ContentType()
	}
	return upload, filename, contentType, true
}

func writeFilePart(writer *multipart.Writer, field string, file *multipart.FileHeader) error {
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("client: open upload %s: %w", field, err)
	}
	defer func() { _ = src.Close() }()
	part, err := writer.CreateFormFile(field, file.Filename)
	if err != nil {
		return fmt.Errorf("client: create multipart part %s: %w", field, err)
	}
	if _, err := io.Copy(part, src); err != nil {
		return fmt.Errorf("client: copy upload %s: %w", field, err)
	}
	return nil
}

func writeUploadPart(writer *multipart.Writer, field string, upload UploadFile, filename, contentType string) error {
	src, err := upload.Open()
	if err != nil {
		return fmt.Errorf("client: open upload %s: %w", field, err)
	}
	defer func() { _ = src.Close() }()
	if filename == "" {
		filename = field
	}
	header := textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename)},
	}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("client: create multipart part %s: %w", field, err)
	}
	if _, err := io.Copy(part, src); err != nil {
		return fmt.Errorf("client: copy upload %s: %w", field, err)
	}
	return nil
}

func substitutePath(path string, req any) (string, error) {
	if !strings.Contains(path, "{") {
		return path, nil
	}
	if req == nil {
		return "", errors.New("client: request is required to fill path parameters")
	}
	values := make(map[string]string)
	if err := walkStruct(reflect.ValueOf(req), func(field reflect.StructField, value reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("uri"))
		if name == "" || name == "-" || isZero(value) {
			return nil
		}
		text, err := stringify(value)
		if err != nil {
			return fmt.Errorf("uri field %s: %w", name, err)
		}
		values[name] = text
		return nil
	}); err != nil {
		return "", err
	}
	out := path
	for {
		start := strings.IndexByte(out, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(out[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("client: unclosed path placeholder in %q", path)
		}
		name := out[start+1 : start+end]
		value, ok := values[name]
		if !ok || value == "" {
			return "", fmt.Errorf("client: path parameter %q is missing", name)
		}
		out = out[:start] + url.PathEscape(value) + out[start+end+1:]
	}
	return out, nil
}

func collectForm(req any, values url.Values) error {
	return walkStruct(reflect.ValueOf(req), func(field reflect.StructField, value reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("form"))
		if name == "" || name == "-" || isZero(value) {
			return nil
		}
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			for i := 0; i < value.Len(); i++ {
				item := value.Index(i)
				if isZero(item) {
					continue
				}
				text, err := stringify(item)
				if err != nil {
					return fmt.Errorf("form field %s: %w", name, err)
				}
				values.Add(name, text)
			}
			return nil
		}
		text, err := stringify(value)
		if err != nil {
			return fmt.Errorf("form field %s: %w", name, err)
		}
		values.Add(name, text)
		return nil
	})
}

func collectHeaders(req any, header http.Header) error {
	return walkStruct(reflect.ValueOf(req), func(field reflect.StructField, value reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("header"))
		if name == "" || name == "-" || isZero(value) {
			return nil
		}
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			for i := 0; i < value.Len(); i++ {
				text, err := stringify(value.Index(i))
				if err != nil {
					return fmt.Errorf("header field %s: %w", name, err)
				}
				header.Add(name, text)
			}
			return nil
		}
		text, err := stringify(value)
		if err != nil {
			return fmt.Errorf("header field %s: %w", name, err)
		}
		header.Set(name, text)
		return nil
	})
}

func collectCookies(req any, header http.Header) error {
	var pairs []string
	err := walkStruct(reflect.ValueOf(req), func(field reflect.StructField, value reflect.Value) error {
		name, _ := parseTag(field.Tag.Get("cookie"))
		if name == "" || name == "-" || isZero(value) {
			return nil
		}
		text, err := stringify(value)
		if err != nil {
			return fmt.Errorf("cookie field %s: %w", name, err)
		}
		pairs = append(pairs, name+"="+url.QueryEscape(text))
		return nil
	})
	if err != nil {
		return err
	}
	if len(pairs) > 0 {
		header.Set("Cookie", strings.Join(pairs, "; "))
	}
	return nil
}

func walkStruct(value reflect.Value, fn func(reflect.StructField, reflect.Value) error) error {
	for value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return errors.New("client: request must be a struct")
	}
	t := value.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fieldValue := value.Field(i)
		if field.Anonymous {
			inner := fieldValue
			for inner.Kind() == reflect.Ptr && !inner.IsNil() {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct && field.Tag.Get("form") == "" {
				if err := walkStruct(inner, fn); err != nil {
					return err
				}
				continue
			}
		}
		if err := fn(field, fieldValue); err != nil {
			return err
		}
	}
	return nil
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

func isZero(v reflect.Value) bool {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	}
	return v.IsZero()
}

func stringify(v reflect.Value) (string, error) {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "", nil
		}
		v = v.Elem()
	}
	if v.Type() == timeType {
		return v.Interface().(time.Time).Format(time.RFC3339), nil
	}
	if v.CanAddr() && v.Addr().Type().Implements(textMarshalerTyp) {
		text, err := v.Addr().Interface().(encoding.TextMarshaler).MarshalText()
		if err != nil {
			return "", err
		}
		return string(text), nil
	}
	if v.Type().Implements(textMarshalerTyp) {
		text, err := v.Interface().(encoding.TextMarshaler).MarshalText()
		if err != nil {
			return "", err
		}
		return string(text), nil
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported kind %s", v.Kind())
	}
}
