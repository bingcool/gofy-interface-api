package errcode

// Range assigns a numeric band to a business domain. CI rejects overlapping
// bands and duplicate code declarations.
// Range 为业务域分配一个数字区间；CI 会拒绝区间重叠与错误码重复声明。
// Min 与 Max 都包含在区间内，重叠判定按闭区间进行。
type Range struct {
	Domain string
	Min    Code
	Max    Code
}

// DomainRanges is the allocation table. Bands are inclusive.
// DomainRanges 是错误码区间分配表，区间为闭区间。新业务域须先在此登记，
// 再在本域号段内取码，避免与其他域冲突；40000-40999 与 50000-50999
// 已由 common 与 common_server 占用。
var DomainRanges = []Range{
	{Domain: "common", Min: 40000, Max: 40999},
	{Domain: "common_server", Min: 50000, Max: 50999},
	{Domain: "order", Min: 10000, Max: 19999},
	{Domain: "user", Min: 20000, Max: 29999},
	{Domain: "payment", Min: 30000, Max: 39999},
	{Domain: "product", Min: 60000, Max: 60999},
	{Domain: "platform", Min: 90000, Max: 99999},
}

// InRange reports whether code belongs to a declared band.
// InRange 判断 code 是否落在任一已登记区间内；CodeOK（0）不属于任何域，
// 但同样视为合法，否则成功响应会被误判为非法码。
func InRange(code Code) bool {
	for _, r := range DomainRanges {
		if code >= r.Min && code <= r.Max {
			return true
		}
	}
	return code == CodeOK
}
