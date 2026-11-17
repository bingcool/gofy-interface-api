package interfaceapi_test

import (
	"testing"

	interfaceapi "github.com/bingcool/gofy-interface-api"
	"github.com/bingcool/gofy-interface-api/client"
)

// TestVersionHasOneSource pins both halves of the version story: the module
// version and the client's User-Agent version are the same constant, and the
// released value is 1.0.0. A release bumps internal/version once.
// TestVersionHasOneSource 固定版本的两个方面：模块版本与客户端 UA 版本是同一个常量，
// 且当前发布值是 1.0.0。发布时只改 internal/version 一处。
func TestVersionHasOneSource(t *testing.T) {
	if interfaceapi.Version != client.Version {
		t.Fatalf("module version %q != client version %q", interfaceapi.Version, client.Version)
	}
	if interfaceapi.Version != "1.0.0" {
		t.Fatalf("Version = %q; changing it means a release, not just a constant edit", interfaceapi.Version)
	}
}
