package testcontract_test

import (
	"net/http"
	"testing"

	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

func TestRoutesCoverRequiredShapes(t *testing.T) {
	routes := testcontract.Routes()
	if len(routes) != 5 {
		t.Fatalf("routes = %d, want 5", len(routes))
	}
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		s := r.Spec()
		if s.OperationID == "" || s.Method == "" || s.Path == "" {
			t.Fatalf("incomplete spec: %+v", s)
		}
		if s.Path[0] != '/' {
			t.Fatalf("path %q must start with /", s.Path)
		}
		if seen[s.OperationID] {
			t.Fatalf("duplicate operation id %q", s.OperationID)
		}
		seen[s.OperationID] = true
	}
	if !seen["GetItem"] || !seen["ListItems"] || !seen["CreateItem"] || !seen["CancelItem"] || !seen["GetConfig"] {
		t.Fatalf("missing operations: %v", seen)
	}
}

func TestGroupAndRateLimitDeclarations(t *testing.T) {
	if testcontract.CreateItem.Spec().RateLimit == nil {
		t.Fatal("CreateItem must declare a rate limit")
	}
	if got := testcontract.Items.Prefix; got != "/api/test" {
		t.Fatalf("Items.Prefix = %q", got)
	}
	if got := testcontract.Items.Middleware; len(got) != 2 || got[0] != "audit" || got[1] != "auth" {
		t.Fatalf("Items.Middleware = %v", got)
	}
	if got := testcontract.Items.Routes[0]; got != testcontract.GetItem {
		t.Fatal("group must keep declaration order")
	}
	if got := testcontract.GetItem.Spec().Errors[0].Status; got != http.StatusNotFound {
		t.Fatalf("declared error status = %d", got)
	}
}
