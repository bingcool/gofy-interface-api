// Package nacos resolves service names through Nacos naming and plugs into
// client.Resolver.
//
//	resolver, err := nacos.NewFromFiles("nacos.yaml", "application.yaml")
//	c, err := client.New(client.Config{
//		Service:  demoservice.ServiceName,
//		Resolver: resolver,
//	})
//	orders := demoserviceclient.New(c)
//
// The resolver caches instance lists for CacheTTL and serves the last good
// list for up to StaleTTL while Nacos is unreachable.
// 包 nacos 通过 Nacos 命名服务解析服务名，并接入 client.Resolver。
//
// resolver 按 CacheTTL 缓存实例列表；Nacos 不可达时，最多在 StaleTTL 内继续
// 返回最近一次成功的结果。
package nacos

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gclient "github.com/bingcool/gofy-interface-api/client"
	nacosconfig "github.com/bingcool/gofy/support/nacos"
)

var (
	// ErrNoInstances is returned when Nacos has no available instance for a
	// service.
	// ErrNoInstances 在 Nacos 中该服务没有可用实例时返回。
	ErrNoInstances = errors.New("nacos resolver: no available instances")
	// ErrUnknownLoadBalancer is returned by New for an unsupported strategy.
	// ErrUnknownLoadBalancer 由 New 针对不支持的负载均衡策略返回。
	ErrUnknownLoadBalancer = errors.New("nacos resolver: unknown load balancer")
)

// Config configures a Resolver. Connection fields mirror nacos.Config;
// discovery fields mirror application.yaml nacos.discovery_service_client.
// Config 配置一个 Resolver。连接字段与 nacos.Config 对应；discovery 字段对应
// application.yaml 中的 nacos.discovery_service_client。
type Config struct {
	Addr      string
	Host      string
	Port      int
	Username  string
	Password  string
	Namespace string

	// Group defaults to the instance group, then DEFAULT_GROUP.
	// Group 默认取实例的 group，仍为空时使用 DEFAULT_GROUP。
	Group string

	// LoadBalancer is random, round_robin or weight. Default random.
	// LoadBalancer 取 random、round_robin 或 weight，默认 random。
	LoadBalancer string

	// CacheTTL keeps an instance list fresh for this long. Default 60s.
	// CacheTTL 是实例列表的保鲜时长，默认 60s。
	CacheTTL time.Duration
	// StaleTTL allows serving a stale list while Nacos is down. Default
	// max(3*CacheTTL, 180s).
	// StaleTTL 允许在 Nacos 不可用时继续返回过期列表，默认 max(3*CacheTTL, 180s)。
	StaleTTL time.Duration

	// IncludeUnhealthy asks for and returns unhealthy instances too. The
	// default keeps only healthy instances.
	// IncludeUnhealthy 为 true 时同时请求并返回不健康实例；默认只保留健康实例。
	IncludeUnhealthy bool

	// Clusters restricts discovery to the listed clusters.
	// Clusters 把服务发现范围限制在列出的集群内。
	Clusters []string
}

// ConfigFromNacos maps a loaded nacos.Config (nacos.yaml + application.yaml)
// onto resolver settings.
// ConfigFromNacos 把已加载的 nacos.Config（nacos.yaml + application.yaml）映射
// 成 Resolver 配置。
func ConfigFromNacos(cfg nacosconfig.Config) Config {
	return Config{
		Addr:             cfg.Addr,
		Host:             cfg.Host,
		Port:             cfg.Port,
		Username:         cfg.Username,
		Password:         cfg.Password,
		Namespace:        cfg.Discovery.Namespace,
		Group:            cfg.Discovery.Group,
		LoadBalancer:     cfg.Discovery.LoadBalancer,
		CacheTTL:         cfg.Discovery.CacheTTL,
		StaleTTL:         cfg.Discovery.StaleTTL,
		IncludeUnhealthy: !cfg.Discovery.HealthyOnly,
		Clusters:         append([]string(nil), cfg.Discovery.Clusters...),
	}
}

// NewFromFiles builds a resolver from the swoolefy-style pair nacos.yaml and
// application.yaml.
// NewFromFiles 从 swoolefy 风格的 nacos.yaml 与 application.yaml 组合构建
// resolver。
func NewFromFiles(nacosPath, appPath string) (*Resolver, error) {
	cfg, err := nacosconfig.Load(nacosPath, appPath)
	if err != nil {
		return nil, err
	}
	return New(ConfigFromNacos(cfg))
}

// Resolver resolves a service name to a base URL using Nacos naming.
// Resolver 使用 Nacos 命名服务把服务名解析成基地址。
type Resolver struct {
	client *nacosconfig.Client
	cfg    Config

	mu    sync.Mutex
	cache map[string]*entry
	rr    map[string]uint64
}

type entry struct {
	instances   []nacosconfig.Instance
	fetchedAt   time.Time
	lastSuccess time.Time
}

// New validates cfg and builds a resolver.
// New 校验配置并构建 resolver；未知的负载均衡策略会直接返回错误。
func New(cfg Config) (*Resolver, error) {
	conn := nacosconfig.Config{
		Addr:      cfg.Addr,
		Host:      cfg.Host,
		Port:      cfg.Port,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Namespace: cfg.Namespace,
	}
	client, err := nacosconfig.NewNamingClient(conn)
	if err != nil {
		return nil, fmt.Errorf("nacos resolver: %w", err)
	}
	if cfg.Namespace == "" {
		cfg.Namespace = client.Config().Namespace
	}
	cfg.Group = strings.TrimSpace(cfg.Group)
	if cfg.Group == "" {
		cfg.Group = nacosconfig.DefaultGroup
	}
	cfg.LoadBalancer = normalizeBalancer(cfg.LoadBalancer)
	switch cfg.LoadBalancer {
	case "random", "round_robin", "weight":
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownLoadBalancer, cfg.LoadBalancer)
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 60 * time.Second
	}
	if cfg.StaleTTL <= 0 {
		cfg.StaleTTL = max(3*cfg.CacheTTL, 180*time.Second)
	}
	if cfg.StaleTTL < cfg.CacheTTL {
		cfg.StaleTTL = max(3*cfg.CacheTTL, 180*time.Second)
	}
	return &Resolver{
		client: client,
		cfg:    cfg,
		cache:  make(map[string]*entry),
		rr:     make(map[string]uint64),
	}, nil
}

// Resolve implements client.Resolver.
// Resolve 实现 client.Resolver：缓存未过期时直接选址，否则回源 Nacos。
func (r *Resolver) Resolve(ctx context.Context, service string) (string, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		return "", errors.New("nacos resolver: service name is required")
	}

	now := time.Now()
	r.mu.Lock()
	cached := r.cache[service]
	if cached != nil && len(cached.instances) > 0 && now.Sub(cached.fetchedAt) < r.cfg.CacheTTL {
		inst := r.chooseLocked(service, cached.instances)
		r.mu.Unlock()
		return baseURL(inst), nil
	}
	r.mu.Unlock()

	instances, err := r.list(ctx, service)
	if err != nil {
		r.mu.Lock()
		if cached != nil && len(cached.instances) > 0 && now.Sub(cached.lastSuccess) <= r.cfg.StaleTTL {
			inst := r.chooseLocked(service, cached.instances)
			r.mu.Unlock()
			return baseURL(inst), nil
		}
		r.mu.Unlock()
		return "", fmt.Errorf("nacos resolver: %q: %w", service, err)
	}
	instances = r.filter(instances)
	if len(instances) == 0 {
		r.mu.Lock()
		delete(r.cache, service)
		r.mu.Unlock()
		return "", fmt.Errorf("%w: %q", ErrNoInstances, service)
	}

	r.mu.Lock()
	r.cache[service] = &entry{instances: instances, fetchedAt: now, lastSuccess: now}
	inst := r.chooseLocked(service, instances)
	r.mu.Unlock()
	return baseURL(inst), nil
}

func (r *Resolver) list(ctx context.Context, service string) ([]nacosconfig.Instance, error) {
	return r.client.ListInstances(ctx, nacosconfig.InstanceQuery{
		ServiceName: service,
		Group:       r.cfg.Group,
		Namespace:   r.cfg.Namespace,
		Clusters:    r.cfg.Clusters,
		HealthyOnly: !r.cfg.IncludeUnhealthy,
	})
}

func (r *Resolver) filter(instances []nacosconfig.Instance) []nacosconfig.Instance {
	if !r.cfg.IncludeUnhealthy {
		return nacosconfig.AvailableInstances(instances)
	}
	out := make([]nacosconfig.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.IP != "" && inst.Port > 0 && inst.Enabled {
			out = append(out, inst)
		}
	}
	return out
}

func (r *Resolver) chooseLocked(service string, instances []nacosconfig.Instance) nacosconfig.Instance {
	switch r.cfg.LoadBalancer {
	case "round_robin":
		index := r.rr[service] % uint64(len(instances))
		r.rr[service]++
		return instances[index]
	case "weight":
		return weightedPick(instances)
	default:
		return instances[rand.IntN(len(instances))]
	}
}

func weightedPick(instances []nacosconfig.Instance) nacosconfig.Instance {
	total := 0.0
	for _, inst := range instances {
		total += weight(inst.Weight)
	}
	if total <= 0 {
		return instances[rand.IntN(len(instances))]
	}
	target := rand.Float64() * total
	for _, inst := range instances {
		target -= weight(inst.Weight)
		if target <= 0 {
			return inst
		}
	}
	return instances[len(instances)-1]
}

func weight(value float64) float64 {
	if value <= 0 {
		return 1
	}
	return value
}

func baseURL(inst nacosconfig.Instance) string {
	scheme := strings.TrimSpace(inst.Metadata["scheme"])
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(inst.IP, strconv.Itoa(inst.Port))
}

func normalizeBalancer(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "random":
		return "random"
	case "round_robin", "round-robin", "roundrobin":
		return "round_robin"
	case "weight", "weighted":
		return "weight"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

var _ gclient.Resolver = (*Resolver)(nil)
