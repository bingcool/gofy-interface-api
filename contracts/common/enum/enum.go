// Package enum provides small helpers for string enums.
// 包 enum 提供字符串枚举的轻量辅助函数，避免每个契约重复写取值与校验逻辑。
package enum

// Values returns the declared values in order.
// Values 按声明顺序返回枚举值：order 非空时直接返回 order；
// 否则遍历 labels 收集，此时顺序不保证，需要稳定顺序时请显式传 order。
func Values[T comparable](labels map[T]string, order []T) []T {
	if len(order) > 0 {
		return order
	}
	out := make([]T, 0, len(labels))
	for v := range labels {
		out = append(out, v)
	}
	return out
}

// Valid reports whether v is declared in labels.
// Valid 判断 v 是否已在 labels 中声明，未声明的值返回 false，
// 适合在服务端入口做枚举白名单校验。
func Valid[T comparable](labels map[T]string, v T) bool {
	_, ok := labels[v]
	return ok
}

// Label returns the label for v, or the zero value when absent.
// Label 返回 v 对应的展示文案；缺失时得到空字符串而非错误，
// 调用方若需区分「未声明」应先调用 Valid。
func Label[T comparable](labels map[T]string, v T) string {
	return labels[v]
}
