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

// specs/write-policy.md: a lease is rendered by the hub, so the session's
// --writable travels in its record. The default is read-only.
func TestLease_WritableTravelsInRecord(t *testing.T) {
	f := setup(t)
	root := mdRoot(t, "r.md", "# R\n")
	save := func(want int) {
		t.Helper()
		req, _ := http.NewRequest("POST", "/ro/r.md", strings.NewReader("# changed\n"))
		req.Host = "localhost"
		if rec := serveReq(f.h.Handler(), req); rec.Code != want {
			t.Errorf("save = %d, want %d", rec.Code, want)
		}
	}
	lease, err := f.st.Register(registry.Record{Name: "ro", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	save(403)
	if b, _ := os.ReadFile(root + "/r.md"); string(b) != "# R\n" {
		t.Fatalf("default lease was written: %q", b)
	}
	lease.Close()
	// The same root leased writable: served writable (no stale handler).
	l2, err := f.st.Register(registry.Record{Name: "ro", Root: root, Writable: "md"})
	if err != nil {
		t.Fatal(err)
	}
	save(204)
	l2.Close()
	// An unrecognized value (a newer build's grammar) fails closed.
	l3, err := f.st.Register(registry.Record{Name: "ro", Root: root, Writable: "glob:**"})
	if err != nil {
		t.Fatal(err)
	}
	defer l3.Close()
	save(403)
}
