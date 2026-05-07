package web_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/web"
)

func TestFileHandler_RendersFixture(t *testing.T) {
	h := web.FileHandler("../../testdata/sample.md")
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<!doctype html>",
		`<title>sample.md</title>`,
		`<h1 id="sample">Sample</h1>`,
		`<code class="language-go">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
}

func TestFileHandler_MissingFileReturns500(t *testing.T) {
	h := web.FileHandler("../../testdata/does-not-exist.md")
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Errorf("expected 500 for missing file, got %d", rec.Code)
	}
}
