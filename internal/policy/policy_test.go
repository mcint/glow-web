package policy_test

import (
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/policy"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]policy.Write{"no": policy.No, "md": policy.MD, "yes": policy.Yes} {
		got, err := policy.Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "true", "YES", "glob:**", "md,yes"} {
		_, err := policy.Parse(bad)
		if err == nil || !strings.Contains(err.Error(), "no|md|yes") {
			t.Errorf("Parse(%q): err = %v, want one listing no|md|yes", bad, err)
		}
	}
}

// Records and pin files may come from a newer glow-web: unknown → no.
func TestLenientFailsClosed(t *testing.T) {
	for in, want := range map[string]policy.Write{"": policy.No, "md": policy.MD, "yes": policy.Yes, "glob:docs/**": policy.No, "nonsense": policy.No} {
		if got := policy.Lenient(in); got != want {
			t.Errorf("Lenient(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAllows(t *testing.T) {
	var zero policy.Write
	if zero.Allows("md") {
		t.Error("zero value must be read-only")
	}
	if !policy.MD.Allows("md") || policy.MD.Allows("txt") {
		t.Error("md allows exactly md")
	}
	if !policy.Yes.Allows("md") {
		t.Error("yes allows md")
	}
	if policy.No.Allows("md") {
		t.Error("no allows nothing")
	}
	if policy.No.String() != "no" || zero.String() != "no" || policy.MD.String() != "md" {
		t.Error("String")
	}
}
