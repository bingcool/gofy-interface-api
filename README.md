# gofy-interface-api

当前版本：`1.0.0`（`interfaceapi.Version`；`client.Version` 与 User-Agent 用的是同一个值）。

框架无关的契约基础设施库：`contracts/`（纯契约原语）、`client/`（泛型 HTTP
客户端）、`docgen/`（OpenAPI 生成）、`docsui/`（Swagger UI）与 `cmd/`（校验、
预览、分层引用检查）。

本库 `go.mod` **不含 gofy、gin 与 gofy-adapter**。把契约绑定到 gofy 框架的
`adapter/gofy` 与基于 Nacos 的服务发现 `client/nacos` 已迁至独立库
`gofy-adapter`；业务契约（`appinterface/`）与生成物（`openapi/`）
由各项目自己的契约 module 维护，生成入口是项目侧 `cmd/openapi`。

## 依赖方向

`gofy` 与本库互不依赖；`gofy-adapter` 单独承担全部框架耦合：

```
gofy  <--require--  gofy-adapter  --require-->  gofy-interface-api  <--require--  项目契约模块
```

因此本库可以独立打 tag，也能被非 gofy 框架的项目消费。

## 分组前缀

`endpoint.NewRouteGroup` / `Group.Add` 在声明阶段把分组 `Prefix` 写入每条
描述符的 `Spec.Prefix`；`client.Call`（含 `StreamSSE`、`DownloadFile`、
`StreamChunked`）用 `endpoint.JoinPath` 自动拼接，调用方无需逐次传前缀。
`WithPathPrefix` 只作为更外层的挂载前缀（例如网关子路径）。
`docgen.Config.Prefixes` 命中标签时替换 stamped 前缀并保留组内剩余路径，
CLI `-prefix` 覆盖因此继续可用；`gofy-adapter` 的 `BindGroup` 在启动期断言
stamped 前缀与分组一致。

## 必填字段

`endpoint.FieldRequired` 是 OpenAPI `required` 与客户端编码器的唯一判定源：
`omitempty` 标签选项或 `default` tag → 可选；`validate:"required"` → 必填；
`validate:"omitempty"` → 可选；其余非指针字段默认必填。必填字段的零值也会
发送（multipart 中缺失的文件除外），`uri` 标签字段不会再重复进 query。

指针字段的"存在"与"零值"是两件事：非 nil 指针一律发送，`*int(0)` 与
`*bool(false)` 是调用方明确给出的取值；只有 nil 才表示未提供。`uri` 路径参数
同理——`0` 和 `false` 是值，不是缺失，只有 nil 指针或空字符串才算没给
（后者报 `path parameter "x" is missing`）。

## 本地联调

本库不依赖任何本地仓库，`go.mod` 没有 `replace`，可直接：

```bash
make check     # fmt-check + vet + test-race + refcheck
```

## 分层约束

- `contracts/**` 只依赖标准库；`client/**` 不依赖任何框架；
- `docgen/**` 限于 contracts、kin-openapi、yaml；`docsui/**` 限于 swgui；
- `cmd/**` 可引用库内任意包；
- **全库禁止 import `gofy`、`gin` 与 `gofy-adapter`**（adapter 依赖本库，
  反向引用既是循环依赖也是分层违规），禁止 `func init()`。

规则由 `cmd/refcheck -mode library` 强制；项目侧用 `-mode contracts`
校验自己的 `appinterface/`。

## 超时、重试与空 data

`Config.Timeout` / `Config.Retry` 的零值表示默认值；显式关闭用哨兵
`client.NoTimeout` 与 `client.NoRetry`（值为 `-1`），其余负值在 `New` 时
被拒绝。`WithTimeout(0)` 与 `NoTimeout` 等效：本次调用只受调用方 context
限制。

成功信封没有 `data`（缺失或为 `null`）时，只有 `NoContent` 路由、
`endpoint.NoBody` 占位响应与指针响应类型返回 `(nil, nil)`；值类型响应得到
`*ProtocolError`，不再把调用方即将解引用的空指针交出去。

## 流式调用

`StreamSSE` / `DownloadFile` / `StreamChunked` 只接受对应 `ResponseKind`
（`sse` / `download` / `chunked`）的路由；接错 helper 在发请求之前返回
`ErrResponseKindMismatch`。

2xx 响应还要通过 `Content-Type` 检查，避免 gofy 错误信封被当成数据：

- SSE 必须是 `text/event-stream`（允许带 charset 等参数）；
- 下载/分块在契约声明 `ResponseContentType` 时精确匹配，`*/*` 与
  `type/*` 通配可用；未声明时接受任意非 JSON 类型，但拒绝
  `application/json` 与 `+json`。

SSE 的正常 EOF 视为断连：按 `Retry` 重新解析地址，并带 `Last-Event-ID`
续传；重试预算用尽后向错误 channel 投递 `ErrStreamEnded`。主动停止请取消
context，此时流直接结束且不报错。

