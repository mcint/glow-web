package hub_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/listen"
	"github.com/mcint/glow-web/internal/registry"
)

type echoed struct {
	Method, Path, Query, Host, Custom, Body string
}

// echoBackend serves on the record's socket and echoes what it received.
func echoBackend(t *testing.T, st registry.State, name string) (sockName string, stop func()) {
	t.Helper()
	sockName = st.SockName(name)
	ln, err := listen.Listen("unix:" + filepath.Join(st.RunDir(), sockName))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "echo")
		json.NewEncoder(w).Encode(echoed{r.Method, r.URL.Path, r.URL.RawQuery, r.Host, r.Header.Get("X-Custom"), string(b)})
	})}
	go srv.Serve(ln)
	return sockName, func() { srv.Close() }
}

// Rung 5 (C): the hub forwards /<name>/… to the session's socket verbatim:
// path unstripped, query, Host, headers and body as sent.
func TestProxy_Verbatim(t *testing.T) {
	f := setup(t)
	sock, stop := echoBackend(t, f.st, "sess")
	defer stop()
	lease, err := f.st.Register(registry.Record{Name: "sess", Root: "/some/root", Sock: sock, Version: "v9"})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()

	req, _ := http.NewRequest("POST", "/sess/a/b.md?view=raw&x=1", strings.NewReader("payload"))
	req.Host = "127.0.0.1:7171"
	req.Header.Set("X-Custom", "kept")
	rec := serveReq(f.h.Handler(), req)
	if rec.Code != 200 || rec.Header().Get("X-Backend") != "echo" {
		t.Fatalf("status %d, backend %q: %s", rec.Code, rec.Header().Get("X-Backend"), rec.Body.String())
	}
	var got echoed
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := echoed{"POST", "/sess/a/b.md", "view=raw&x=1", "127.0.0.1:7171", "kept", "payload"}
	if got != want {
		t.Errorf("backend saw %+v\nwant          %+v", got, want)
	}

	var kind string
	for _, m := range f.h.Mounts() {
		if m.Name == "sess" {
			kind = m.Kind
		}
	}
	if kind != "proxy" {
		t.Errorf("kind = %q, want proxy", kind)
	}
}

// A live record whose socket stopped answering: 502 naming the root.
func TestProxy_DeadSocketIs502(t *testing.T) {
	f := setup(t)
	sock, stop := echoBackend(t, f.st, "sess")
	lease, err := f.st.Register(registry.Record{Name: "sess", Root: "/some/root", Sock: sock})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	stop()
	rec := get(f.h.Handler(), "localhost", "/sess/")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "/some/root") {
		t.Errorf("dead socket: %d %q", rec.Code, rec.Body.String())
	}
}

// When the session goes away, the record and socket go with it: 404.
func TestProxy_ClosedSessionIs404(t *testing.T) {
	f := setup(t)
	sock, stop := echoBackend(t, f.st, "sess")
	defer stop()
	lease, err := f.st.Register(registry.Record{Name: "sess", Root: "/r", Sock: sock})
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
	if rec := get(f.h.Handler(), "localhost", "/sess/"); rec.Code != 404 {
		t.Errorf("after close: %d", rec.Code)
	}
}
