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

// --- listener URL formatting ---

func TestListenerLines_WildcardExpandsToLocalhostAndLAN(t *testing.T) {
	got := web.FormatListenerLinesForTest(":8080", "", []string{"192.168.1.5", "10.0.0.7"})
	want := []string{
		"    http://localhost:8080/  (loopback)",
		"    http://192.168.1.5:8080/  (lan)",
		"    http://10.0.0.7:8080/  (lan)",
	}
	if !equalLines(got, want) {
		t.Errorf("wildcard expansion:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestListenerLines_ZeroAddrTreatedAsWildcard(t *testing.T) {
	got := web.FormatListenerLinesForTest("0.0.0.0:8080", "", nil)
	want := []string{"    http://localhost:8080/  (loopback)"}
	if !equalLines(got, want) {
		t.Errorf("0.0.0.0 expansion: %#v", got)
	}
}

func TestListenerLines_DualStackZeroAddrTreatedAsWildcard(t *testing.T) {
	// What net.Listen(":8080") returns on dual-stack systems is "[::]:8080".
	got := web.FormatListenerLinesForTest("[::]:8080", "", nil)
	want := []string{"    http://localhost:8080/  (loopback)"}
	if !equalLines(got, want) {
		t.Errorf("[::] expansion: %#v", got)
	}
}

func TestListenerLines_SpecificHostShownVerbatim(t *testing.T) {
	got := web.FormatListenerLinesForTest("127.0.0.1:8080", "", []string{"192.168.1.5"})
	want := []string{"    http://127.0.0.1:8080/"}
	if !equalLines(got, want) {
		t.Errorf("specific host: %#v", got)
	}
}

func TestListenerLines_PrefixIncluded(t *testing.T) {
	got := web.FormatListenerLinesForTest("127.0.0.1:8080", "/docs", nil)
	if got[0] != "    http://127.0.0.1:8080/docs/" {
		t.Errorf("prefix not applied: %q", got[0])
	}
}

func TestListenerLines_IPv6Bracketed(t *testing.T) {
	got := web.FormatListenerLinesForTest("[::1]:8080", "", nil)
	if got[0] != "    http://[::1]:8080/" {
		t.Errorf("IPv6 bracketing: %q", got[0])
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- markup view toggle ---

func TestMarkupView_OptIn(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/alpha.md?view=markup", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="md-markup"`) {
		t.Errorf("markup view missing md-markup container")
	}
	if !strings.Contains(body, `<span class="md-marker">#`) {
		t.Errorf("markup view missing heading marker span")
	}
	// Toggle should now point back to the rendered view
	if !strings.Contains(body, `href="/alpha.md?view=rendered"`) {
		t.Errorf("toggle should switch back to rendered: %s", body)
	}
}

func TestMarkupView_DefaultMarkupServer(t *testing.T) {
	_, s := dirFixture(t)
	s.DefaultMarkup = true
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()
	if !strings.Contains(body, `class="md-markup"`) {
		t.Errorf("DefaultMarkup=true should pick markup view by default")
	}
	if !strings.Contains(body, `href="/alpha.md?view=rendered"`) {
		t.Errorf("toggle should switch to rendered when default is markup")
	}
}

// --- edit + live preview + save ---

func TestEdit_PageRenders(t *testing.T) {
	dir, s := dirFixture(t)
	_ = dir
	rec := serve(s.Handler(), "GET", "/alpha.md?edit=1", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<title>alpha.md (edit)</title>`,
		`<textarea id="src"`,
		`# Alpha`, // source filled in
		`const RENDER_URL = "/_/render"`,
		`const SAVE_URL   = "/alpha.md"`,
		`id="preview"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestEdit_DisabledWhenReadOnly(t *testing.T) {
	_, s := dirFixture(t)
	s.ReadOnly = true
	rec := serve(s.Handler(), "GET", "/alpha.md?edit=1", "")
	if rec.Code != 403 {
		t.Errorf("readonly edit should 403, got %d", rec.Code)
	}
	// View page should also hide the Edit link
	rec = serve(s.Handler(), "GET", "/alpha.md", "")
	if strings.Contains(rec.Body.String(), `?edit=1`) {
		t.Errorf("readonly mode should not render Edit link")
	}
}

func TestRenderEndpoint_ConvertsBody(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "POST", "/_/render", "# Hello\n\n*bold*")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<h1 id="hello">Hello</h1>`) {
		t.Errorf("render endpoint did not convert markdown: %q", body)
	}
}

func TestSave_WritesFile(t *testing.T) {
	dir, s := dirFixture(t)
	rec := serve(s.Handler(), "POST", "/alpha.md", "# Updated\n")
	if rec.Code != 204 {
		t.Fatalf("save status = %d, want 204", rec.Code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Updated\n" {
		t.Errorf("file content = %q", string(got))
	}
}

func TestSave_DisabledWhenReadOnly(t *testing.T) {
	dir, s := dirFixture(t)
	s.ReadOnly = true
	rec := serve(s.Handler(), "POST", "/alpha.md", "# Hijack\n")
	if rec.Code != 403 {
		t.Errorf("readonly save should 403, got %d", rec.Code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "alpha.md"))
	if string(got) != "# Alpha\n" {
		t.Errorf("file changed despite readonly: %q", string(got))
	}
}

func TestSave_GitignoredReturns404(t *testing.T) {
	dir, s := dirFixture(t)
	rec := serve(s.Handler(), "POST", "/secret.md", "# Pwn\n")
	if rec.Code != 404 {
		t.Errorf("gitignored save should 404, got %d", rec.Code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "secret.md"))
	if string(got) != "shh" {
		t.Errorf("gitignored file was overwritten: %q", string(got))
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
