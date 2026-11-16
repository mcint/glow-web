package listen_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/listen"
)

// shortDir returns a temp dir with a short path: macOS caps sun_path at
// 104 bytes and t.TempDir() under /var/folders/… can exceed it.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "gw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestParse(t *testing.T) {
	cases := []struct {
		in, network, address string
	}{
		{"127.0.0.1:8080", "tcp", "127.0.0.1:8080"},
		{":8080", "tcp", ":8080"},
		{"tcp:127.0.0.1:0", "tcp", "127.0.0.1:0"},
		{"tcp:[::1]:80", "tcp", "[::1]:80"},
		{"unix:/tmp/g.sock", "unix", "/tmp/g.sock"},
		{"unix:rel/g.sock", "unix", "rel/g.sock"},
	}
	for _, c := range cases {
		n, a, err := listen.Parse(c.in)
		if err != nil || n != c.network || a != c.address {
			t.Errorf("Parse(%q) = %q,%q,%v; want %q,%q", c.in, n, a, err, c.network, c.address)
		}
	}
	for _, bad := range []string{"", "unix:", "tcp:", "udp:1.2.3.4:5"} {
		if _, _, err := listen.Parse(bad); err == nil {
			t.Errorf("Parse(%q): want error", bad)
		}
	}
}

func unixClient(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
}

func TestUnix_ServesHTTPAndIsPrivate(t *testing.T) {
	sock := filepath.Join(shortDir(t), "g.sock")
	ln, err := listen.Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok "+r.URL.Path)
	}))

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket perm = %o, want 600", perm)
	}

	resp, err := unixClient(sock).Get("http://unix/x/y")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok /x/y" {
		t.Errorf("body = %q", b)
	}
}

func TestUnix_StaleSocketReplaced(t *testing.T) {
	sock := filepath.Join(shortDir(t), "g.sock")
	// Leave a dead socket file behind, as a killed process would.
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("precondition: stale socket should exist: %v", err)
	}

	ln, err := listen.Listen("unix:" + sock)
	if err != nil {
		t.Fatalf("stale socket should be replaced, got %v", err)
	}
	ln.Close()
}

func TestUnix_LiveSocketRefused(t *testing.T) {
	sock := filepath.Join(shortDir(t), "g.sock")
	first, err := listen.Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	go func() {
		for {
			c, err := first.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, err = listen.Listen("unix:" + sock)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("second Listen on live socket: err = %v, want 'in use'", err)
	}
}

func TestUnix_RegularFileNeverDeleted(t *testing.T) {
	p := filepath.Join(shortDir(t), "notasock")
	if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listen.Listen("unix:" + p); err == nil {
		t.Fatal("Listen over a regular file: want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "keep me" {
		t.Fatal("regular file was clobbered")
	}
}

func TestTCP_Listens(t *testing.T) {
	ln, err := listen.Listen("tcp:127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if ln.Addr().Network() != "tcp" {
		t.Errorf("network = %s", ln.Addr().Network())
	}
}
