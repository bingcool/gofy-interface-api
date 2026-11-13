package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAllowedLibraryRules(t *testing.T) {
	rules := libraryRules(".", defaultAPIModule)
	byName := make(map[string]rule, len(rules))
	for _, r := range rules {
		byName[r.name] = r
	}
	cases := []struct {
		ruleName string
		imp      string
		want     bool
	}{
		{"contracts", "context", true},
		{"contracts", defaultAPIModule + "/contracts/common/endpoint", true},
		{"contracts", "github.com/gin-gonic/gin", false},
		{"client", defaultAPIModule + "/client", true},
		{"client", "github.com/bingcool/gofy/support/nacos", false},
		{"docgen", "github.com/getkin/kin-openapi/openapi3", true},
		{"docgen", defaultAPIModule + "/docsui", false},
		{"docsui", "github.com/swaggest/swgui/v5emb", true},
		{"internal", defaultAPIModule + "/contracts/common/endpoint", true},
		{"internal", "github.com/gin-gonic/gin", false},
		{"cmd", "anything/at/all", true},
	}
	for _, tc := range cases {
		if got := allowed(tc.imp, byName[tc.ruleName]); got != tc.want {
			t.Errorf("allowed(%q, %s) = %v, want %v", tc.imp, tc.ruleName, got, tc.want)
		}
	}
	if _, ok := byName["adapter/gofy"]; ok {
		t.Fatal("the gofy adapter must no longer be a library rule")
	}
	if _, ok := byName["client/nacos"]; ok {
		t.Fatal("the nacos resolver must no longer be a library rule")
	}
}

func TestDeniedFrameworkImports(t *testing.T) {
	cases := []struct {
		imp  string
		want bool
	}{
		{"github.com/bingcool/gofy", true},
		{"github.com/bingcool/gofy/support", true},
		{"github.com/bingcool/gofy/support/nacos", true},
		{"github.com/gin-gonic/gin", true},
		{"github.com/bingcool/gofy-interface-api/contracts/common/endpoint", false},
		{"github.com/bingcool/gofy-interface-api/client", false},
		{"github.com/bingcool/gofy-interface-gofy/adapter/gofy", false},
		{"context", false},
	}
	for _, tc := range cases {
		if got := isDenied(tc.imp, forbiddenFrameworkImports); got != tc.want {
			t.Errorf("isDenied(%q) = %v, want %v", tc.imp, got, tc.want)
		}
	}
}

func TestLibraryRulesApplyFrameworkDenyEverywhere(t *testing.T) {
	for _, r := range libraryRules(".", defaultAPIModule) {
		if len(r.deny) == 0 {
			t.Errorf("%s has no framework deny list", r.name)
		}
	}
}

func TestCheckRuleRejectsForbiddenImportAndInit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "api.go", `package order

import "github.com/gin-gonic/gin"

func init() {}

var _ = gin.New
`)
	r := rule{
		name: "contracts", dirs: []string{dir},
		allow: []string{defaultAPIModule + "/contracts"},
		deny:  forbiddenFrameworkImports, checkInit: true,
	}
	problems, err := checkRule(r)
	if err != nil {
		t.Fatalf("checkRule: %v", err)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"gin-gonic", "init()"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems missing %q: %v", want, problems)
		}
	}
}

func TestContractsRuleAllowsAPIClientAndOwnPackages(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "client.go", `package demoservice

import (
	"context"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/contracts/common/paging"
	"github.com/yourorg/interface-service/appinterface/demoservice/orderdto"
)

var _ = context.Background
var _ = client.Call
var _ = paging.PageRequest{}
var _ = orderdto.OrderDTO{}
`)
	r := contractsRules(dir, "github.com/yourorg/interface-service", defaultAPIModule)[0]
	problems, err := checkRule(r)
	if err != nil {
		t.Fatalf("checkRule: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestContractsRuleRejectsFrameworkImports(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "api.go", `package demoservice

import (
	"`+adapterModule+`/adapter/gofy"
	"github.com/bingcool/gofy-interface-api/docgen"
)

var _ = gofyadapter.BindAll
var _ = docgen.Build
`)
	r := contractsRules(dir, "github.com/yourorg/interface-service", defaultAPIModule)[0]
	problems, err := checkRule(r)
	if err != nil {
		t.Fatalf("checkRule: %v", err)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"adapter/gofy", "docgen"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems missing %q: %v", want, problems)
		}
	}
}

func TestRealLibraryIsClean(t *testing.T) {
	for _, r := range libraryRules("../..", defaultAPIModule) {
		problems, err := checkRule(r)
		if err != nil {
			t.Fatalf("checkRule(%s): %v", r.name, err)
		}
		if len(problems) != 0 {
			t.Fatalf("%s problems: %v", r.name, problems)
		}
	}
}
