package web_test

import (
	"strings"
	"testing"
)

// Under a hub, the location starts with a link to the hub root, just
// before the project's base-path crumb: hub / <project> / … The hub is the
// outermost scope (specs/header-layout.md). Standalone, there is no link.
func TestHubCrumb_PresentUnderHubOnEveryView(t *testing.T) {
	_, s := dirFixture(t)
	s.HubURL = "/docs/"
	h := s.Handler()
	for _, path := range []string{"/", "/alpha.md", "/alpha.md?edit=1"} {
		body := serve(h, "GET", path, "").Body.String()
		hub := strings.Index(body, `<a class="crumb crumb-hub" href="/docs/"`)
		first := strings.Index(body, `<nav class="crumbs"`)
		if hub < 0 || hub < first {
			t.Errorf("%s: hub crumb missing or outside the crumbs nav", path)
			continue
		}
		// It precedes the project (base path) crumb.
		rest := body[hub:]
		if !strings.Contains(rest[:300], `crumb-sep`) {
			t.Errorf("%s: hub crumb not followed by a separator", path)
		}
	}
}

func TestHubCrumb_AbsentStandalone(t *testing.T) {
	_, s := dirFixture(t)
	if body := serve(s.Handler(), "GET", "/", "").Body.String(); strings.Contains(body, `class="crumb crumb-hub"`) {
		t.Error("standalone page shows a hub crumb")
	}
}
