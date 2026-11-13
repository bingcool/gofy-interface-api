package gofyadapter

import (
	"github.com/bingcool/gofy-interface-api/contracts/common/errcode"
	"github.com/bingcool/gofy/apperr"
)

// Error converts a contract fault into a gofy application error.
// Error 把契约错误转换成 gofy 应用错误。
func Error(f errcode.Fault) *apperr.Error {
	return apperr.New(int(f.Code), f.Status, f.Message)
}

// ErrorWithDetails converts a fault and attaches field-level details.
// ErrorWithDetails 转换契约错误并附加字段级明细（通常来自校验失败）。
func ErrorWithDetails(f errcode.Fault, details map[string]string) *apperr.Error {
	return Error(f).WithDetails(details)
}
