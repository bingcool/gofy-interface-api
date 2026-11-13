# apicore/client/nacos

Nacos 服务发现实现，用于把服务名解析成 `client.Resolver` 需要的 `base_uri`。
服务端由 gofy 的 `gofy.WithNacosRegistration` 注册后，本包负责读取实例列表、
做本地缓存与负载均衡；不使用 Nacos 的服务可以直接用固定地址，无需引入本包。

## 快速开始

```go
import (
    "github.com/yourorg/interface-service/appinterface/demoservice/demoserviceclient"
    resolvernacos "github.com/bingcool/gofy-interface-api/client/nacos"
)

// nacos.yaml 提供连接信息，application.yaml 提供 discovery_service_client
resolver, err := resolvernacos.NewFromFiles("nacos.yaml", "application.yaml")
if err != nil {
    return err
}

// 固定使用 demoservice.ServiceName（"demo-service"）
orders, err := demoserviceclient.NewWithResolver(resolver)
if err != nil {
    return err
}

res, err := orders.GetOrder(ctx, &orderrequest.GetOrderRequest{ID: 42})
```

需要自定义超时、Token 或 Transport 时：

```go
import (
    "github.com/yourorg/interface-service/appinterface/demoservice"
    "github.com/yourorg/interface-service/appinterface/demoservice/demoserviceclient"
)

c, err := client.New(client.Config{
    Service:  demoservice.ServiceName,
    Resolver: resolver,
    Timeout:  5 * time.Second,
})
orders := demoserviceclient.New(c)
```

固定域名或 IP 的场景不需要本包，直接：

```go
orders, err := demoserviceclient.NewWithBaseURL("10.0.0.5:8080")
```

## Config

`New(Config)` 直接构造时可用以下字段：

| 字段 | 说明 | 默认值 |
| --- | --- | --- |
| `Addr` | Nacos 地址，优先级高于 `Host`/`Port` | `http://127.0.0.1:8848` |
| `Host` / `Port` | `Addr` 为空时使用 | `127.0.0.1` / `8848` |
| `Username` / `Password` | 开启鉴权的集群才需要 | 空 |
| `Namespace` | 命名空间 ID；为空时沿用连接配置 | 空（public） |
| `Group` | 实例分组 | `DEFAULT_GROUP` |
| `LoadBalancer` | `random`、`round_robin`、`weight` | `random` |
| `CacheTTL` | 实例列表新鲜期 | `60s` |
| `StaleTTL` | Nacos 不可达时允许使用陈旧列表的时长 | `max(3×CacheTTL, 180s)` |
| `IncludeUnhealthy` | 是否返回不健康实例 | `false` |
| `Clusters` | 只发现指定集群 | 空（全部） |

`ConfigFromNacos` 把 `github.com/bingcool/gofy/support/nacos` 解析出的
`Config.Discovery` 映射为本包配置；`NewFromFiles` 等价于
`ConfigFromNacos(nacos.Load(nacosPath, appPath))`，因此：

- 连接信息来自 `nacos.yaml`；
- `group`、`namespace`、`load_balancer`、`cache_ttl`、`stale_ttl`、`healthy_only`、
  `clusters` 来自 `application.yaml` 的 `nacos.discovery_service_client`；
- `namespace` 和 `group` 为空时继承 `service_register` 的值（由 support/nacos 完成）；
- 环境变量（`NACOS_DISCOVERY_LOAD_BALANCER`、`NACOS_DISCOVERY_CACHE_TTL` 等）
  同样由 support/nacos 在解析配置时应用。

## 解析行为

1. 缓存未过期（`CacheTTL`）时直接按策略选一个实例，不访问 Nacos；
2. 缓存过期后调用 `instance/list`，只保留可服务的实例：
   - `IncludeUnhealthy=false`：丢弃 `healthy=false`、`enabled=false`、IP/端口无效的实例；
   - `IncludeUnhealthy=true`：保留不健康实例，但仍要求 `enabled=true` 且地址有效；
3. 列表为空时清空缓存并返回 `ErrNoInstances`；
4. 请求失败时，若存在最后一次成功结果且未超过 `StaleTTL`，继续用它；
   超过 `StaleTTL` 才返回错误；
5. 实例地址默认为 `http://ip:port`；实例 `metadata.scheme=https` 时使用 `https`。

负载均衡在 `Resolver` 内完成：

- `random`：随机选择；
- `round_robin`：按服务名维护递增计数；
- `weight`：按实例 `weight` 加权随机，`weight<=0` 视为 1。

`client.Client` 不缓存单个实例地址：每次调用都会再走一次 `Resolve`，由本包按
`CacheTTL` 决定是否访问 Nacos，并重新执行负载均衡。GET/HEAD/OPTIONS/DELETE 或带
`Idempotency-Key` 的请求在传输错误、502/503/504 重试时同样会重新解析，因此一次调用
可能切换到另一个实例；固定地址模式不解析，也不会切换。

## 错误

| 错误 | 含义 |
| --- | --- |
| `ErrNoInstances` | 当前没有可用实例（包括全部被健康过滤掉） |
| `ErrUnknownLoadBalancer` | `LoadBalancer` 不是支持的策略 |

解析失败时错误会带上服务名，例如 `nacos resolver: "demo-service": ...`，
便于和 `client.APIError` 区分：前者是寻址失败，后者是业务响应码非 0。

## 与注册端配合

- 服务名必须一致：注册端 `nacos.service_register.service_name`（demo-service 为
  `demo-service`）与 `demoservice.ServiceName` 相同；
- 注册命名空间/分组与发现命名空间/分组一致，否则 `instance/list` 查不到实例；
- gofy 注册的是临时实例（ephemeral），进程正常退出会注销；异常退出后由 Nacos
  心跳过期清理，期间的陈旧列表会由 `StaleTTL` 兜底；
- `application.yaml` 的 `nacos.discovery_service_client` 只影响客户端，不影响注册。

## 测试

- `resolver_test.go`：使用假 Nacos 覆盖缓存、陈旧回退、健康过滤、三种负载均衡、
  scheme metadata、`NewFromFiles` 配置映射；
- `registration_test.go`：端到端串联 gofy 的 `Registrar` → 本包 `Resolver` →
  `demoserviceclient` 类型化调用 → 注销后返回 `ErrNoInstances`。

```bash
go test ./apicore/client/...
go test -race ./apicore/client/nacos/
```
