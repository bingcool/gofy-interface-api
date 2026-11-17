package client

import "testing"

// TestUserAgentCarriesLibraryVersion checks the string that actually goes on
// the wire, not just the constant it is built from: a service reading its
// dependency versions from traffic should see the module version there.
// TestUserAgentCarriesLibraryVersion 检查真正发到线上的字符串，而不只是拼它的常量：
// 从流量里判断依赖版本的服务，应该在那里看到模块版本。
func TestUserAgentCarriesLibraryVersion(t *testing.T) {
	if defaultUserAgent != "gofy-interface-api/1.0.0" {
		t.Fatalf("User-Agent = %q, want the 1.0.0 library version", defaultUserAgent)
	}
	if Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", Version)
	}
}
