// Package client is a framework-neutral HTTP client for interface-service
// contracts.
//
// Callers use Call[Req, Res] with a route descriptor; the client builds the
// URL, encodes the request, decodes the gofy envelope and maps business codes
// to *APIError. The package depends only on the standard library and contracts.
// 包 client 是面向 interface-service 契约、与框架无关的 HTTP Client。
//
// 调用方配合路由描述符使用 Call[Req, Res]；Client 负责构造 URL、编码请求、
// 解析 gofy 信封并把业务码映射成 *APIError。本包只依赖标准库与契约包。
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
)

// Version is the client version reported in User-Agent.
// Version 是写入 User-Agent 的客户端版本号。
const Version = "0.1.0"

const (
	defaultTimeout          = 30 * time.Second
	defaultMaxResponseBytes = 10 << 20
	defaultRetry            = 1
	defaultUserAgent        = "gofy-interface-api/" + Version
	defaultRequestIDHeader  = "X-Trace-ID"
	dialKeepAlive           = 30 * time.Second
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

	// Timeout bounds ordinary requests. Default 30s.
	// Timeout 限制普通请求的耗时，默认 30s。
	Timeout time.Duration

	// MaxResponseBytes limits ordinary JSON bodies. Default 10 MiB.
	// MaxResponseBytes 限制普通 JSON 响应体大小，默认 10 MiB。
	MaxResponseBytes int64

	// Retry is the maximum number of retries for replayable requests.
	// Default 1 (two attempts total).
	// Retry 是可重放请求的最大重试次数，默认 1（共两次尝试）。
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
	retry           int
	resolver        Resolver
	tokenSource     func(ctx context.Context) (string, error)
	userAgent       string
	requestIDHeader string

	// callClients caches the clients derived by WithConnectTimeout, keyed by
	// the dial timeout, so calls with the same setting reuse one connection
	// pool instead of opening a transport per call.
	// callClients 缓存 WithConnectTimeout 派生出的 Client，以拨号超时为 key，
	// 相同配置的调用共用连接池，避免每次调用都新建 transport。
	callClients sync.Map
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

	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Transport: defaultTransport()}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("client: Timeout must not be negative")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	if cfg.MaxResponseBytes < 0 {
		return nil, errors.New("client: MaxResponseBytes must not be negative")
	}
	if cfg.Retry < 0 {
		return nil, errors.New("client: Retry must not be negative")
	}
	if cfg.Retry == 0 {
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
		retry:           cfg.Retry,
		resolver:        cfg.Resolver,
		tokenSource:     cfg.TokenSource,
		userAgent:       cfg.UserAgent,
		requestIDHeader: cfg.RequestIDHeader,
	}, nil
}

func defaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
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
	if cached, ok := c.callClients.Load(opts.connectTimeout); ok {
		return cached.(*http.Client), nil
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
	actual, _ := c.callClients.LoadOrStore(opts.connectTimeout, &derived)
	return actual.(*http.Client), nil
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
