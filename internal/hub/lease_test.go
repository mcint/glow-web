package hub_test

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mcint/glow-web/internal/registry"
)

// Rung 4 (B2): the hub serves a root only while its process holds the lease.
func TestLease_ServedWhileHeld(t *testing.T) {
	f := setup(t)
	h := f.h.Handler()
	root := mdRoot(t, "l.md", "# Leased\n")

	if rec := get(h, "localhost", "/sess/"); rec.Code != 404 {
		t.Fatalf("before register: %d", rec.Code)
	}
	lease, err := f.st.Register(registry.Record{Name: "sess", Root: root, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "localhost", "/sess/l.md")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Leased") {
		t.Fatalf("while held: %d", rec.Code)
	}
	if idx := get(h, "localhost", "/").Body.String(); !strings.Contains(idx, `href="/sess/"`) || !strings.Contains(idx, "lease") {
		t.Errorf("index doesn't list the lease")
	}
	lease.Close()
	if rec := get(h, "localhost", "/sess/l.md"); rec.Code != 404 {
		t.Errorf("after close: %d, want 404", rec.Code)
	}
}

// A real crash: a child process registers, then is SIGKILLed. The kernel
// drops its flock; the hub must stop serving without any cleanup by the child.
func TestLease_CrashStopsServing(t *testing.T) {
	f := setup(t)
	h := f.h.Handler()
	root := mdRoot(t, "l.md", "# L\n")
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperRegisterAndSleep")
	cmd.Env = append(os.Environ(), "GW_HELPER=1", "GW_STATE="+f.st.Dir, "GW_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for get(h, "localhost", "/sess/").Code != 200 {
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("helper never registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cmd.Process.Signal(syscall.SIGKILL)
	cmd.Wait()
	if rec := get(h, "localhost", "/sess/"); rec.Code != 404 {
		t.Errorf("after SIGKILL: %d, want 404", rec.Code)
	}
}

func TestHelperRegisterAndSleep(t *testing.T) {
	if os.Getenv("GW_HELPER") != "1" {
		t.Skip("helper process only")
	}
	st := registry.State{Dir: os.Getenv("GW_STATE")}
	if _, err := st.Register(registry.Record{Name: "sess", Root: os.Getenv("GW_ROOT")}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

// A record for the same root as a pin shadows it; the index shows lease.
func TestLease_ShadowsPinOfSameRoot(t *testing.T) {
	f := setup(t)
	lease, err := f.st.Register(registry.Record{Name: "beta", Root: f.beta})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	for _, m := range f.h.Mounts() {
		if m.Name == "beta" && m.Kind != "lease" {
			t.Errorf("beta kind = %q, want lease", m.Kind)
		}
	}
}

// Security review (2026-11-16): a lease is rendered by the hub, so the
// session's --readonly must travel in the record. The hub applies
// readonly = hub's || record's: a lease can only tighten, never loosen.
func TestLease_ReadOnlyTravelsInRecord(t *testing.T) {
	f := setup(t)
	root := mdRoot(t, "r.md", "# R\n")
	lease, err := f.st.Register(registry.Record{Name: "ro", Root: root, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	req, _ := http.NewRequest("POST", "/ro/r.md", strings.NewReader("# overwritten\n"))
	req.Host = "localhost"
	if rec := serveReq(f.h.Handler(), req); rec.Code != 403 {
		t.Errorf("save to a read-only lease = %d, want 403", rec.Code)
	}
	if b, _ := os.ReadFile(root + "/r.md"); string(b) != "# R\n" {
		t.Fatalf("read-only lease was written: %q", b)
	}
	// The same root leased writable afterwards is writable: no stale cache.
	lease.Close()
	l2, err := f.st.Register(registry.Record{Name: "ro", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	req, _ = http.NewRequest("POST", "/ro/r.md", strings.NewReader("# ok\n"))
	req.Host = "localhost"
	if rec := serveReq(f.h.Handler(), req); rec.Code != 204 {
		t.Errorf("save to a writable lease = %d, want 204", rec.Code)
	}
}
