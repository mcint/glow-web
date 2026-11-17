package auth_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mcint/glow-web/internal/auth"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

func line(t *testing.T, user, pass string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return user + ":" + string(h) + "\n"
}

func write(t *testing.T, p, body string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func do(h http.Handler, user, pass string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/x", nil)
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOptionalMissingFileIsOpen(t *testing.T) {
	g := &auth.Gate{Path: filepath.Join(t.TempDir(), "htpasswd"), Optional: true}
	if rec := do(g.Wrap(ok), "", ""); rec.Code != 200 {
		t.Fatalf("missing optional file: %d", rec.Code)
	}
	if err := g.Check(); err != nil {
		t.Errorf("Check on missing optional file: %v", err)
	}
}

func TestEnforced(t *testing.T) {
	p := filepath.Join(t.TempDir(), "htpasswd")
	write(t, p, "# users\n\n"+line(t, "ann", "s3cret"), time.Now())
	h := (&auth.Gate{Path: p, Optional: true}).Wrap(ok)

	rec := do(h, "", "")
	if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != `Basic realm="glow-web", charset="UTF-8"` {
		t.Errorf("no creds: %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
	for _, c := range [][2]string{{"ann", "wrong"}, {"bob", "s3cret"}, {"", "s3cret"}} {
		if rec := do(h, c[0], c[1]); rec.Code != 401 {
			t.Errorf("%v: %d, want 401", c, rec.Code)
		}
	}
	for i := 0; i < 2; i++ { // second hit comes from the cache
		if rec := do(h, "ann", "s3cret"); rec.Code != 200 || rec.Body.String() != "ok" {
			t.Errorf("good creds (#%d): %d", i, rec.Code)
		}
	}
}

func TestFileChangesApplyWithoutRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "htpasswd")
	g := &auth.Gate{Path: p, Optional: true}
	h := g.Wrap(ok)
	if rec := do(h, "", ""); rec.Code != 200 {
		t.Fatalf("before file: %d", rec.Code)
	}
	t0 := time.Now().Add(-time.Hour)
	write(t, p, line(t, "ann", "a")+line(t, "bob", "b"), t0)
	if rec := do(h, "", ""); rec.Code != 401 {
		t.Errorf("file created after start must be enforced: %d", rec.Code)
	}
	if rec := do(h, "bob", "b"); rec.Code != 200 {
		t.Errorf("bob before removal: %d", rec.Code)
	}
	write(t, p, line(t, "ann", "a"), t0.Add(time.Minute))
	if rec := do(h, "bob", "b"); rec.Code != 401 {
		t.Errorf("bob after removal (cached creds must not survive): %d", rec.Code)
	}
	if rec := do(h, "ann", "a"); rec.Code != 200 {
		t.Errorf("ann after edit: %d", rec.Code)
	}
}

func TestFailsClosed(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]func() *auth.Gate{
		"non-bcrypt hash": func() *auth.Gate {
			p := filepath.Join(dir, "apr1")
			write(t, p, "ann:$apr1$abc$def\n", time.Now())
			return &auth.Gate{Path: p, Optional: true}
		},
		"world-writable": func() *auth.Gate {
			p := filepath.Join(dir, "loose")
			write(t, p, line(t, "ann", "a"), time.Now())
			os.Chmod(p, 0o666)
			return &auth.Gate{Path: p, Optional: true}
		},
		"required but missing": func() *auth.Gate {
			return &auth.Gate{Path: filepath.Join(dir, "nope")}
		},
		"malformed line": func() *auth.Gate {
			p := filepath.Join(dir, "bad")
			write(t, p, "no-colon-here\n", time.Now())
			return &auth.Gate{Path: p, Optional: true}
		},
	}
	for name, mk := range cases {
		g := mk()
		if err := g.Check(); err == nil {
			t.Errorf("%s: Check = nil, want error", name)
		}
		if rec := do(g.Wrap(ok), "ann", "a"); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: %d, want 500 (fail closed)", name, rec.Code)
		}
	}
}

func TestNonBcryptErrorSaysHow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h")
	write(t, p, "ann:{SHA}abc\n", time.Now())
	err := (&auth.Gate{Path: p}).Check()
	if err == nil || !strings.Contains(err.Error(), "bcrypt") || !strings.Contains(err.Error(), "glow-web passwd") {
		t.Errorf("err = %v; want it to name bcrypt and glow-web passwd", err)
	}
}

func TestHashLineVerifies(t *testing.T) {
	l, err := auth.HashLine("ann", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(l, "ann:$2") {
		t.Fatalf("line = %q", l)
	}
	p := filepath.Join(t.TempDir(), "h")
	write(t, p, l+"\n", time.Now())
	if rec := do((&auth.Gate{Path: p}).Wrap(ok), "ann", "pw"); rec.Code != 200 {
		t.Errorf("HashLine output didn't verify: %d", rec.Code)
	}
	if _, err := auth.HashLine("a:b", "pw"); err == nil {
		t.Error("user with ':' accepted")
	}
}
