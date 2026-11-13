// Package envelope defines the wire envelope shared by every contract.
//
// Contract responses describe only the data payload; the envelope is applied by
// the server adapter and parsed by the client SDK.
// 包 envelope 定义所有契约共用的传输信封。契约响应只描述数据载荷，
// 信封由服务端 adapter 填充、由客户端 SDK 解析。
package envelope

// Envelope is the shape of a successful gofy response.
// Envelope 是 gofy 成功响应的结构：Code 为 0 表示成功，Data 装载契约数据，
// ReqID 透传链路追踪号，Details 承载字段级校验明细（可省略）。
type Envelope[T any] struct {
	Code    int               `json:"code"`
	Msg     string            `json:"msg"`
	Data    T                 `json:"data"`
	ReqID   string            `json:"req_id,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// ErrorEnvelope is the shape of a failed gofy response.
// ErrorEnvelope 是 gofy 失败响应的结构：Code 为非 0 的业务错误码，
// Msg 为可直接展示的提示，Data 通常为空，Details 存放字段级错误信息。
type ErrorEnvelope struct {
	Code    int               `json:"code"`
	Msg     string            `json:"msg"`
	Data    any               `json:"data"`
	ReqID   string            `json:"req_id,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}
