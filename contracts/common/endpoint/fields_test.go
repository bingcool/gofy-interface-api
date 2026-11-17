package endpoint_test

import (
	"reflect"
	"testing"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

type fieldFixture struct {
	Name     string  `json:"name"`
	Note     string  `json:"note,omitempty"`
	Page     string  `form:"page" default:"1"`
	Owner    string  `form:"owner" validate:"required"`
	OwnerPtr *string `form:"owner" validate:"required"`
	Status   string  `form:"status" validate:"omitempty,oneof=paid unpaid"`
	Tag      *string `form:"tag"`
	Forced   string  `form:"force,omitempty" validate:"required"`
	ID       uint64  `uri:"id" form:"id"`
	Skip     string  `uri:"-"`
	NoURI    string
}

func fieldByName(t *testing.T, name string) reflect.StructField {
	t.Helper()
	field, ok := reflect.TypeOf(fieldFixture{}).FieldByName(name)
	if !ok {
		t.Fatalf("fixture field %q is missing", name)
	}
	return field
}

// TestFieldRequired pins the precedence shared by docgen and the encoder: an
// omitempty tag option wins over validate:"required", then an explicit default,
// then the validate rules, and a non-pointer field is required by default.
// TestFieldRequired 固定 docgen 与编码器共用的判定优先级：omitempty 标签选项优先于
// validate:"required"，其次是显式 default，再次是 validate 规则；没有声明时非指针
// 字段默认必填。
func TestFieldRequired(t *testing.T) {
	cases := []struct {
		field string
		tag   string
		want  bool
	}{
		{"Name", "json", true},
		{"Note", "json", false},
		{"Page", "form", false},
		{"Owner", "form", true},
		{"OwnerPtr", "form", true},
		{"Status", "form", false},
		{"Tag", "form", false},
		{"Forced", "form", false},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			if got := endpoint.FieldRequired(fieldByName(t, tc.field), tc.tag); got != tc.want {
				t.Fatalf("FieldRequired(%s, %q) = %v, want %v", tc.field, tc.tag, got, tc.want)
			}
		})
	}
}

func TestPathParamName(t *testing.T) {
	if got := endpoint.PathParamName(fieldByName(t, "ID")); got != "id" {
		t.Fatalf("PathParamName(ID) = %q, want id", got)
	}
	if got := endpoint.PathParamName(fieldByName(t, "Skip")); got != "" {
		t.Fatalf("PathParamName(Skip) = %q, want empty", got)
	}
	if got := endpoint.PathParamName(fieldByName(t, "NoURI")); got != "" {
		t.Fatalf("PathParamName(NoURI) = %q, want empty", got)
	}
}
