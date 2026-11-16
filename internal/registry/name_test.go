package registry_test

import (
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/registry"
)

// Golden outputs captured from lmux's _kebab (~/dev-llm/lmux/bin/local-mux)
// on 2026-11-16, so a project has one name across both tools.
func TestKebab_MatchesLmux(t *testing.T) {
	for in, want := range map[string]string{
		"ClaudeCollab":      "claude-collab",
		"glow-web":          "glow-web",
		"NB-Noisebridge":    "nb-noisebridge",
		"dev-llm":           "dev-llm",
		".claude":           ".claude",
		"HTTPServer":        "http-server",
		"my_proj":           "my-proj",
		"foo.bar":           "foo.bar",
		"ATProto-HackAbout": "at-proto-hack-about",
		"v0.5":              "v0.5",
		"Foo Bar":           "foo bar",
	} {
		if got := registry.Kebab(in); got != want {
			t.Errorf("Kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

// NameFor = Kebab(basename), then made URL-safe only where lmux's slug
// would not be a valid name.
func TestNameFor(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/me/dev-llm/ClaudeCollab": "claude-collab",
		"/Users/me/dev-llm/glow-web/":    "glow-web",
		"/Users/me/.claude":              "claude",
		"/Users/me/Foo Bar":              "foo-bar",
		"/Users/me/notes/README.md":      "readme.md",
		"/":                              "root",
		"/Users/me/日本語":                  "root",
		"/x/" + strings.Repeat("a", 80):  strings.Repeat("a", 63),
	} {
		if got := registry.NameFor(in); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "glow-web", "v0.5", "a_b", "x" + strings.Repeat("y", 62)} {
		if err := registry.ValidName(ok); err != nil {
			t.Errorf("ValidName(%q) = %v, want ok", ok, err)
		}
	}
	for _, bad := range []string{"", "_hub", ".x", "-x", "a/b", "..", "A", "a b", "x" + strings.Repeat("y", 63)} {
		if err := registry.ValidName(bad); err == nil {
			t.Errorf("ValidName(%q) = nil, want error", bad)
		}
	}
}
