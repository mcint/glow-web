package hub_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/policy"
	"github.com/mcint/glow-web/internal/web"
)

func postStatus(t *testing.T, h http.Handler, path string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", path, strings.NewReader("# changed\n"))
	req.Host = "localhost"
	return serveReq(h, req).Code
}

// specs/write-policy.md §3: pins are read-only unless writable/<name> says
// otherwise, and the hub process's own policy never widens them.
func TestPin_ReadOnlyByDefault_FileGrants(t *testing.T) {
	f := setup(t)
	if c := postStatus(t, f.h.Handler(), "/beta/b.md"); c != 403 {
		t.Errorf("default pin save = %d, want 403", c)
	}
	if err := f.st.SetPinWritable("beta", "md"); err != nil {
		t.Fatal(err)
	}
	if c := postStatus(t, f.h.Handler(), "/beta/b.md"); c != 204 {
		t.Errorf("md pin save = %d, want 204", c)
	}
}

func TestHubPolicyNeverWidensPinsOrLeases(t *testing.T) {
	f := setup(t)
	// An elected session started with --writable=yes: its Configure must
	// not reach other owners' mounts.
	f.h.Configure = func(s *web.Server) { s.Writable = policy.Yes }
	if c := postStatus(t, f.h.Handler(), "/beta/b.md"); c != 403 {
		t.Errorf("pin under a yes-hub = %d, want 403", c)
	}
}

// Arg roots belong to the hub process: they take its --writable via Extra.
func TestArgMountTakesProcessPolicy(t *testing.T) {
	f := setup(t)
	if c := postStatus(t, f.h.Handler(), "/alpha/a.md"); c != 403 {
		t.Errorf("arg root default = %d, want 403", c)
	}
	f.h.Extra[0].Writable = "md"
	if c := postStatus(t, f.h.Handler(), "/alpha/a.md"); c != 204 {
		t.Errorf("arg root md = %d, want 204", c)
	}
}

func TestIndexShowsWritePolicy(t *testing.T) {
	f := setup(t)
	f.st.SetPinWritable("beta", "md")
	idx := get(f.h.Handler(), "localhost", "/").Body.String()
	if !strings.Contains(idx, "<th>write</th>") || !strings.Contains(idx, `class="write">md<`) {
		t.Errorf("index lacks the write column / md value")
	}
}
