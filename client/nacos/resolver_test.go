package nacos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	nacosconfig "github.com/bingcool/gofy/support/nacos"
)

const (
	testListPath     = "/nacos/v1/ns/instance/list"
	testInstancePath = "/nacos/v1/ns/instance"
	testBeatPath     = "/nacos/v1/ns/instance/beat"
)

type fakeNaming struct {
	mu sync.Mutex

	hosts   []nacosconfig.Instance
	fail    bool
	calls   int
	queries []url.Values
}

func (f *fakeNaming) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.URL.Path {
	case testListPath:
		f.calls++
		f.queries = append(f.queries, r.URL.Query())
		if f.fail {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		payload, err := json.Marshal(map[string]any{"hosts": f.hosts})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(payload)

	case testInstancePath:
		query := r.URL.Query()
		port, _ := strconv.Atoi(query.Get("port"))
		switch r.Method {
		case http.MethodPost:
			inst := nacosconfig.Instance{
				IP:          query.Get("ip"),
				Port:        port,
				Weight:      1,
				Healthy:     query.Get("healthy") == "true",
				Enabled:     query.Get("enabled") == "true",
				Ephemeral:   query.Get("ephemeral") == "true",
				ClusterName: query.Get("clusterName"),
				ServiceName: query.Get("serviceName"),
				Group:       query.Get("groupName"),
				Namespace:   query.Get("namespaceId"),
			}
			replaced := false
			for i := range f.hosts {
				if f.hosts[i].IP == inst.IP && f.hosts[i].Port == inst.Port {
					f.hosts[i] = inst
					replaced = true
				}
			}
			if !replaced {
				f.hosts = append(f.hosts, inst)
			}
		case http.MethodDelete:
			kept := f.hosts[:0]
			for _, host := range f.hosts {
				if host.IP == query.Get("ip") && host.Port == port {
					continue
				}
				kept = append(kept, host)
			}
			f.hosts = kept
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "ok")

	case testBeatPath:
		fmt.Fprint(w, `{"clientBeatInterval":3000,"lightBeatEnabled":false,"code":10200}`)

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeNaming) setHosts(hosts ...nacosconfig.Instance) {
	f.mu.Lock()
	f.hosts = hosts
	f.mu.Unlock()
}

func (f *fakeNaming) setFail(fail bool) {
	f.mu.Lock()
	f.fail = fail
	f.mu.Unlock()
}

func (f *fakeNaming) snapshot() (calls int, queries []url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, append([]url.Values(nil), f.queries...)
}

func newTestResolver(t *testing.T, cfg Config) (*Resolver, *fakeNaming) {
	t.Helper()
	fake := &fakeNaming{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	if cfg.Addr == "" {
		cfg.Addr = server.URL
	}
	resolver, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return resolver, fake
}

func healthyInstance(ip string, port int, weight float64) nacosconfig.Instance {
	return nacosconfig.Instance{
		IP:      ip,
		Port:    port,
		Weight:  weight,
		Healthy: true,
		Enabled: true,
	}
}

func TestResolveCachesInstances(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{
		Namespace: "dev",
		Group:     "gofy-group",
		CacheTTL:  time.Minute,
	})
	fake.setHosts(healthyInstance("10.0.0.1", 8080, 1), healthyInstance("10.0.0.2", 8081, 1))

	first, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	if first != "http://10.0.0.1:8080" && first != "http://10.0.0.2:8081" {
		t.Fatalf("first = %q", first)
	}
	if second != "http://10.0.0.1:8080" && second != "http://10.0.0.2:8081" {
		t.Fatalf("second = %q", second)
	}
	if calls, _ := fake.snapshot(); calls != 1 {
		t.Fatalf("list calls = %d, want 1 within CacheTTL", calls)
	}
}

func TestResolveRoundRobin(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{
		LoadBalancer: "round_robin",
		CacheTTL:     time.Minute,
	})
	fake.setHosts(healthyInstance("10.0.0.1", 8080, 1), healthyInstance("10.0.0.2", 8081, 1))

	first, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	if first != "http://10.0.0.1:8080" || second != "http://10.0.0.2:8081" {
		t.Fatalf("round robin = %q, %q", first, second)
	}
}

func TestResolveFiltersUnhealthy(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{Namespace: "dev", CacheTTL: time.Minute})
	unhealthy := healthyInstance("10.0.0.2", 8081, 1)
	unhealthy.Healthy = false
	fake.setHosts(healthyInstance("10.0.0.1", 8080, 1), unhealthy)

	base, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if base != "http://10.0.0.1:8080" {
		t.Fatalf("base = %q, want the healthy instance", base)
	}
	_, queries := fake.snapshot()
	if got := queries[0].Get("healthyOnly"); got != "true" {
		t.Fatalf("healthyOnly = %q, want true", got)
	}
	if got := queries[0].Get("namespaceId"); got != "dev" {
		t.Fatalf("namespaceId = %q", got)
	}
	if got := queries[0].Get("groupName"); got != nacosconfig.DefaultGroup {
		t.Fatalf("groupName = %q", got)
	}
}

func TestResolveServesStaleWhileNacosIsDown(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{
		CacheTTL: 10 * time.Millisecond,
		StaleTTL: time.Hour,
	})
	fake.setHosts(healthyInstance("10.0.0.1", 8080, 1))

	first, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	fake.setFail(true)
	time.Sleep(20 * time.Millisecond)

	base, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("stale Resolve: %v", err)
	}
	if base != first {
		t.Fatalf("stale base = %q, want %q", base, first)
	}
}

func TestResolveExpiresStaleAfterStaleTTL(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{
		CacheTTL: 10 * time.Millisecond,
		StaleTTL: 20 * time.Millisecond,
	})
	fake.setHosts(healthyInstance("10.0.0.1", 8080, 1))
	if _, err := resolver.Resolve(context.Background(), "demo-service"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	fake.setFail(true)
	time.Sleep(50 * time.Millisecond)

	if _, err := resolver.Resolve(context.Background(), "demo-service"); err == nil {
		t.Fatal("Resolve served a list older than StaleTTL")
	}
}

func TestResolveNoInstances(t *testing.T) {
	resolver, _ := newTestResolver(t, Config{})
	if _, err := resolver.Resolve(context.Background(), "demo-service"); !errors.Is(err, ErrNoInstances) {
		t.Fatalf("err = %v, want ErrNoInstances", err)
	}
}

func TestResolveRequiresServiceName(t *testing.T) {
	resolver, _ := newTestResolver(t, Config{})
	if _, err := resolver.Resolve(context.Background(), "  "); err == nil {
		t.Fatal("Resolve accepted an empty service name")
	}
}

func TestNewRejectsUnknownLoadBalancer(t *testing.T) {
	if _, err := New(Config{LoadBalancer: "hash"}); !errors.Is(err, ErrUnknownLoadBalancer) {
		t.Fatalf("err = %v, want ErrUnknownLoadBalancer", err)
	}
}

func TestBaseURLUsesSchemeMetadata(t *testing.T) {
	resolver, fake := newTestResolver(t, Config{CacheTTL: time.Minute})
	inst := healthyInstance("10.0.0.1", 8443, 1)
	inst.Metadata = map[string]string{"scheme": "https"}
	fake.setHosts(inst)

	base, err := resolver.Resolve(context.Background(), "demo-service")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if base != "https://10.0.0.1:8443" {
		t.Fatalf("base = %q", base)
	}
}

func TestConfigFromNacosMapsDiscovery(t *testing.T) {
	cfg := nacosconfig.Config{
		Addr:      "http://10.0.0.2:8848",
		Namespace: "dev",
		Discovery: nacosconfig.DiscoveryConfig{
			Group:        "gofy-group",
			Namespace:    "dev",
			LoadBalancer: "weight",
			CacheTTL:     30 * time.Second,
			StaleTTL:     90 * time.Second,
			HealthyOnly:  false,
			Clusters:     []string{"DEFAULT"},
		},
	}
	got := ConfigFromNacos(cfg)
	if got.Addr != "http://10.0.0.2:8848" || got.Namespace != "dev" || got.Group != "gofy-group" {
		t.Fatalf("connection fields = %+v", got)
	}
	if got.LoadBalancer != "weight" || got.CacheTTL != 30*time.Second || got.StaleTTL != 90*time.Second {
		t.Fatalf("discovery fields = %+v", got)
	}
	if !got.IncludeUnhealthy {
		t.Fatal("IncludeUnhealthy = false, want true when HealthyOnly is false")
	}
	if len(got.Clusters) != 1 || got.Clusters[0] != "DEFAULT" {
		t.Fatalf("Clusters = %v", got.Clusters)
	}
}

func TestNewFromFiles(t *testing.T) {
	dir := t.TempDir()
	nacosPath := filepath.Join(dir, "nacos.yaml")
	appPath := filepath.Join(dir, "application.yaml")
	writeFile(t, nacosPath, `nacos:
  host: 10.0.0.2
  namespace: dev
`)
	writeFile(t, appPath, `nacos:
  discovery_service_client:
    load_balancer: round_robin
    namespace_id: dev
    group_name: gofy-group
`)

	resolver, err := NewFromFiles(nacosPath, appPath)
	if err != nil {
		t.Fatalf("NewFromFiles: %v", err)
	}
	if resolver.cfg.LoadBalancer != "round_robin" {
		t.Fatalf("LoadBalancer = %q", resolver.cfg.LoadBalancer)
	}
	if resolver.cfg.Group != "gofy-group" || resolver.cfg.Namespace != "dev" {
		t.Fatalf("group/namespace = %q/%q", resolver.cfg.Group, resolver.cfg.Namespace)
	}
	if resolver.cfg.CacheTTL != 60*time.Second || resolver.cfg.StaleTTL != 180*time.Second {
		t.Fatalf("cache/stale = %s/%s", resolver.cfg.CacheTTL, resolver.cfg.StaleTTL)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
