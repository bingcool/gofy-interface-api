package docgen

import (
	"reflect"
	"testing"
)

// constraintFixture covers the shapes applyConstraints has to tell apart:
// exclusive vs inclusive bounds, and values behind a pointer.
// constraintFixture 覆盖 applyConstraints 必须区分的形态：排他边界与闭区间，
// 以及藏在指针后面的值。
type constraintFixture struct {
	ExclusiveInt     int       `validate:"gt=0,lt=100"`
	InclusiveInt     int       `validate:"gte=0,lte=100"`
	ExclusiveString  string    `validate:"gt=1,lt=9"`
	ExclusiveSlice   []string  `validate:"gt=1,lt=9"`
	PointerString    *string   `validate:"min=2,max=8"`
	PointerSlice     *[]string `validate:"min=1,max=3"`
	PointerExclusive *int      `validate:"gt=0"`
	Impossible       string    `validate:"lt=0"`
	Unconstrained    string
}

// TestApplyConstraintsExclusiveAndPointers pins two correctness rules the
// generated document has to follow:
//
//  1. validator's gt/lt are exclusive, while minimum/maximum are closed. For
//     numbers OpenAPI 3.0 carries the difference in exclusiveMinimum/Maximum;
//     for strings and arrays there is no exclusive keyword, so the bound moves
//     by one. Emitting a closed bound let the document accept values the
//     server rejects.
//  2. A constraint on *string / *[]T describes the element, so it becomes
//     minLength / minItems rather than a numeric minimum.
//
// TestApplyConstraintsExclusiveAndPointers 固定生成文档必须遵守的两条：
//
//  1. validator 的 gt/lt 是排他的，而 minimum/maximum 是闭区间。数字由 OpenAPI 3.0
//     的 exclusiveMinimum/Maximum 承载差异；字符串与数组没有排他关键字，边界平移一。
//     写成闭区间会让文档接受服务端拒绝的值。
//  2. *string / *[]T 上的约束描述的是元素，应生成 minLength / minItems 而不是数字
//     minimum。
func TestApplyConstraintsExclusiveAndPointers(t *testing.T) {
	typ := reflect.TypeOf(constraintFixture{})
	cases := []struct {
		field string
		want  map[string]any
	}{
		{
			field: "ExclusiveInt",
			want: map[string]any{
				"minimum":          float64(0),
				"exclusiveMinimum": true,
				"maximum":          float64(100),
				"exclusiveMaximum": true,
			},
		},
		{
			field: "InclusiveInt",
			want:  map[string]any{"minimum": float64(0), "maximum": float64(100)},
		},
		{
			field: "ExclusiveString",
			want:  map[string]any{"minLength": 2, "maxLength": 8},
		},
		{
			field: "ExclusiveSlice",
			want:  map[string]any{"minItems": 2, "maxItems": 8},
		},
		{
			field: "PointerString",
			want:  map[string]any{"minLength": 2, "maxLength": 8},
		},
		{
			field: "PointerSlice",
			want:  map[string]any{"minItems": 1, "maxItems": 3},
		},
		{
			field: "PointerExclusive",
			want:  map[string]any{"minimum": float64(0), "exclusiveMinimum": true},
		},
		{
			// Empty length < 0 can never hold; a negative maxLength would be
			// invalid, so no constraint is written at all.
			// 长度 < 0 永远不成立；负的 maxLength 非法，因此不写任何约束。
			field: "Impossible",
			want:  map[string]any{},
		},
		{
			field: "Unconstrained",
			want:  map[string]any{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			field, ok := typ.FieldByName(tc.field)
			if !ok {
				t.Fatalf("fixture has no field %s", tc.field)
			}
			schema := map[string]any{}
			applyConstraints(schema, field.Type, field.Tag.Get("validate"))
			if len(schema) != len(tc.want) {
				t.Fatalf("schema = %#v, want %#v", schema, tc.want)
			}
			for key, want := range tc.want {
				got, ok := schema[key]
				if !ok {
					t.Errorf("missing %q: %#v", key, schema)
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}
