package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/walk"
	"github.com/mcint/glow-web/internal/web"
)

// --- single-file mode (back-compat) ---

func TestFileHandler_RendersFixture(t *testing.T) {
	h := web.FileHandler("../../testdata/sample.md")
	rec := serve(h, "GET", "/", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<!doctype html>",
		`<title>sample.md</title>`,
		`class="bar-name">sample.md`,
		`<h1 id="sample">Sample</h1>`,
		`<code class="language-go">`,
		`href="/?raw=1"`,
		`href="/?download=1"`,
		`<footer>`,
		`glow-web v`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
}

func TestFileHandler_RawAndDownload(t *testing.T) {
	h := web.FileHandler("../../testdata/sample.md")

	raw := serve(h, "GET", "/?raw=1", "")
	if !strings.HasPrefix(raw.Body.String(), "# Sample") {
		t.Errorf("raw view should be original markdown, got %q", firstLine(raw.Body.String()))
	}
	if ct := raw.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("raw content-type = %q, want text/plain", ct)
	}

	dl := serve(h, "GET", "/?download=1", "")
	if cd := dl.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("download disposition = %q", cd)
	}
}

func TestFileHandler_MissingFileReturns500(t *testing.T) {
	h := web.FileHandler("../../testdata/does-not-exist.md")
	rec := serve(h, "GET", "/", "")
	if rec.Code != 500 {
		t.Errorf("expected 500 for missing file, got %d", rec.Code)
	}
}

// --- dir mode ---

func dirFixture(t *testing.T) (string, *web.Server) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.md"), "# Alpha\n")
	mustMk(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "beta.md"), "# Beta\n")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "secret.md\n")
	mustWrite(t, filepath.Join(dir, "secret.md"), "shh")
	mustWrite(t, filepath.Join(dir, "ignore-me.txt"), "")

	s, err := web.NewServer(dir, walk.Options{Gitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	return dir, s
}

func TestDirMode_IndexListsDiscoveredFiles(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/alpha.md"`,
		`href="/sub/beta.md"`,
		"2 files",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	for _, unwanted := range []string{"secret.md", "ignore-me.txt"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("index leaked filtered file %q", unwanted)
		}
	}
}

func TestDirMode_RendersFile(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/sub/beta.md", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<h1 id="beta">Beta</h1>`,
		`class="bar-name">beta.md`,
		`>/sub/beta.md<`, // path display
		`href="/"`,       // back-to-index
	} {
		if !strings.Contains(body, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestDirMode_GitignoredReturns404(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/secret.md", "")
	if rec.Code != 404 {
		t.Errorf("gitignored file should 404, got %d", rec.Code)
	}
}

func TestDirMode_PathTraversalRejected(t *testing.T) {
	_, s := dirFixture(t)
	// ServeMux pre-cleans dotty paths and 301s to the cleaned form; that
	// cleaned form is then re-resolved through findFile and 404s. Either
	// way, no outside file may be served. Accept any non-2xx as rejection.
	for _, p := range []string{"/../etc/passwd", "/sub/../../etc/passwd"} {
		rec := serve(s.Handler(), "GET", p, "")
		if rec.Code >= 200 && rec.Code < 300 {
			t.Errorf("path %q served %d, body: %s", p, rec.Code, rec.Body.String())
		}
	}
}

// --- url prefix ---

func TestURLPrefix_RoutesUnderMount(t *testing.T) {
	_, s := dirFixture(t)
	s.URLPrefix = "/docs"
	h := s.Handler()

	// Outside the prefix → 404
	if rec := serve(h, "GET", "/alpha.md", ""); rec.Code != 404 {
		t.Errorf("unprefixed request should 404, got %d", rec.Code)
	}
	// Index under prefix
	rec := serve(h, "GET", "/docs/", "")
	if rec.Code != 200 {
		t.Fatalf("prefixed index status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/docs/alpha.md"`) {
		t.Errorf("links should be prefixed: %s", body)
	}
	// File under prefix
	rec = serve(h, "GET", "/docs/alpha.md", "")
	if rec.Code != 200 {
		t.Fatalf("prefixed file status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `href="/docs/alpha.md?raw=1"`) {
		t.Errorf("self-links should carry prefix")
	}
}

// --- helpers ---

func serve(h interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
