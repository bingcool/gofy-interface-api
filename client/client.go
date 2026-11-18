// Package client is a framework-neutral HTTP client for interface-service
// contracts.
//
// Callers use Call[Req, Res] with a route descriptor; the client builds the
// URL, encodes the request, decodes the gofy envelope and maps business codes
// to *APIError. The package depends only on the standard library and contracts.
//
// Multipart requests are buffered in memory before they are sent, so peak
// memory is roughly body size × concurrent uploads. Config.MaxMultipartBytes
// caps one body; a larger file belongs outside this client (a signed upload
// URL), not in a Call.
// 包 client 是面向 interface-service 契约、与框架无关的 HTTP Client。
//
// 调用方配合路由描述符使用 Call[Req, Res]；Client 负责构造 URL、编码请求、
// 解析 gofy 信封并把业务码映射成 *APIError。本包只依赖标准库与契约包。
//
// multipart 请求在发送前整体缓冲在内存中，峰值约为请求体大小 × 并发上传数。
// Config.MaxMultipartBytes 限制单个请求体；更大的文件应走本客户端之外的通道
// （例如预签名上传地址），而不是放进一次 Call。
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	interfaceapi "github.com/bingcool/gofy-interface-api"
)

// Version is the client version reported in User-Agent. It is the library
// version: the client ships as part of the module, so a separate number would
// only create a second thing to bump.
// Version 是写入 User-Agent 的客户端版本。它就是库版本：客户端随模块一起发布，单独
// 一个版本号只会多出第二个需要维护的数字。
const Version = interfaceapi.Version

// NoTimeout disables the per-call deadline: only the caller's context bounds
// the request. It is a sentinel because the zero value already means "use the
// default"; pass it as Config.Timeout or to WithTimeout.
// NoTimeout 关闭每调用 deadline，只由调用方 ctx 限制请求时长。它是个哨兵值，
// 因为零值已经表示"使用默认值"；把它用作 Config.Timeout 或传给 WithTimeout。
const NoTimeout time.Duration = -1

// NoRetry disables retries. Retry keeps its zero-means-default rule, so the
// explicit "off" is a negative sentinel.
// NoRetry 关闭重试。Retry 的零值仍表示"使用默认值"，显式关闭因此用负哨兵。
const NoRetry = -1

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = 10 << 20
	// defaultMaxMultipartBytes bounds one multipart body. Multipart is encoded
	// into memory before it is sent, so without a cap a single upload is limited
	// only by available RAM; 16 MiB covers the small-file path and fails loudly
	// above it instead of at the OOM killer.
	// defaultMaxMultipartBytes 限制单个 multipart 请求体。multipart 在发送前会整体编码
	// 到内存，不设上限时单次上传只受可用内存限制；16 MiB 覆盖小文件场景，超过时明确报错，
	// 而不是等 OOM killer。
	defaultMaxMultipartBytes = 16 << 20
	defaultRetry             = 1
	defaultUserAgent         = "gofy-interface-api/" + Version
	defaultRequestIDHeader   = "X-Trace-ID"
	dialKeepAlive            = 30 * time.Second

	// maxDerivedClients bounds the WithConnectTimeout cache. Each entry holds a
	// transport of its own, so an unbounded cache would keep connection pools
	// alive for every distinct duration a caller ever passed.
	// maxDerivedClients 限制 WithConnectTimeout 缓存条数。每条都持有独立的
	// transport，不设上限就会为调用方传过的每个 duration 长期保留一个连接池。
	maxDerivedClients = 32
)

// Config configures a Client.
// Config 配置一个 Client。
type Config struct {
	// BaseURL is a fixed address such as http://order-service:8080. When it is
	// set, Resolver is ignored and retries stay on this address.
	// BaseURL 是固定地址，例如 http://order-service:8080。设置后忽略 Resolver，
	// 重试也始终留在该地址上。
	BaseURL string

	// Service is the service name used with Resolver. It is ignored when
	// BaseURL is set.
	// Service 是与 Resolver 配合使用的服务名；设置了 BaseURL 时忽略。
	Service string

	// HTTPClient may inject a custom transport. When nil a safe default is
	// used. Its Timeout field is not used; per-call contexts control deadlines.
	// HTTPClient 可注入自定义 transport；为 nil 时使用安全的默认值。其 Timeout
	// 字段不生效，超时统一由每次调用的 context 控制。
	HTTPClient *http.Client

	// Timeout bounds ordinary requests. Zero means the 30s default,
	// NoTimeout disables the deadline, and any other negative value is
	// rejected.
	// Timeout 限制普通请求的耗时。零值表示默认 30s，NoTimeout 关闭 deadline，
	// 其余负值会被拒绝。
	Timeout time.Duration

	// MaxResponseBytes limits ordinary JSON bodies. Default 10 MiB.
	// MaxResponseBytes 限制普通 JSON 响应体大小，默认 10 MiB。
	MaxResponseBytes int64

	// MaxMultipartBytes limits one encoded multipart body. Default 16 MiB. The
	// body is buffered in memory, so this is also the per-upload memory ceiling;
	// the limit is per request, not per client. A negative value is rejected.
	// MaxMultipartBytes 限制单个编码后的 multipart 请求体，默认 16 MiB。请求体会缓冲在
	// 内存中，因此它同时是单次上传的内存上限；上限按请求算，不按客户端算。负值会被拒绝。
	MaxMultipartBytes int64

	// Retry is the maximum number of retries for replayable requests.
	// Zero means the default (1, two attempts total); NoRetry disables retries;
	// any other negative value is rejected.
	// Retry 是可重放请求的最大重试次数。零值表示默认值（1，共两次尝试），
	// NoRetry 关闭重试，其余负值会被拒绝。
	Retry int

	// Resolver is used when BaseURL is empty.
	// Resolver 在 BaseURL 为空时使用，负责把服务名解析成基地址。
	Resolver Resolver

	// TokenSource returns a bearer token. Nil disables Authorization.
	// TokenSource 返回 bearer token；为 nil 时不设置 Authorization。
	TokenSource func(ctx context.Context) (string, error)

	// UserAgent defaults to gofy-interface-api/<version>.
	// UserAgent 默认 gofy-interface-api/<version>。
	UserAgent string

	// RequestIDHeader defaults to X-Trace-ID.
	// RequestIDHeader 默认 X-Trace-ID。
	RequestIDHeader string
}

// Client performs typed contract calls. It is safe for concurrent use.
// Client 执行类型化契约调用，可安全并发使用。
type Client struct {
	baseURL         string
	service         string
	httpClient      *http.Client
	timeout         time.Duration
	maxResponse     int64
	maxMultipart    int64
	retry           int
	resolver        Resolver
	tokenSource     func(ctx context.Context) (string, error)
	userAgent       string
	requestIDHeader string

	// ownHTTPClient records whether this Client built the http.Client, which is
	// what makes Close allowed to release its idle connections. A client passed
	// in through Config belongs to the caller and is left alone.
	// ownHTTPClient 记录 http.Client 是否由本 Client 构建，Close 因此可以释放它的
	// 空闲连接。通过 Config 传入的 client 归调用方所有，不做处理。
	ownHTTPClient bool

	// callMu guards callClients. A map behind a mutex rather than sync.Map: the
	// cache is bounded and needs a length check plus a clean drain on Close,
	// both of which sync.Map makes awkward. Lookups happen once per call, far
	// from the hot path a sync.Map is meant for.
	// callMu 保护 callClients。这里用 map + 互斥锁而不是 sync.Map：缓存有上限，需要
	// 长度检查，Close 时还要整体取出并清空，这两件事 sync.Map 都不好做。查找每次调用
	// 一次，根本不在 sync.Map 想优化的热点上。
	callMu      sync.Mutex
	callClients map[time.Duration]*http.Client
}

// New validates cfg and builds a Client.
// New 校验配置并构建 Client；BaseURL 与 Resolver 必须二选一。
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base != "" {
		u, err := url.Parse(base)
		if err != nil {
			return nil, fmt.Errorf("client: invalid BaseURL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("client: BaseURL must use http or https, got %q", u.Scheme)
		}
		if u.Host == "" {
			return nil, errors.New("client: BaseURL has no host")
		}
	} else {
		if cfg.Resolver == nil {
			return nil, errors.New("client: BaseURL or Resolver is required")
		}
		if strings.TrimSpace(cfg.Service) == "" {
			return nil, errors.New("client: Service is required when Resolver is used")
		}
	}

	ownHTTPClient := cfg.HTTPClient == nil
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Transport: defaultTransport()}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	} else if cfg.Timeout < 0 && cfg.Timeout != NoTimeout {
		return nil, errors.New("client: Timeout must be positive, zero (default) or NoTimeout")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	if cfg.MaxResponseBytes < 0 {
		return nil, errors.New("client: MaxResponseBytes must not be negative")
	}
	if cfg.MaxMultipartBytes == 0 {
		cfg.MaxMultipartBytes = defaultMaxMultipartBytes
	}
	if cfg.MaxMultipartBytes < 0 {
		return nil, errors.New("client: MaxMultipartBytes must not be negative")
	}
	switch {
	case cfg.Retry == NoRetry:
		cfg.Retry = 0
	case cfg.Retry < 0:
		return nil, errors.New("client: Retry must be non-negative or NoRetry")
	case cfg.Retry == 0:
		cfg.Retry = defaultRetry
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	if cfg.RequestIDHeader == "" {
		cfg.RequestIDHeader = defaultRequestIDHeader
	}

	return &Client{
		baseURL:         base,
		service:         strings.TrimSpace(cfg.Service),
		httpClient:      cfg.HTTPClient,
		timeout:         cfg.Timeout,
		maxResponse:     cfg.MaxResponseBytes,
		maxMultipart:    cfg.MaxMultipartBytes,
		retry:           cfg.Retry,
		resolver:        cfg.Resolver,
		tokenSource:     cfg.TokenSource,
		userAgent:       cfg.UserAgent,
		requestIDHeader: cfg.RequestIDHeader,
		ownHTTPClient:   ownHTTPClient,
		callClients:     make(map[time.Duration]*http.Client),
	}, nil
}

func defaultTransport() *http.Transport {
	return &http.Transport{
		// No environment proxy: these clients call service addresses resolved
		// from Nacos (a pod IP, a LAN host), and routing them through
		// HTTP_PROXY turns a dead target into the proxy's 502 instead of a
		// connection error. A caller that really needs a proxy injects its own
		// *http.Client through Config.HTTPClient.
		// 不走环境代理：这些客户端调用的是从 Nacos 解析出来的服务地址（Pod IP、内网
		// 主机），经 HTTP_PROXY 转发会把"目标已死"变成代理返回的 502，而不是连接错误。
		// 确实需要代理的调用方通过 Config.HTTPClient 注入自己的 *http.Client。
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: dialKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// httpClientFor returns the client for one call. A per-call *http.Client wins;
// otherwise a connect timeout derives a client whose transport clones the
// configured one and replaces only the dialer.
// httpClientFor 返回单次调用使用的 Client：调用级 *http.Client 优先；否则按
// 连接超时派生一个 Client，其 transport 克隆自配置值且只替换 dialer。
func (c *Client) httpClientFor(opts callOptions) (*http.Client, error) {
	if opts.httpClient != nil {
		return opts.httpClient, nil
	}
	if opts.connectTimeout == 0 {
		return c.httpClient, nil
	}
	if opts.connectTimeout < 0 {
		return nil, errors.New("client: connect timeout must not be negative")
	}
	// Fast path: a setting already in use reuses its pool without building a
	// transport we would immediately throw away.
	// 快路径：已经在用的配置直接复用连接池，不必先建一个马上就丢的 transport。
	c.callMu.Lock()
	cached, ok := c.callClients[opts.connectTimeout]
	c.callMu.Unlock()
	if ok {
		return cached, nil
	}

	base := c.httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		return nil, errors.New("client: WithConnectTimeout requires an *http.Transport")
	}
	clone := transport.Clone()
	clone.DialContext = (&net.Dialer{
		Timeout:   opts.connectTimeout,
		KeepAlive: dialKeepAlive,
	}).DialContext
	derived := *c.httpClient
	derived.Transport = clone

	c.callMu.Lock()
	if cached, ok := c.callClients[opts.connectTimeout]; ok {
		c.callMu.Unlock()
		// Another goroutine stored the same setting first; keep its client and
		// release the idle connections this clone opened while being built.
		// 另一个 goroutine 先存了同样的配置：用它的，并释放本克隆在构建期间打开的
		// 空闲连接。
		clone.CloseIdleConnections()
		return cached, nil
	}
	if len(c.callClients) < maxDerivedClients {
		c.callClients[opts.connectTimeout] = &derived
		c.callMu.Unlock()
		return &derived, nil
	}

	// The cache is full. Evict one entry and release its idle connections, then
	// store the new pool: refusing to store would hand out a transport nobody
	// ever closes, leaking a pool per distinct duration beyond the bound.
	// A pool evicted here is rebuilt (and its connections re-dialed) on demand.
	// 缓存已满。淘汰一条并释放它的空闲连接，再存入新池：拒绝写入会交出一个永远
	// 没人关闭的 transport，让超限后的每个不同取值都泄漏一个连接池。被淘汰的池
	// 在下次用到时按需重建（连接重新拨号）。
	for evicted, oldClient := range c.callClients {
		delete(c.callClients, evicted)
		oldClient.CloseIdleConnections()
		break
	}
	c.callClients[opts.connectTimeout] = &derived
	c.callMu.Unlock()
	return &derived, nil
}

// Close releases the idle connections this Client owns. Calling it more than
// once is safe, and a Client built with Config.HTTPClient leaves that client
// alone: it belongs to the caller, who may still be using it.
// Close 释放本 Client 拥有的空闲连接。重复调用安全；用 Config.HTTPClient 构建的
// Client 不会动那个 client，它归调用方所有，调用方可能还在用。
func (c *Client) Close() error {
	c.callMu.Lock()
	clients := c.callClients
	c.callClients = make(map[time.Duration]*http.Client)
	c.callMu.Unlock()

	for _, hc := range clients {
		hc.CloseIdleConnections()
	}
	if c.ownHTTPClient {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

// base returns the base URL for one call. A fixed BaseURL is returned as-is;
// otherwise the Resolver is consulted on every call, so the Resolver owns the
// only instance-list cache and its load-balancing choice applies per request.
// base 返回单次调用的基地址。固定 BaseURL 原样返回；否则每次调用都询问
// Resolver，实例列表缓存只由 Resolver 持有，负载均衡选择按请求生效。
func (c *Client) base(ctx context.Context) (string, error) {
	if c.baseURL != "" {
		return c.baseURL, nil
	}
	base, err := c.resolver.Resolve(ctx, c.service)
	if err != nil {
		return "", fmt.Errorf("client: resolve %q: %w", c.service, err)
	}
	return validateResolvedBase(base, c.service)
}

// validateResolvedBase trims a resolved address and rejects anything that is
// not an http(s) URL with a host, so bad registry metadata cannot become a
// target address.
// validateResolvedBase 去掉解析结果首尾空白，并拒绝任何不是带 host 的 http(s)
// URL 的地址，避免错误的注册中心元数据变成请求目标。
func validateResolvedBase(base, service string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("client: resolver returned an empty address for %q", service)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("client: resolver returned an invalid address %q for %q: %w", base, service, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("client: resolver returned unsupported scheme %q for %q", u.Scheme, service)
	}
	if u.Host == "" {
		return "", fmt.Errorf("client: resolver returned an address without host for %q", service)
	}
	return base, nil
}
