package render_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/render"
)

func TestHTML_Sample(t *testing.T) {
	src, err := os.ReadFile("../../testdata/sample.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out, err := render.HTML(src)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		`<h1 id="sample">Sample</h1>`,
		`<h2 id="goals">Goals</h2>`,
		`<code class="language-go">`,
		`<a href="https://github.com/yuin/goldmark">goldmark</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, got)
		}
	}
}
