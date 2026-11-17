package hub_test

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/mcint/glow-web/internal/hub"
	"github.com/mcint/glow-web/internal/registry"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := ln.Addr().String()
	ln.Close()
	return a
}

func status(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Rung 7 (D): two sessions; the first becomes the hub, the second only
// registers. Kill the first: the second takes over the same port and
// serves everything that is still registered, straight from disk.
func TestElect_FirstWinsThenHandover(t *testing.T) {
	f := setup(t)
	addr := freeAddr(t)
	newElector := func() *hub.Elector {
		return &hub.Elector{State: f.st, Addr: addr, NewHub: func() *hub.Hub { return &hub.Hub{State: &f.st} }}
	}
	e1, e2 := newElector(), newElector()
	defer e2.Close()

	if won, err := e1.TryOnce(); !won || err != nil {
		t.Fatalf("e1: won=%v err=%v", won, err)
	}
	if won, err := e2.TryOnce(); won || err != nil {
		t.Fatalf("e2 should lose quietly: won=%v err=%v", won, err)
	}

	r1, err := f.st.Register(registry.Record{Name: "one", Root: mdRoot(t, "1.md", "# 1\n")})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := f.st.Register(registry.Record{Name: "two", Root: mdRoot(t, "2.md", "# 2\n")})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	base := "http://" + addr
	for _, p := range []string{"/one/1.md", "/two/2.md", "/beta/b.md"} {
		if c := status(t, base+p); c != 200 {
			t.Fatalf("under e1, %s = %d", p, c)
		}
	}

	// Session one (the hub) exits: its lease and its hub go together.
	r1.Close()
	e1.Close()
	if c := status(t, base+"/two/2.md"); c != 0 {
		t.Fatalf("port should be free after e1 exits, got %d", c)
	}

	if won, err := e2.TryOnce(); !won || err != nil {
		t.Fatalf("e2 takeover: won=%v err=%v", won, err)
	}
	for p, want := range map[string]int{"/two/2.md": 200, "/beta/b.md": 200, "/one/1.md": 404} {
		if c := status(t, base+p); c != want {
			t.Errorf("under e2, %s = %d, want %d", p, c, want)
		}
	}
}

// A port held by something that isn't a glow-web hub: lose with an error
// and don't keep the lock, so a later try can win once the port frees up.
func TestElect_PortBusyReleasesLock(t *testing.T) {
	f := setup(t)
	squat, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := squat.Addr().String()
	e := &hub.Elector{State: f.st, Addr: addr, NewHub: func() *hub.Hub { return &hub.Hub{State: &f.st} }}
	won, err := e.TryOnce()
	if won || err == nil {
		t.Fatalf("busy port: won=%v err=%v", won, err)
	}
	if _, ok, _ := f.st.TryHubLock(); !ok {
		t.Error("lock still held after a failed bind")
	}
	squat.Close()
}

// Rung 9: the elector's Gate guards the TCP listener only; hub.sock (0600)
// stays open to this user so sessions can probe /_/hub.
func TestElect_GateOnTCPOnly(t *testing.T) {
	f := setup(t)
	addr := freeAddr(t)
	deny := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "auth", http.StatusUnauthorized)
		})
	}
	e := &hub.Elector{State: f.st, Addr: addr, Gate: deny, NewHub: func() *hub.Hub { return &hub.Hub{State: &f.st} }}
	defer e.Close()
	if won, err := e.TryOnce(); !won || err != nil {
		t.Fatalf("won=%v err=%v", won, err)
	}
	if c := status(t, "http://"+addr+"/"); c != 401 {
		t.Errorf("TCP = %d, want 401", c)
	}
	sock := filepath.Join(f.st.RunDir(), "hub.sock")
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
	resp, err := c.Get("http://hub/_/hub")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("hub.sock = %d, want 200", resp.StatusCode)
	}
}
