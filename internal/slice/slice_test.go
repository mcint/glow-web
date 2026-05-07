package slice_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/slice"
)

func loadSample(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("../../testdata/sample.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return src
}

func TestBySection_Goals(t *testing.T) {
	src := loadSample(t)
	out, ok := slice.BySection(src, []string{"Goals"})
	if !ok {
		t.Fatal("expected match for Goals")
	}
	got := string(out)
	if !strings.HasPrefix(got, "## Goals") {
		t.Errorf("expected ## Goals prefix; first line: %q", firstLine(got))
	}
	if !strings.Contains(got, "Be deterministic") {
		t.Errorf("Goals section missing list content:\n%s", got)
	}
	if strings.Contains(got, "## Code") {
		t.Errorf("Goals section should not include Code:\n%s", got)
	}
}

func TestBySection_CaseInsensitive(t *testing.T) {
	src := loadSample(t)
	out, ok := slice.BySection(src, []string{"goals"})
	if !ok || !strings.HasPrefix(string(out), "## Goals") {
		t.Fatalf("case-insensitive match failed: ok=%v out=%q", ok, firstLine(string(out)))
	}
}

func TestBySection_NoMatch(t *testing.T) {
	src := loadSample(t)
	if _, ok := slice.BySection(src, []string{"nope"}); ok {
		t.Errorf("expected no match")
	}
}

func TestBySection_EmptyPathReturnsAll(t *testing.T) {
	src := loadSample(t)
	out, ok := slice.BySection(src, nil)
	if !ok || len(out) != len(src) {
		t.Errorf("empty path should return src unchanged; got len=%d want=%d ok=%v", len(out), len(src), ok)
	}
}

// Synthetic doc for nested-path tests so we don't have to bloat sample.md.
const nested = `# Top

## Foo
foo body

### Inner
inner body

## Bar
bar body

### Inner
other inner body
`

func TestBySection_NestedPath_PicksRightInner(t *testing.T) {
	out, ok := slice.BySection([]byte(nested), []string{"Bar", "Inner"})
	if !ok {
		t.Fatal("expected nested match")
	}
	got := string(out)
	if !strings.HasPrefix(got, "### Inner") {
		t.Errorf("expected ### Inner prefix; got: %q", firstLine(got))
	}
	if !strings.Contains(got, "other inner body") {
		t.Errorf("expected Bar's inner body; got:\n%s", got)
	}
	if strings.Contains(got, "inner body\n") && !strings.Contains(got, "other inner body") {
		t.Errorf("picked Foo's Inner instead of Bar's Inner:\n%s", got)
	}
}

func TestBySection_NoCrossSibling(t *testing.T) {
	// "Foo / Inner" must not match Bar's Inner even though it appears later.
	out, ok := slice.BySection([]byte(nested), []string{"Foo", "Inner"})
	if !ok {
		t.Fatal("expected match for Foo/Inner")
	}
	got := string(out)
	if strings.Contains(got, "other inner body") {
		t.Errorf("Foo/Inner leaked into Bar's section:\n%s", got)
	}
	if !strings.Contains(got, "inner body") {
		t.Errorf("Foo/Inner missing its body:\n%s", got)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
