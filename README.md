# gofy-interface-api

当前版本：`1.0.0`（`interfaceapi.Version`；`client.Version` 与 User-Agent 用的是同一个值）。

框架无关的契约基础设施库：`contracts/`（纯契约原语）、`client/`（泛型 HTTP
客户端）、`docgen/`（OpenAPI 生成）、`docsui/`（Swagger UI）与 `cmd/`（校验、
预览、分层引用检查）。

本库 `go.mod` **不含 gofy，也不含 gin**。把契约绑定到 gofy 框架的
`adapter/gofy` 与基于 Nacos 的服务发现 `client/nacos` 已迁至独立库
`gofy-adapter`；业务契约（`appinterface/`）与生成物（`openapi/`）
由各项目自己的契约 module 维护，生成入口是项目侧 `cmd/openapi`。

## 依赖方向

`gofy` 与本库互不依赖；`gofy-adapter` 单独承担全部框架耦合：

```
gofy  <--require--  gofy-adapter  --require-->  gofy-interface-api  <--require--  项目契约模块
```

因此本库可以独立打 tag，也能被非 gofy 框架的项目消费。

## 本地联调

本库不依赖任何本地仓库，`go.mod` 没有 `replace`，可直接：

```bash
make check     # fmt-check + vet + test-race + refcheck
```

## 分层约束

- `contracts/**` 只依赖标准库；`client/**` 不依赖任何框架；
- `docgen/**` 限于 contracts、kin-openapi、yaml；`docsui/**` 限于 swgui；
- `cmd/**` 可引用库内任意包；
- **全库禁止 import `gofy` 与 `gin`**，禁止 `func init()`。

规则由 `cmd/refcheck -mode library` 强制；项目侧用 `-mode contracts`
校验自己的 `appinterface/`。
