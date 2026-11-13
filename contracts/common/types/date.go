package types

import (
	"fmt"
	"strings"
	"time"
)

// Date is a calendar date rendered as yyyy-MM-dd.
// Date 是只含年月日的日历日期，序列化为 yyyy-MM-dd，时分秒被丢弃。
// 相比 time.Time，它更适合生日、账期这类不带时刻与时区语义的字段。
type Date struct {
	time.Time
}

const dateLayout = "2006-01-02"

// NewDate truncates t to its calendar date in t's location.
// NewDate 按 t 自身时区取年月日，时刻归零；不转成 UTC，
// 否则东八区的凌晨会被算成前一天。
func NewDate(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Time: time.Date(y, m, d, 0, 0, 0, 0, t.Location())}
}

// ParseDate parses yyyy-MM-dd.
// ParseDate 严格按 yyyy-MM-dd 解析，结果落在 UTC 零点；
// 格式不符会返回解析错误，不做宽松容错。
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, err
	}
	return Date{Time: t}, nil
}

// MarshalJSON implements json.Marshaler.
// MarshalJSON 输出带双引号的 yyyy-MM-dd 字符串。
func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.Format(dateLayout) + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
// UnmarshalJSON 接受带引号的 yyyy-MM-dd 字符串，引号会被剥离；
// 格式错误时返回带原值的包装错误，便于定位入参。
func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("types: invalid date %q: %w", s, err)
	}
	d.Time = t
	return nil
}

// MarshalText lets query/form encoders render yyyy-MM-dd.
// MarshalText 供 query/form 编码使用，输出不带引号的 yyyy-MM-dd。
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.Format(dateLayout)), nil
}

// UnmarshalText parses yyyy-MM-dd.
// UnmarshalText 解析 query/form 传入的 yyyy-MM-dd 字符串，错误原样返回。
func (d *Date) UnmarshalText(b []byte) error {
	t, err := time.Parse(dateLayout, string(b))
	if err != nil {
		return err
	}
	d.Time = t
	return nil
}

// OpenAPIType lets docgen emit a date schema.
// OpenAPIType 让 docgen 生成 string/date 的 schema，保持 OpenAPI 的日期语义。
func (Date) OpenAPIType() (typ, format string) { return "string", "date" }

// Decimal is a fixed-point number represented as a string.
// Decimal 用字符串承载定点小数，避免 float64 的精度丢失；
// 在 JSON 中表现为字符串，金额等字段前后端都应按十进制字符串处理。
type Decimal string

// OpenAPIType lets docgen emit a decimal schema.
// OpenAPIType 让 docgen 生成 string/decimal 的 schema，用于标注金额类字段格式。
func (Decimal) OpenAPIType() (typ, format string) { return "string", "decimal" }
