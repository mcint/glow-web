package web_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A page on another site can send a "simple" POST to a loopback glow-web
// and overwrite a file. A hub multiplies the targets, so every
// state-changing request now goes through CrossOriginProtection
// (Sec-Fetch-Site / Origin). Same-origin fetches from glow-web's own edit
// page and header-less CLI clients still work.
func TestSave_CrossSiteRefused(t *testing.T) {
	dir, s := dirFixture(t)
	before, _ := os.ReadFile(filepath.Join(dir, "alpha.md"))

	for _, hdr := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "https://evil.example"},
	} {
		req := httptest.NewRequest("POST", "/alpha.md", strings.NewReader("# Pwned\n"))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("%v: status = %d, want 403", hdr, rec.Code)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "alpha.md"))
	if string(after) != string(before) {
		t.Fatalf("cross-site POST changed the file: %q", after)
	}
}

func TestSave_SameOriginAndCLIAllowed(t *testing.T) {
	_, s := dirFixture(t)
	req := httptest.NewRequest("POST", "http://127.0.0.1:7171/alpha.md", strings.NewReader("# Mine\n"))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://127.0.0.1:7171")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Errorf("same-origin save = %d, want 204", rec.Code)
	}
	// No browser headers at all (curl): allowed, as before.
	if rec := serve(s.Handler(), "POST", "/alpha.md", "# CLI\n"); rec.Code != 204 {
		t.Errorf("header-less save = %d, want 204", rec.Code)
	}
}
