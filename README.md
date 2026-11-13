# gofy-interface-api

gofy 生态的契约基础设施库：`contracts/`（纯契约原语）、`client/`（泛型 HTTP 客户端）、
`adapter/gofy/`（服务端绑定）、`docgen/`（OpenAPI 生成）、`docsui/`（Swagger UI）与
`cmd/`（校验、预览、分层引用检查）。

业务契约（`appinterface/`）与生成物（`openapi/`）**不属于本库**，由各项目自己的契约
module 维护；生成入口是项目侧的 `cmd/openapi`，调用 `docgen.Command(routes, defaults)`。

## 本地联调

`go.mod` 中的 `replace github.com/bingcool/gofy => ../gofy` 要求 gofy 仓库与本库并排
checkout。发布时删除该 replace，并固定一个已发布的 gofy 最低版本。

```bash
make check     # fmt-check + vet + test-race + refcheck
```

## 分层约束

`contracts/**` 只依赖标准库；`client/**`（除 `client/nacos`）不依赖 gofy；仅
`adapter/gofy` 与 `client/nacos` 允许依赖 gofy。规则由 `cmd/refcheck -mode library`
校验，项目侧用 `-mode contracts` 校验自己的 `appinterface/`。

本库是 **gofy 生态专用**契约库（`go.mod` require gofy），不是框架无关的通用契约库。
