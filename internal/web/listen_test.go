package web_test

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
	"github.com/mcint/glow-web/internal/walk"
	"github.com/mcint/glow-web/internal/web"
)

// Rung A (specs/unified-listener.md §4.A): a Server serves over a unix
// socket, prefix-aware, exactly as it does over TCP.
func TestServeListener_UnixWithPrefix(t *testing.T) {
	d, err := os.MkdirTemp("/tmp", "gw")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	sock := filepath.Join(d, "g.sock")

	s, err := web.NewServer("../../testdata", walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.URLPrefix = "/proj"
	ln, err := listen.Listen("unix:" + sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go s.ServeListener(ln)

	c := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dd net.Dialer
			return dd.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := c.Get("http://unix/proj/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(b), `href="/proj/sample.md"`) {
		t.Errorf("index links not prefixed with /proj")
	}
}

func TestUnixListenerLine(t *testing.T) {
	got := web.UnixListenerLineForTest("/tmp/g.sock", "/proj")
	want := "    unix:/tmp/g.sock  (curl --unix-socket /tmp/g.sock http://localhost/proj/)"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
