// Package hub serves many glow-web roots from one listener, each at
// /<name>/ (specs/unified-listener.md §4.B). Mounts come from explicit
// args and from the registry's pinned mounts. The registry is re-read on
// every request, so publish/unpublish apply without a restart.
//
// Routing looks only at the first path segment. The request reaches the
// mount's web.Server unchanged; that server was built with
// URLPrefix=<hub prefix>/<name> and strips its own prefix.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"sort"
	"strings"
	"sync"

	"github.com/mcint/glow-web/internal/registry"
	"github.com/mcint/glow-web/internal/version"
	"github.com/mcint/glow-web/internal/walk"
	"github.com/mcint/glow-web/internal/web"
)

// Mount is one served root.
type Mount struct {
	Name string `json:"name"`
	Root string `json:"root"`
	Kind string `json:"kind"` // "arg" (command line), "lease" (run/ record) or "pin" (mounts/)
	Live bool   `json:"live"`
	// Session details for run-record mounts.
	Version string `json:"version,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Sock    string `json:"sock,omitempty"` // proxy mounts: the session's socket
	// Contract is set for "incompatible" mounts: a record from a contract
	// major this hub doesn't speak. Listed, never routed.
	Contract int `json:"contract,omitempty"`
}

// Hub routes /<name>/… to per-root servers.
type Hub struct {
	State     *registry.State // nil: serve Extra only (ephemeral `web a/ b/`)
	Extra     []Mount         // explicit roots; they win over pins of the same name
	URLPrefix string          // hub mount point, e.g. "/docs"; "" for root
	// BaseURL is how browsers reach the hub (e.g. "http://127.0.0.1:7171"),
	// set once the TCP listener is bound; sessions print it via /_/hub.
	BaseURL string
	Walk    walk.Options // template for every mount (Root is overwritten)
	// Configure applies shared flags (theme, read-only, …) to each new
	// per-mount server. It may be nil.
	Configure func(*web.Server)

	mu      sync.Mutex
	servers map[string]cached // by name
}

type cached struct {
	key string // kind|root|sock: a change rebuilds the handler
	h   http.Handler
}

// Mounts returns the current mount table: Extra first, then pins whose
// names Extra doesn't already use, sorted by name.
func (h *Hub) Mounts() []Mount {
	seen := map[string]bool{}
	var out []Mount
	for _, m := range h.Extra {
		if seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		m.Kind, m.Live = "arg", true
		out = append(out, m)
	}
	if h.State != nil {
		// Live run records (rung 4 lease). Registration already refused a
		// record whose name a pin holds for another root, so a record
		// here shadows only a pin of the same root.
		records := h.State.Records()
		for _, r := range records {
			if seen[r.Name] || !r.Usable() {
				continue
			}
			seen[r.Name] = true
			out = append(out, Mount{Name: r.Name, Root: r.Root, Kind: r.Mode(), Live: true,
				Version: r.Version, PID: r.PID, Sock: h.State.SockPath(r)})
		}
		pins, _ := h.State.Pins()
		for _, p := range pins {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			out = append(out, Mount{Name: p.Name, Root: p.Root, Kind: "pin", Live: p.Live})
		}
		// Last: records this hub can't read. They are surfaced so skew is
		// visible, but never routed, and they never shadow a mount we can serve.
		for _, r := range records {
			if seen[r.Name] || r.Usable() {
				continue
			}
			seen[r.Name] = true
			out = append(out, Mount{Name: r.Name, Root: r.Root, Kind: "incompatible",
				Version: r.Version, PID: r.PID, Contract: r.Contract})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Handler returns the hub's HTTP handler (no Host check; see HostCheck).
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(h.serve)
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, h.URLPrefix)
	if !ok || (rest != "" && rest[0] != '/') {
		http.NotFound(w, r)
		return
	}
	switch {
	case rest == "" || rest == "/":
		if rest == "" {
			http.Redirect(w, r, h.URLPrefix+"/", http.StatusMovedPermanently)
			return
		}
		h.serveIndex(w, r)
		return
	case rest == "/_/hub":
		h.serveStatus(w)
		return
	}
	name, _, hasSlash := strings.Cut(rest[1:], "/")
	m, found := h.lookup(name)
	if !found {
		http.NotFound(w, r)
		return
	}
	if !hasSlash {
		http.Redirect(w, r, h.URLPrefix+"/"+name+"/", http.StatusMovedPermanently)
		return
	}
	if m.Kind == "incompatible" {
		http.Error(w, fmt.Sprintf("glow-web hub: %q was registered by glow-web %s using contract %d; this hub (%s) speaks contract %d. Restart the hub with a newer glow-web.",
			name, m.Version, m.Contract, version.String(), registry.Contract), http.StatusBadGateway)
		return
	}
	srv, err := h.server(m)
	if err != nil {
		http.Error(w, "mount "+name+": "+err.Error(), http.StatusBadGateway)
		return
	}
	srv.ServeHTTP(w, r)
}

func (h *Hub) lookup(name string) (Mount, bool) {
	if registry.ValidName(name) != nil {
		return Mount{}, false
	}
	for _, m := range h.Mounts() {
		if m.Name == name {
			return m, true
		}
	}
	return Mount{}, false
}

func (h *Hub) server(m Mount) (http.Handler, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := m.Kind + "|" + m.Root + "|" + m.Sock
	if c, ok := h.servers[m.Name]; ok && c.key == key {
		return c.h, nil
	}
	if h.servers == nil {
		h.servers = map[string]cached{}
	}
	if m.Kind == "proxy" {
		c := cached{key: key, h: proxyTo(m)}
		h.servers[m.Name] = c
		return c.h, nil
	}
	s, err := web.NewServer(m.Root, h.Walk)
	if err != nil {
		return nil, err
	}
	// The mount name is the tab title prefix by default: one port, many
	// projects, so the port no longer tells tabs apart.
	s.TitlePrefix = m.Name
	if h.Configure != nil {
		h.Configure(s)
	}
	s.URLPrefix = h.URLPrefix + "/" + m.Name
	s.HubURL = h.URLPrefix + "/"
	c := cached{key: key, h: s.Handler()}
	h.servers[m.Name] = c
	return c.h, nil
}

// proxyTo forwards to a session's unix socket verbatim (rung 5): path
// unstripped (the session knows its prefix), query, Host, headers and body
// as received. Only hop-by-hop headers are dropped, by the stdlib.
// FlushInterval -1 streams, ready for SSE or live reload.
func proxyTo(m Mount) http.Handler {
	sock := m.Sock
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "glow-web-session"
			pr.Out.Host = pr.In.Host
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sock)
			},
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, fmt.Sprintf("glow-web hub: %s (%s, pid %d) isn't answering on its socket: %v",
				m.Name, m.Root, m.PID, err), http.StatusBadGateway)
		},
	}
}

// Status is the /_/hub JSON body; `glow-web web` probes it to tell a
// glow-web hub from anything else holding the port (rung D).
type Status struct {
	Hub     string  `json:"hub"`
	Version string  `json:"version"`
	Prefix  string  `json:"prefix"`
	URL     string  `json:"url,omitempty"` // BaseURL + Prefix: mounts live at URL/<name>/
	Mounts  []Mount `json:"mounts"`
}

func (h *Hub) serveStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(Status{Hub: "glow-web", Version: version.String(), Prefix: h.URLPrefix, URL: h.urlOrEmpty(), Mounts: h.Mounts()})
}

func (h *Hub) urlOrEmpty() string {
	if h.BaseURL == "" {
		return ""
	}
	return h.BaseURL + h.URLPrefix
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>glow-web hub</title>
<style>
:root { color-scheme: light dark; --fg:#1f2328; --bg:#fff; --muted:#656d76; --line:#d0d7de; --link:#0969da; }
@media (prefers-color-scheme: dark) { :root { --fg:#e6edf3; --bg:#0d1117; --muted:#8d96a0; --line:#30363d; --link:#4493f8; } }
body { font: 15px/1.5 system-ui, sans-serif; color: var(--fg); background: var(--bg); max-width: 60rem; margin: 2rem auto; padding: 0 1rem; }
h1 { font-size: 1.2rem; } table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; padding: .35rem .6rem; border-bottom: 1px solid var(--line); }
th { color: var(--muted); font-weight: 500; } a { color: var(--link); }
.root { font-family: ui-monospace, monospace; font-size: .85em; color: var(--muted); word-break: break-all; }
.dead { opacity: .5; } .skew { font-weight: 600; } .skew::after { content: " ≠ hub"; font-weight: 400; color: var(--muted); } footer { color: var(--muted); font-size: .85em; margin-top: 1rem; }
</style></head><body>
<h1>glow-web hub</h1>
{{if .Mounts}}<table>
<tr><th>name</th><th>kind</th><th>version</th><th>root</th></tr>
{{range .Mounts}}<tr{{if not .Live}} class="dead"{{end}}>
<td><a href="{{$.Prefix}}/{{.Name}}/">{{.Name}}</a></td>
<td>{{.Kind}}{{if .Contract}} (contract {{.Contract}}){{end}}</td>
<td>{{if .Version}}<span{{if ne .Version $.Version}} class="skew" title="differs from this hub ({{$.Version}})"{{end}}>{{.Version}}</span>{{end}}</td>
<td class="root">{{.Root}}{{if and (not .Live) (ne .Kind "incompatible")}} (missing){{end}}</td></tr>
{{end}}</table>
{{else}}<p>Nothing mounted. <code>glow-web publish PATH</code> pins a directory here.</p>{{end}}
<footer>glow-web {{.Version}} · <a href="{{.Prefix}}/_/hub">/_/hub</a></footer>
</body></html>
`))

func (h *Hub) serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, map[string]any{
		"Prefix":  h.URLPrefix,
		"Mounts":  h.Mounts(),
		"Version": version.String(),
	})
}

// HostCheck rejects requests whose Host isn't a loopback name or in
// allow: a DNS-rebinding guard for the TCP listener. The unix-socket
// listener doesn't need it, because its 0600 mode is the gate (and lmux
// forwards its own Host).
func HostCheck(next http.Handler, allow []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
		if loopbackHost(host) || contains(allow, host) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "glow-web hub: Host "+r.Host+" not allowed (use --allow-host)", http.StatusForbidden)
	})
}

func loopbackHost(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
