package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/bingcool/gofy-interface-api/client"
)

func TestContextHeadersRoundTripIsACopy(t *testing.T) {
	ctx := client.ContextWithHeaders(context.Background(), http.Header{
		"X-Tenant-Id": {"tenant-a"},
	})
	if got := client.HeaderFromContext(ctx, "X-Tenant-Id"); got != "tenant-a" {
		t.Fatalf("HeaderFromContext = %q", got)
	}
	snapshot := client.HeadersFromContext(ctx)
	snapshot.Set("X-Tenant-Id", "tenant-b")
	if got := client.HeaderFromContext(ctx, "X-Tenant-Id"); got != "tenant-a" {
		t.Fatalf("HeadersFromContext must return a copy, got %q after mutation", got)
	}
}

func TestContextWithHeaderOverrides(t *testing.T) {
	ctx := client.ContextWithHeader(context.Background(), "X-Trace-Id", "first")
	ctx = client.ContextWithHeader(ctx, "X-Trace-Id", "second")
	if got := client.HeaderFromContext(ctx, "X-Trace-Id"); got != "second" {
		t.Fatalf("HeaderFromContext = %q, want second", got)
	}
	if empty := client.HeadersFromContext(context.Background()); len(empty) != 0 {
		t.Fatalf("empty context returned %v", empty)
	}
}
