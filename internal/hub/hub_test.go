package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/hub"
	"github.com/mcint/glow-web/internal/registry"
)

func mdRoot(t *testing.T, file, body string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func get(h http.Handler, host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type fixture struct {
	h     *hub.Hub
	st    registry.State
	alpha string
	beta  string
}

func setup(t *testing.T) fixture {
	t.Helper()
	// Short path: unix sockets live under run/, and t.TempDir() on macOS
	// (/var/folders/…) exceeds the 104-byte sun_path limit.
	d, err := os.MkdirTemp("/tmp", "gwhub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	st := registry.State{Dir: filepath.Join(d, "gw")}
	if err := st.Ensure(); err != nil {
		t.Fatal(err)
	}
	alpha := mdRoot(t, "a.md", "# Alpha\n")
	beta := mdRoot(t, "b.md", "# Beta\n")
	if _, err := st.Publish(beta, "beta"); err != nil {
		t.Fatal(err)
	}
	h := &hub.Hub{
		State: &st,
		Extra: []hub.Mount{{Name: "alpha", Root: alpha}},
	}
	return fixture{h, st, alpha, beta}
}

func TestIndexListsArgAndPinMounts(t *testing.T) {
	f := setup(t)
	rec := get(f.h.Handler(), "localhost:7171", "/")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`href="/alpha/"`, `href="/beta/"`, f.alpha, f.beta, "pin", "arg"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestMountServesWithItsPrefix(t *testing.T) {
	f := setup(t)
	h := f.h.Handler()
	rec := get(h, "localhost", "/beta/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `href="/beta/b.md"`) {
		t.Fatalf("/beta/ = %d; links not prefixed:\n%.400s", rec.Code, rec.Body.String())
	}
	rec = get(h, "localhost", "/alpha/a.md")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Alpha") {
		t.Fatalf("/alpha/a.md = %d", rec.Code)
	}
}

func TestBareNameRedirectsAndUnknownIs404(t *testing.T) {
	f := setup(t)
	h := f.h.Handler()
	rec := get(h, "localhost", "/beta")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/beta/" {
		t.Errorf("/beta = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "localhost", "/nope/"); rec.Code != 404 {
		t.Errorf("/nope/ = %d", rec.Code)
	}
}

func TestUnpublishTakesEffectWithoutRestart(t *testing.T) {
	f := setup(t)
	h := f.h.Handler()
	if rec := get(h, "localhost", "/beta/"); rec.Code != 200 {
		t.Fatalf("precondition: %d", rec.Code)
	}
	if err := f.st.Unpublish("beta"); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "localhost", "/beta/"); rec.Code != 404 {
		t.Errorf("after unpublish /beta/ = %d, want 404", rec.Code)
	}
	gamma := mdRoot(t, "g.md", "# G\n")
	if _, err := f.st.Publish(gamma, "gamma"); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "localhost", "/gamma/g.md"); rec.Code != 200 {
		t.Errorf("after publish /gamma/g.md = %d, want 200", rec.Code)
	}
}

func TestStatusJSON(t *testing.T) {
	f := setup(t)
	rec := get(f.h.Handler(), "localhost", "/_/hub")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("/_/hub = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var st struct {
		Hub     string `json:"hub"`
		Version string `json:"version"`
		Mounts  []struct {
			Name, Root, Kind string
		} `json:"mounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Hub != "glow-web" || st.Version == "" || len(st.Mounts) != 2 {
		t.Errorf("status = %+v", st)
	}
}

func TestArgMountShadowsPinOfSameName(t *testing.T) {
	f := setup(t)
	f.h.Extra = append(f.h.Extra, hub.Mount{Name: "beta", Root: f.alpha})
	rec := get(f.h.Handler(), "localhost", "/beta/a.md")
	if rec.Code != 200 {
		t.Errorf("explicit arg should win over pin: %d", rec.Code)
	}
}

func TestHubPrefix(t *testing.T) {
	f := setup(t)
	f.h.URLPrefix = "/docs"
	h := f.h.Handler()
	if rec := get(h, "localhost", "/docs/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `href="/docs/beta/"`) {
		t.Errorf("/docs/ index = %d", rec.Code)
	}
	if rec := get(h, "localhost", "/docs/beta/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `href="/docs/beta/b.md"`) {
		t.Errorf("/docs/beta/ = %d", rec.Code)
	}
	if rec := get(h, "localhost", "/beta/"); rec.Code != 404 {
		t.Errorf("unprefixed path should 404, got %d", rec.Code)
	}
}

// DNS-rebinding guard: the TCP-facing handler only answers loopback names.
func TestHostCheck(t *testing.T) {
	f := setup(t)
	h := hub.HostCheck(f.h.Handler(), nil)
	for _, ok := range []string{"localhost", "localhost:7171", "127.0.0.1:7171", "[::1]:7171", "beta.localhost:7171"} {
		if rec := get(h, ok, "/"); rec.Code != 200 {
			t.Errorf("Host %q = %d, want 200", ok, rec.Code)
		}
	}
	for _, bad := range []string{"evil.example", "evil.example:7171", "127.0.0.1.evil.example"} {
		if rec := get(h, bad, "/"); rec.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", bad, rec.Code)
		}
	}
	allowed := hub.HostCheck(f.h.Handler(), []string{"docs.lmux.test"})
	if rec := get(allowed, "docs.lmux.test", "/"); rec.Code != 200 {
		t.Errorf("allow-listed host = %d", rec.Code)
	}
}

func serveReq(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
