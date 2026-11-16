package hub_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mcint/glow-web/internal/registry"
	"github.com/mcint/glow-web/internal/version"
)

// holdRecord writes run/<name>.json verbatim and flocks it, standing in for
// a session from another glow-web build.
func holdRecord(t *testing.T, st registry.State, name, body string) {
	t.Helper()
	p := filepath.Join(st.RunDir(), name+".json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

// Rung 6 (V): a record from a future contract is listed, never routed.
func TestContract_UnknownMajorListedNotRouted(t *testing.T) {
	f := setup(t)
	holdRecord(t, f.st, "future", `{"contract": 2, "name": "future", "root": "/x", "pid": 1, "version": "v9.0.0"}`)
	h := f.h.Handler()
	idx := get(h, "localhost", "/").Body.String()
	if !strings.Contains(idx, "future") || !strings.Contains(idx, "incompatible") {
		t.Errorf("index should list future as incompatible")
	}
	rec := get(h, "localhost", "/future/")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "contract 2") {
		t.Errorf("/future/ = %d %q", rec.Code, rec.Body.String())
	}
}

func TestContract_IncompatibleNeverShadowsPin(t *testing.T) {
	f := setup(t)
	holdRecord(t, f.st, "beta", `{"contract": 2, "name": "beta", "root": "/x", "pid": 1}`)
	if rec := get(f.h.Handler(), "localhost", "/beta/b.md"); rec.Code != 200 {
		t.Errorf("pin should still serve: %d", rec.Code)
	}
}

// Same major, unknown fields: a minor addition, routed normally.
func TestContract_UnknownFieldsIgnored(t *testing.T) {
	f := setup(t)
	root := mdRoot(t, "n.md", "# Newer\n")
	holdRecord(t, f.st, "newer", `{"contract": 1, "name": "newer", "root": "`+root+`", "pid": 1, "version": "v9.9.9", "shiny": {"x": 1}}`)
	if rec := get(f.h.Handler(), "localhost", "/newer/n.md"); rec.Code != 200 {
		t.Errorf("same-major record with extra fields: %d", rec.Code)
	}
}

// The index shows each session's version and marks skew from the hub's.
func TestIndex_ShowsVersionSkew(t *testing.T) {
	f := setup(t)
	lease, err := f.st.Register(registry.Record{Name: "old", Root: mdRoot(t, "o.md", "# O\n"), Version: "v0.0.1-old"})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	same, err := f.st.Register(registry.Record{Name: "same", Root: mdRoot(t, "s.md", "# S\n"), Version: version.String()})
	if err != nil {
		t.Fatal(err)
	}
	defer same.Close()
	idx := get(f.h.Handler(), "localhost", "/").Body.String()
	if !strings.Contains(idx, "v0.0.1-old") || strings.Count(idx, `class="skew"`) != 1 {
		t.Errorf("want exactly one skew marker (old), index:\n%s", idx)
	}
}
