package endpoint_test

import (
	"testing"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

// TestJoinPath covers the two documented Group.Prefix forms: a plain group
// name combined with a full Spec.Path must not duplicate the overlap, while a
// deployment prefix combined with a relative path must be prepended.
// TestJoinPath 覆盖两种 Group.Prefix 形态：分组名加完整 Spec.Path 时重叠部分不能
// 重复，部署前缀加相对路径时前缀必须补上。
func TestJoinPath(t *testing.T) {
	cases := []struct {
		prefix string
		path   string
		want   string
	}{
		{"", "/items", "/items"},
		{"", "", ""},
		{"/api/v1/order", "/orders/{id}", "/api/v1/order/orders/{id}"},
		{"/api/v1/order/", "orders", "/api/v1/order/orders"},
		{"/api/test", "/api/test/items/7", "/api/test/items/7"},
		{"/api/test/", "/api/test", "/api/test"},
		{"/api/test", "/api/testing", "/api/test/api/testing"},
		{"/api/test", "", "/api/test"},
	}
	for _, tc := range cases {
		if got := endpoint.JoinPath(tc.prefix, tc.path); got != tc.want {
			t.Fatalf("JoinPath(%q, %q) = %q, want %q", tc.prefix, tc.path, got, tc.want)
		}
	}
}
