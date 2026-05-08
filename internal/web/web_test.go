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
		`<title>sample.md</title>`, // FileHandler shim leaves TitlePrefix empty
		`<span class="crumb crumb-current">sample.md</span>`,
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
	// Match the CLI default so production tests describe what users see.
	s.TitlePrefix = "glow-web"
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
		`data-rel="alpha.md"`,
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
	dir, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/sub/beta.md", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	rootName := filepath.Base(dir)
	for _, want := range []string{
		`<h1 id="beta">Beta</h1>`,
		`<span class="crumb crumb-current">beta.md</span>`,
		`<a class="crumb" href="/">` + rootName + `</a>`,
		`<a class="crumb" href="/?prefix=sub%2F">sub</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("view missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestDirMode_IndexPrefixFilter(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/?prefix=sub/", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/sub/beta.md"`) {
		t.Errorf("filtered index missing beta link")
	}
	// alpha.md is at root, must not appear under prefix=sub/
	if strings.Contains(body, `href="/alpha.md"`) {
		t.Errorf("filtered index should hide alpha.md")
	}
	if !strings.Contains(body, `<span class="crumb crumb-current">sub</span>`) {
		t.Errorf("filtered index should show 'sub' as current crumb")
	}
	// Display should drop the prefix in the listing
	if !strings.Contains(body, `>beta.md</a>`) {
		t.Errorf("filtered listing should show prefix-trimmed display name")
	}
	// "1 files" in the count
	if !strings.Contains(body, "1 files") {
		t.Errorf("count not 1 under prefix filter")
	}
}

func TestDirMode_IndexPrefixFilterRejectsTraversal(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/?prefix=../etc/", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	// Should fall back to no-filter (showing all 2 files)
	if !strings.Contains(rec.Body.String(), "2 files") {
		t.Errorf("traversal prefix should be rejected, falling through to root")
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

// --- title prefix ---

func TestTitlePrefix_LiteralValueAppendsAfterDoc(t *testing.T) {
	_, s := dirFixture(t) // dirFixture sets TitlePrefix = "glow-web"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · glow-web</title>`) {
		t.Errorf("literal prefix not applied: %s", rec.Body.String())
	}
}

func TestTitlePrefix_IndexAlsoSuffixed(t *testing.T) {
	dir, s := dirFixture(t)
	_ = dir
	rec := serve(s.Handler(), "GET", "/", "")
	if !strings.Contains(rec.Body.String(), ` · glow-web</title>`) {
		t.Errorf("index title missing prefix suffix: %s", rec.Body.String())
	}
}

func TestTitlePrefix_EmptyDisables(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = ""
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md</title>`) {
		t.Errorf("empty TitlePrefix should give bare doc title: %s", rec.Body.String())
	}
}

func TestTitlePrefix_AutoExpandsWithPort(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = "auto"
	s.Addr = "127.0.0.1:18099"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · glow-web:18099</title>`) {
		t.Errorf("auto + Addr should produce 'glow-web:18099': %s", rec.Body.String())
	}
}

func TestTitlePrefix_PortNeverOmitsPort(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = "auto"
	s.TitlePort = "never"
	s.Addr = "127.0.0.1:18099"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · glow-web</title>`) {
		t.Errorf("--title-prefix-port=never should omit port: %s", rec.Body.String())
	}
}

func TestTitlePrefix_PortNeverIgnoredForLiteralPrefix(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = "docs-of-truth"
	s.TitlePort = "never"
	s.Addr = "127.0.0.1:18099"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · docs-of-truth</title>`) {
		t.Errorf("port handling shouldn't affect literal prefix: %s", rec.Body.String())
	}
}

func TestTitlePrefix_AutoNoAddrJustGlowWeb(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = "auto"
	s.Addr = ""
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · glow-web</title>`) {
		t.Errorf("auto without Addr should fall back to bare 'glow-web': %s", rec.Body.String())
	}
}

func TestTitlePrefix_CustomLiteral(t *testing.T) {
	_, s := dirFixture(t)
	s.TitlePrefix = "docs-of-truth"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `<title>alpha.md · docs-of-truth</title>`) {
		t.Errorf("custom literal not applied verbatim: %s", rec.Body.String())
	}
}

// --- theme (auto/light/dark) ---

func TestTheme_DefaultAutoBakedIntoInitScript(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()
	// theme-init script ships in <head>
	if !strings.Contains(body, `var server = "auto"`) {
		t.Errorf("default --theme=auto should bake server=\"auto\" into theme-init: %s", body)
	}
	// toggle button ships in bar
	if !strings.Contains(body, `id="theme-toggle"`) {
		t.Errorf("bar missing theme toggle button")
	}
}

func TestTheme_LightFlagBakedIn(t *testing.T) {
	_, s := dirFixture(t)
	s.Theme = "light"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `var server = "light"`) {
		t.Errorf("--theme=light should bake server=\"light\" into theme-init")
	}
}

func TestTheme_DarkFlagBakedIn(t *testing.T) {
	_, s := dirFixture(t)
	s.Theme = "dark"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `var server = "dark"`) {
		t.Errorf("--theme=dark should bake server=\"dark\" into theme-init")
	}
}

func TestTheme_InvalidValueFallsBackToAuto(t *testing.T) {
	_, s := dirFixture(t)
	s.Theme = "purple-haze"
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	if !strings.Contains(rec.Body.String(), `var server = "auto"`) {
		t.Errorf("invalid --theme value should normalise to auto")
	}
}

func TestTheme_TogglePresentOnAllPages(t *testing.T) {
	_, s := dirFixture(t)
	for _, path := range []string{"/", "/alpha.md", "/alpha.md?edit=1"} {
		rec := serve(s.Handler(), "GET", path, "")
		if !strings.Contains(rec.Body.String(), `id="theme-toggle"`) {
			t.Errorf("path %q missing theme toggle", path)
		}
	}
}

// --- command palette + /_/files + index filter ---

func TestFilesEndpoint_DirMode(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/_/files", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"rel":"alpha.md"`, `"url":"/alpha.md"`, `"rel":"sub/beta.md"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/_/files missing %q in: %s", want, body)
		}
	}
	if strings.Contains(body, "secret.md") {
		t.Errorf("/_/files leaked gitignored entry: %s", body)
	}
}

func TestPalette_RenderedWhenEnabled(t *testing.T) {
	_, s := dirFixture(t)
	s.CommandPalette = true
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()
	for _, want := range []string{
		`id="palette"`,
		`id="palette-input"`,
		`const filesURL = "/_/files"`,
		`STATE_KEY = K('palette-open')`, // namespaced sidebar persistence
	} {
		if !strings.Contains(body, want) {
			t.Errorf("palette missing %q", want)
		}
	}
}

func TestScrollMemory_ShipsOnViewPages(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()
	if !strings.Contains(body, `K('scroll:'`) {
		t.Errorf("namespaced scroll-memory script missing on view page")
	}
}

func TestKeyPrefix_PerProjectScoped(t *testing.T) {
	dir1 := t.TempDir()
	mustWrite(t, filepath.Join(dir1, "a.md"), "# A")
	s1, err := web.NewServer(dir1, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	mustWrite(t, filepath.Join(dir2, "a.md"), "# A")
	s2, err := web.NewServer(dir2, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}

	body1 := serve(s1.Handler(), "GET", "/a.md", "").Body.String()
	body2 := serve(s2.Handler(), "GET", "/a.md", "").Body.String()

	prefix1 := extractKeyPrefix(t, body1)
	prefix2 := extractKeyPrefix(t, body2)

	if prefix1 == prefix2 {
		t.Errorf("two distinct project roots should yield different KEY_PREFIX, got %q == %q", prefix1, prefix2)
	}
	for _, p := range []string{prefix1, prefix2} {
		if !strings.HasPrefix(p, "glow-") || !strings.HasSuffix(p, "-") || len(p) != len("glow-XXXXXXXX-") {
			t.Errorf("KEY_PREFIX shape unexpected: %q", p)
		}
	}
}

// extractKeyPrefix pulls the JS-string literal out of `const KEY_PREFIX = "..."`.
func extractKeyPrefix(t *testing.T, body string) string {
	t.Helper()
	const marker = `const KEY_PREFIX = "`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("KEY_PREFIX not found in body")
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("KEY_PREFIX value not terminated")
	}
	return rest[:j]
}

func TestPalette_HiddenWhenDisabled(t *testing.T) {
	_, s := dirFixture(t)
	s.CommandPalette = false
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()
	if strings.Contains(body, `id="palette"`) {
		t.Errorf("palette overlay should not render when disabled")
	}
	// The shared script block still ships (for the index filter), but the
	// palette-specific UI must not.
	if strings.Contains(body, `id="palette-input"`) {
		t.Errorf("palette input should not render when disabled")
	}
}

func TestIndexFilterInput_Present(t *testing.T) {
	_, s := dirFixture(t)
	rec := serve(s.Handler(), "GET", "/", "")
	body := rec.Body.String()
	if !strings.Contains(body, `id="index-filter"`) {
		t.Errorf("index page missing filter input")
	}
}

func TestPalette_FilesURLPrefixed(t *testing.T) {
	_, s := dirFixture(t)
	s.URLPrefix = "/docs"
	s.CommandPalette = true
	rec := serve(s.Handler(), "GET", "/docs/alpha.md", "")
	body := rec.Body.String()
	if !strings.Contains(body, `const filesURL = "/docs/_/files"`) {
		t.Errorf("palette filesURL should carry URL prefix: %s", body)
	}
}

// --- intra-project link rewriting ---

func TestDirMode_RewritesIntraProjectLinks(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.md"), `# Alpha

See [beta](sub/beta.md), [also alpha](alpha.md), [external](https://example.com),
[anchor](#section), [missing](nope.md).
`)
	mustMk(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "beta.md"), "# Beta\n")

	s, err := web.NewServer(dir, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(s.Handler(), "GET", "/alpha.md", "")
	body := rec.Body.String()

	// Existing intra-project links rewritten to served URLs.
	if !strings.Contains(body, `href="/sub/beta.md"`) {
		t.Errorf("relative md link not rewritten: %s", body)
	}
	if !strings.Contains(body, `href="/alpha.md"`) {
		t.Errorf("self-link not rewritten: %s", body)
	}
	// External, anchor, and missing links left alone.
	if !strings.Contains(body, `href="https://example.com"`) {
		t.Errorf("external link should not be rewritten")
	}
	if !strings.Contains(body, `href="#section"`) {
		t.Errorf("anchor link should not be rewritten")
	}
	if !strings.Contains(body, `href="nope.md"`) {
		t.Errorf("missing-target link should be left as-is (becomes a 404 if clicked) but stayed: %s", body)
	}
}

func TestDirMode_LinkResolutionFromSubdir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "top.md"), "# Top")
	mustMk(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "beta.md"), `# Beta

[up to top](../top.md) and [sibling](beta.md).
`)
	s, err := web.NewServer(dir, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(s.Handler(), "GET", "/sub/beta.md", "")
	body := rec.Body.String()

	if !strings.Contains(body, `href="/top.md"`) {
		t.Errorf("../top.md should resolve to /top.md: %s", body)
	}
	if !strings.Contains(body, `href="/sub/beta.md"`) {
		t.Errorf("sibling beta.md should resolve to /sub/beta.md: %s", body)
	}
}

func TestDirMode_LinkResolutionRejectsRootEscape(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), `[escape](../../../etc/passwd)`)
	s, err := web.NewServer(dir, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(s.Handler(), "GET", "/doc.md", "")
	body := rec.Body.String()
	// The escape attempt should NOT be rewritten — it stays as the original
	// (broken) string and won't resolve against any served URL.
	if !strings.Contains(body, `href="../../../etc/passwd"`) {
		t.Errorf("traversal link should be left unchanged, got: %s", body)
	}
	// And critically — it must not have been turned into an absolute URL
	// targeting anything inside the served root.
	if strings.Contains(body, `href="/etc/passwd"`) || strings.Contains(body, `href="/passwd"`) {
		t.Errorf("traversal must not collapse into a served URL: %s", body)
	}
}

func TestDirMode_LinkResolutionWithURLPrefix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "[b](b.md)")
	mustWrite(t, filepath.Join(dir, "b.md"), "# B")
	s, err := web.NewServer(dir, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.URLPrefix = "/docs"
	rec := serve(s.Handler(), "GET", "/docs/a.md", "")
	body := rec.Body.String()
	if !strings.Contains(body, `href="/docs/b.md"`) {
		t.Errorf("rewritten link should carry URL prefix: %s", body)
	}
}

func TestDirMode_LinkResolutionPreservesAnchorAndQuery(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), `[deep](b.md#section "title")`)
	mustWrite(t, filepath.Join(dir, "b.md"), "# B\n## Section\n")
	s, err := web.NewServer(dir, walk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(s.Handler(), "GET", "/a.md", "")
	body := rec.Body.String()
	if !strings.Contains(body, `href="/b.md#section"`) {
		t.Errorf("anchor should survive rewrite: %s", body)
	}
}

// --- listener URL formatting ---
//
// LAN IP placeholders use RFC 5737 TEST-NET ranges (192.0.2.0/24,
// 198.51.100.0/24) which are reserved for documentation/examples and won't
// be confused for any real network. Avoid RFC1918 (192.168.x, 10.x) here —
// reading a real-looking IP in a public test is a small but pointless
// "is this leaked from the author's network?" hesitation.

func TestListenerLines_WildcardExpandsToLocalhostAndLAN(t *testing.T) {
	got := web.FormatListenerLinesForTest(":8080", "", []string{"192.0.2.5", "198.51.100.7"})
	want := []string{
		"    http://localhost:8080/  (loopback)",
		"    http://192.0.2.5:8080/  (lan)",
		"    http://198.51.100.7:8080/  (lan)",
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
	got := web.FormatListenerLinesForTest("127.0.0.1:8080", "", []string{"192.0.2.5"})
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
		`<title>alpha.md (edit) · glow-web</title>`,
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
	// Crumbs back to project root must also use the prefix
	if !strings.Contains(rec.Body.String(), `<a class="crumb" href="/docs/">`) {
		t.Errorf("project-root crumb should use URL prefix")
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
