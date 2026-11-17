package web_test

import (
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/policy"
)

// specs/write-policy.md: read-only is the default (the zero value), and
// the Edit link follows the policy.
func TestWritePolicy_DefaultHidesEdit(t *testing.T) {
	_, s := dirFixture(t)
	if body := serve(s.Handler(), "GET", "/alpha.md", "").Body.String(); strings.Contains(body, "?edit=1") {
		t.Error("default (read-only) page offers Edit")
	}
	if rec := serve(s.Handler(), "POST", "/alpha.md", "# x\n"); rec.Code != 403 || !strings.Contains(rec.Body.String(), "--writable") {
		t.Errorf("default save = %d %q; want 403 naming --writable", rec.Code, rec.Body.String())
	}
}

func TestWritePolicy_MDShowsEditAndSaves(t *testing.T) {
	_, s := dirFixture(t)
	s.Writable = policy.MD
	if body := serve(s.Handler(), "GET", "/alpha.md", "").Body.String(); !strings.Contains(body, "?edit=1") {
		t.Error("md policy page lacks Edit")
	}
	if rec := serve(s.Handler(), "POST", "/alpha.md", "# x\n"); rec.Code != 204 {
		t.Errorf("md save = %d", rec.Code)
	}
}
