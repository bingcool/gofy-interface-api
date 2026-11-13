package docgen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bingcool/gofy-interface-api/docgen"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

func withArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"openapi"}, args...)
	t.Cleanup(func() { os.Args = old })
}

func fixtureConfig() docgen.Config {
	return docgen.Config{Title: "fixture api", Version: "1.0.0"}
}

func TestCommandGeneratesAll(t *testing.T) {
	dir := t.TempDir()
	withArgs(t, "-all", "-out", dir)
	if err := docgen.Command(testcontract.Routes(), fixtureConfig()); err != nil {
		t.Fatalf("Command: %v", err)
	}
	for _, name := range []string{"all.yaml", "all.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	if err := docgen.ValidateFile(filepath.Join(dir, "all.yaml")); err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
}

func TestCommandServiceAndPrefixOverride(t *testing.T) {
	dir := t.TempDir()
	withArgs(t, "-service", testcontract.ServiceName, "-out", dir,
		"-prefix", testcontract.ServiceName+"=/api/changed", "-version", "2.0.0")
	if err := docgen.Command(testcontract.Routes(), fixtureConfig()); err != nil {
		t.Fatalf("Command: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, testcontract.ServiceName+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "2.0.0") || !strings.Contains(string(data), "/api/changed") {
		t.Fatalf("overrides not applied: %s", data[:min(len(data), 400)])
	}
}

func TestCommandValidateOnlyIgnoresRoutes(t *testing.T) {
	dir := t.TempDir()
	withArgs(t, "-all", "-out", dir)
	if err := docgen.Command(testcontract.Routes(), fixtureConfig()); err != nil {
		t.Fatalf("generate: %v", err)
	}
	withArgs(t, "-validate", filepath.Join(dir, "all.yaml"))
	if err := docgen.Command(nil, docgen.Config{}); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestCommandRejectsMissingSelector(t *testing.T) {
	withArgs(t, "-out", t.TempDir())
	if err := docgen.Command(testcontract.Routes(), fixtureConfig()); err == nil {
		t.Fatal("Command must reject a call without -all or -service")
	}
}
