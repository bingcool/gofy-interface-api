// Package types defines JSON-safe scalar types used by contracts.
// 包 types 定义契约使用的 JSON 安全标量类型，主要解决大整数的精度问题。
package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Uint64String marshals as a JSON string to avoid JavaScript precision loss.
// In query/form/uri bindings it parses like a normal uint64.
// Uint64String 序列化为 JSON 字符串，避免超过 2^53 的整数在 JavaScript
// 中丢精度；在 query/form/uri 绑定里仍按普通 uint64 解析，无需额外配置。
type Uint64String uint64

// MarshalJSON implements json.Marshaler.
// MarshalJSON 输出带双引号的十进制字符串。
func (v Uint64String) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(v), 10) + `"`), nil
}

// UnmarshalJSON accepts a JSON string or a bare number.
// UnmarshalJSON 同时接受 JSON 字符串与裸数字，引号会被剥离；
// 负数或非法内容报错，错误信息带上原始入参便于排查。
func (v *Uint64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("types: invalid uint64 %q", s)
	}
	*v = Uint64String(n)
	return nil
}

// String returns the decimal representation.
// String 返回十进制字符串，可直接用于日志或 URL 拼接。
func (v Uint64String) String() string { return strconv.FormatUint(uint64(v), 10) }

// Uint64 returns the underlying value.
// Uint64 返回底层的 uint64 值，供需要原生整数的调用方使用。
func (v Uint64String) Uint64() uint64 { return uint64(v) }

// OpenAPIType lets docgen emit a string schema.
// OpenAPIType 让 docgen 生成 string/uint64 的 schema，文档里同样按字符串描述。
func (Uint64String) OpenAPIType() (typ, format string) { return "string", "uint64" }

// Int64String marshals as a JSON string.
// Int64String 序列化为 JSON 字符串，原因同上：超出 2^53 的 int64
// 在 JavaScript 中会丢精度，用字符串才能保证 ID 原样往返。
type Int64String int64

// MarshalJSON implements json.Marshaler.
// MarshalJSON 输出带双引号的十进制字符串。
func (v Int64String) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(v), 10) + `"`), nil
}

// UnmarshalJSON accepts a JSON string or a bare number.
// UnmarshalJSON 同时接受 JSON 字符串与裸数字，引号会被剥离；
// 非法内容返回带原始入参的错误，负值对 int64 是合法取值。
func (v *Int64String) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("types: invalid int64 %q", s)
	}
	*v = Int64String(n)
	return nil
}

// String returns the decimal representation.
// String 返回十进制字符串，负数同样带负号。
func (v Int64String) String() string { return strconv.FormatInt(int64(v), 10) }

// Int64 returns the underlying value.
// Int64 返回底层的 int64 值，供需要原生整数的调用方使用。
func (v Int64String) Int64() int64 { return int64(v) }

// OpenAPIType lets docgen emit a string schema.
// OpenAPIType 让 docgen 生成 string/int64 的 schema，文档里表现为字符串。
func (Int64String) OpenAPIType() (typ, format string) { return "string", "int64" }
