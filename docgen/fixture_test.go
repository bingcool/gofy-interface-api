package docgen_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/bingcool/gofy-interface-api/docgen"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

func TestBuildWithFixtureRoutes(t *testing.T) {
	spec, err := docgen.Build(testcontract.Routes(), docgen.Config{
		Title:   "fixture api",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := spec.Validate(ctx); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	data, err := spec.YAML()
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	for _, want := range [][]byte{
		[]byte("/api/test/items/{id}"),
		[]byte("/api/test/items"),
		[]byte("GetItem"),
	} {
		if !bytes.Contains(data, want) {
			t.Fatalf("generated spec missing %q", want)
		}
	}
}

// TestBuildPrefixOverrideKeepsRemainder pins the Config.Prefixes override
// semantics: the configured entry replaces the stamped deployment prefix, and
// the group-relative remainder is re-attached below it. This keeps the docgen
// CLI -prefix override working now that descriptors carry their own prefix.
// TestBuildPrefixOverrideKeepsRemainder 固定 Config.Prefixes 的覆盖语义：配置项
// 替换 stamped 部署前缀，分组内剩余路径重新挂在其下。描述符自带前缀之后，
// docgen CLI 的 -prefix 覆盖因此仍然有效。
func TestBuildPrefixOverrideKeepsRemainder(t *testing.T) {
	spec, err := docgen.Build(testcontract.Routes(), docgen.Config{
		Title:    "fixture api",
		Version:  "1.0.0",
		Prefixes: map[string]string{testcontract.ServiceName: "/api/changed"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	data, err := spec.YAML()
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	for _, want := range [][]byte{
		[]byte("/api/changed/items/{id}"),
		[]byte("/api/changed/items"),
	} {
		if !bytes.Contains(data, want) {
			t.Fatalf("generated spec missing %q", want)
		}
	}
	if bytes.Contains(data, []byte("/api/test")) {
		t.Fatal("generated spec still contains the stamped prefix")
	}
}

func TestFilterByServiceWithFixture(t *testing.T) {
	got := docgen.FilterByService(testcontract.Routes(), testcontract.ServiceName)
	if len(got) != len(testcontract.Routes()) {
		t.Fatalf("FilterByService = %d routes, want %d", len(got), len(testcontract.Routes()))
	}
	if empty := docgen.FilterByService(testcontract.Routes(), "unknown"); len(empty) != 0 {
		t.Fatalf("unknown service returned %d routes", len(empty))
	}
}
