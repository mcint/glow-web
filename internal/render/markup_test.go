package render_test

import (
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/render"
)

func TestHTMLMarkup_Heading(t *testing.T) {
	out := string(render.HTMLMarkup([]byte("## Goals\n")))
	for _, want := range []string{
		`class="md-line md-h2"`,
		`<span class="md-marker">##`,
		`<span class="md-text">Goals</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestHTMLMarkup_BoldItalicCode(t *testing.T) {
	out := string(render.HTMLMarkup([]byte("a **b** c *d* e `f`\n")))
	for _, want := range []string{
		`<strong>b</strong>`,
		`<em>d</em>`,
		`<code>f</code>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Markers should be present in muted spans, not absorbed
	if !strings.Contains(out, `<span class="md-marker">**</span>`) {
		t.Errorf("expected ** marker spans in: %s", out)
	}
}

func TestHTMLMarkup_BoldDoesNotEatItalic(t *testing.T) {
	// Edge case: bold+italic on the same line shouldn't mangle nesting.
	out := string(render.HTMLMarkup([]byte("**bold** then *italic*\n")))
	if !strings.Contains(out, `<strong>bold</strong>`) || !strings.Contains(out, `<em>italic</em>`) {
		t.Errorf("bold/italic confusion in:\n%s", out)
	}
}

func TestHTMLMarkup_CodeFenceVerbatim(t *testing.T) {
	src := "before\n```go\nfn main() { *not_italic* }\n```\nafter\n"
	out := string(render.HTMLMarkup([]byte(src)))
	// The `*` inside the code fence must NOT become an <em>.
	if strings.Contains(out, `<em>`) {
		t.Errorf("italic leaked into code fence:\n%s", out)
	}
	if !strings.Contains(out, "fn main() { *not_italic* }") {
		// goldmark-style escapes underscores; we don't, so the literal
		// content should round-trip through html.EscapeString unchanged.
		t.Errorf("code fence content lost:\n%s", out)
	}
}

func TestHTMLMarkup_Link(t *testing.T) {
	out := string(render.HTMLMarkup([]byte("see [docs](https://example.com).\n")))
	if !strings.Contains(out, `<a href="https://example.com">docs</a>`) {
		t.Errorf("link missing in:\n%s", out)
	}
	if !strings.Contains(out, `md-link-url">https://example.com</span>`) {
		t.Errorf("link URL marker missing in:\n%s", out)
	}
}

func TestHTMLMarkup_NoXSSInLinkURL(t *testing.T) {
	// HTML-special chars in URLs must be escaped wherever they're emitted.
	out := string(render.HTMLMarkup([]byte("[x](\"javascript:alert(1)\")\n")))
	if strings.Contains(out, `href=""javascript`) {
		t.Errorf("unescaped quote in href:\n%s", out)
	}
}
