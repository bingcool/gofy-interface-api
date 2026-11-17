package endpoint

import "strings"

// JoinPath joins a group deployment prefix with a route path. Group.Prefix has
// two documented forms and both keep a single copy of the path: a full
// deployment prefix with a relative Spec.Path, or a plain group name with the
// full path in Spec.Path. JoinPath accepts both, so a stamped descriptor works
// whether or not Spec.Path already covers the prefix: the prefix is prepended
// only when the path does not already start with it at a segment boundary, and
// exactly one slash separates the two parts.
// JoinPath 拼接分组部署前缀与路由路径。Group.Prefix 有两种合法写法，两者都只
// 维护一份路径：完整部署前缀 + 相对 Spec.Path，或分组名 + 完整 Spec.Path。
// JoinPath 同时接受两种写法，因此已写入前缀的描述符不依赖 Spec.Path 是否包含
// 前缀：只有路径尚未按段边界以该前缀开头时才拼接，两部分之间保留且仅保留一个
// 斜杠。
func JoinPath(prefix, path string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return path
	}
	prefix = "/" + prefix
	if path == "" {
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path == prefix || strings.HasPrefix(path, prefix+"/") {
		return path
	}
	return prefix + path
}
