package registry_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/registry"
)

// macOS caps sun_path at 104 bytes: SockName uses <name>.sock when the full
// path fits, otherwise a short hash. The record's sock field maps it back.
func TestSockName_FitsSunPath(t *testing.T) {
	short := registry.State{Dir: "/tmp/gw"}
	if got := short.SockName("glow-web"); got != "glow-web.sock" {
		t.Errorf("short: %q", got)
	}
	long := registry.State{Dir: "/" + strings.Repeat("d", 60)}
	n := strings.Repeat("n", 63)
	got := long.SockName(n)
	if got == n+".sock" || !strings.HasSuffix(got, ".sock") {
		t.Errorf("long: %q, want hashed", got)
	}
	if p := filepath.Join(long.RunDir(), got); len(p) > 103 {
		t.Errorf("hashed path still too long: %d", len(p))
	}
	if long.SockName(n) != got {
		t.Error("SockName not deterministic")
	}
}
