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

func TestFilterByServiceWithFixture(t *testing.T) {
	got := docgen.FilterByService(testcontract.Routes(), testcontract.ServiceName)
	if len(got) != len(testcontract.Routes()) {
		t.Fatalf("FilterByService = %d routes, want %d", len(got), len(testcontract.Routes()))
	}
	if empty := docgen.FilterByService(testcontract.Routes(), "unknown"); len(empty) != 0 {
		t.Fatalf("unknown service returned %d routes", len(empty))
	}
}
