// Package web serves rendered markdown over HTTP. It supports two modes —
// single-file and directory — both honoring an optional URL prefix so the
// server can be reverse-proxied under a subpath. In dir mode the walk-result
// list is the security boundary: files not in the list 404.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mcint/glow-web/internal/render"
	"github.com/mcint/glow-web/internal/version"
	"github.com/mcint/glow-web/internal/walk"
)

//go:embed templates/*.html.tmpl
var templateFS embed.FS

var pageTmpl = template.Must(template.ParseFS(templateFS, "templates/*.html.tmpl"))

// Mode distinguishes single-file vs directory serving.
type Mode int

const (
	ModeFile Mode = iota
	ModeDir
)

// Server is an HTTP markdown viewer.
type Server struct {
	Mode          Mode
	Root          string       // absolute path: a file (ModeFile) or dir (ModeDir)
	Walk          walk.Options // populated when Mode == ModeDir
	URLPrefix     string       // mount point, e.g. "/docs"; empty means no prefix
	ReadOnly      bool         // disables edit + save (also hides Edit link)
	DefaultMarkup bool         // when true, default view is hybrid markup; ?view=rendered overrides
}

// NewServer constructs a Server, autodetecting Mode from path's stat. walkOpts
// applies only in dir mode; its Root is overwritten with the resolved abs path.
func NewServer(p string, walkOpts walk.Options) (*Server, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	s := &Server{Root: abs}
	if info.IsDir() {
		s.Mode = ModeDir
		walkOpts.Root = abs
		s.Walk = walkOpts
	}
	return s, nil
}

// Handler returns the HTTP handler. The handler is wrapped in
// http.StripPrefix when URLPrefix is non-empty, so internally registered
// routes always see paths without the prefix.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_/render", s.handleRender)
	mux.HandleFunc("/", s.handleRoot)
	if s.URLPrefix == "" {
		return mux
	}
	return http.StripPrefix(s.URLPrefix, mux)
}

// Serve binds addr and blocks. Useful for the CLI; tests should use Handler().
//
// We bind first then log so the URLs reflect the actual port (handles `:0`)
// and a bind failure surfaces before the misleading "serving …" notice.
func (s *Server) Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "glow-web %s — serving %s\n", version.String(), s.Root)
	for _, line := range s.listenerLines(ln.Addr().String()) {
		fmt.Fprintln(os.Stderr, line)
	}
	return http.Serve(ln, s.Handler())
}

// listenerLines formats one line per reachable URL for the bound address.
// Wildcard binds expand to localhost plus any non-loopback IPv4 interfaces;
// specific binds emit a single line. Each URL is on its own line with no
// trailing punctuation so terminal linkifiers (iTerm2, VS Code, kitty, …)
// turn it into a clickable target.
func (s *Server) listenerLines(addr string) []string {
	return formatListenerLines(addr, s.URLPrefix, nonLoopbackIPv4s())
}

// formatListenerLines is the pure version of listenerLines: lanIPs is passed
// in so tests can drive deterministic output without depending on the host's
// network configuration.
func formatListenerLines(addr, urlPrefix string, lanIPs []string) []string {
	suffix := urlPrefix + "/"
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{"    http://" + addr + suffix}
	}
	if !isWildcardHost(host) {
		return []string{"    " + buildURL(host, port, suffix)}
	}
	out := []string{"    " + buildURL("localhost", port, suffix) + "  (loopback)"}
	for _, ip := range lanIPs {
		out = append(out, "    "+buildURL(ip, port, suffix)+"  (lan)")
	}
	return out
}

func isWildcardHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

func buildURL(host, port, suffix string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + host + ":" + port + suffix
}

func nonLoopbackIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	return out
}

// urlFor builds an externally-visible URL for the given relative file path
// (forward-slash form, no leading slash). rel == "" returns the prefix root.
func (s *Server) urlFor(rel string) string {
	base := s.URLPrefix
	if rel == "" {
		if base == "" {
			return "/"
		}
		return base + "/"
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return base + "/" + strings.Join(parts, "/")
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleSave(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch s.Mode {
	case ModeFile:
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.serveOne(w, r, s.Root, filepath.Base(s.Root), "")
	case ModeDir:
		if r.URL.Path == "/" {
			s.serveIndex(w, r)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/")
		files, err := walk.Files(s.Walk)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		match := findFile(files, rel)
		if match == nil {
			http.NotFound(w, r)
			return
		}
		s.serveOne(w, r, match.Abs, match.Name, match.Rel)
	}
}

// serveOne handles a single markdown URL: the rendered HTML view, plus the
// ?raw=1 / ?download=1 / ?edit=1 short-circuits.
func (s *Server) serveOne(w http.ResponseWriter, r *http.Request, abs, displayName, rel string) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	if q.Get("raw") == "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(raw)
		return
	}
	if q.Get("download") == "1" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename=%q`, displayName))
		_, _ = w.Write(raw)
		return
	}
	if q.Get("edit") == "1" {
		if s.ReadOnly {
			http.Error(w, "edit disabled (--readonly)", http.StatusForbidden)
			return
		}
		s.serveEdit(w, raw, displayName, rel)
		return
	}

	markup := s.viewIsMarkup(q.Get("view"))
	var body []byte
	if markup {
		body = render.HTMLMarkup(raw)
	} else {
		body, err = render.HTML(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	selfURL := s.urlFor(rel)
	data := pageData{
		Title:       displayName,
		Name:        displayName,
		Path:        displayPath(displayName, rel),
		Body:        template.HTML(body),
		ViewURL:     selfURL,
		RawURL:      selfURL + "?raw=1",
		DownloadURL: selfURL + "?download=1",
		Version:     version.String(),
		Markup:      markup,
	}
	if markup {
		data.ToggleURL = selfURL + "?view=rendered"
		data.ToggleLabel = "Rendered"
	} else {
		data.ToggleURL = selfURL + "?view=markup"
		data.ToggleLabel = "Markup"
	}
	if !s.ReadOnly {
		data.EditURL = selfURL + "?edit=1"
	}
	if s.Mode == ModeDir {
		data.IndexURL = s.urlFor("")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "view.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: view template: %v\n", err)
	}
}

func (s *Server) serveEdit(w http.ResponseWriter, src []byte, displayName, rel string) {
	selfURL := s.urlFor(rel)
	renderURL := "/_/render"
	if s.URLPrefix != "" {
		renderURL = s.URLPrefix + "/_/render"
	}
	data := pageData{
		Title:     displayName + " (edit)",
		Name:      displayName,
		Path:      displayPath(displayName, rel),
		Source:    string(src),
		ViewURL:   selfURL,
		SaveURL:   selfURL,
		RenderURL: renderURL,
		ShowSave:  true,
		Version:   version.String(),
	}
	if s.Mode == ModeDir {
		data.IndexURL = s.urlFor("")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "edit.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: edit template: %v\n", err)
	}
}

// displayPath returns the friendly "/foo.md" or "/sub/foo.md" string shown in
// the bar and footer. For ModeFile (rel == "") we fall back to the basename.
func displayPath(displayName, rel string) string {
	if rel == "" {
		return "/" + displayName
	}
	return "/" + rel
}

// viewIsMarkup decides which renderer to use for the main view. The query
// parameter wins; otherwise we fall back to the server default.
func (s *Server) viewIsMarkup(q string) bool {
	switch q {
	case "markup":
		return true
	case "rendered":
		return false
	default:
		return s.DefaultMarkup
	}
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	files, err := walk.Files(s.Walk)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]indexItem, 0, len(files))
	for _, f := range files {
		items = append(items, indexItem{
			Rel:  f.Rel,
			Name: f.Name,
			URL:  s.urlFor(f.Rel),
			Size: f.Size,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := indexData{
		Title:   filepath.Base(s.Root),
		Path:    s.Root,
		Files:   items,
		Count:   len(items),
		Version: version.String(),
	}
	if err := pageTmpl.ExecuteTemplate(w, "index.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: index template: %v\n", err)
	}
}

// handleRender powers the live-preview pane: POST a markdown body, get the
// HTML fragment back. 1 MiB cap is plenty for one document.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := render.HTML(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(out)
}

// handleSave writes the request body to the file's location on disk, gated
// by ReadOnly and the same path-resolution rules as serveOne.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if s.ReadOnly {
		http.Error(w, "save disabled (--readonly)", http.StatusForbidden)
		return
	}
	var abs string
	switch s.Mode {
	case ModeFile:
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		abs = s.Root
	case ModeDir:
		rel := strings.TrimPrefix(r.URL.Path, "/")
		files, err := walk.Files(s.Walk)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		match := findFile(files, rel)
		if match == nil {
			http.NotFound(w, r)
			return
		}
		abs = match.Abs
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// findFile gates serving on the walk result. It rejects path-traversal
// attempts even before consulting the list.
func findFile(files []walk.File, rel string) *walk.File {
	clean := path.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		return nil
	}
	for i := range files {
		if files[i].Rel == clean {
			return &files[i]
		}
	}
	return nil
}

// pageData / indexData feed the templates. Both expose Path + Version so the
// shared footer template can pull from either.

type pageData struct {
	Title       string
	Name        string
	Path        string
	Body        template.HTML
	Source      string // edit page only
	IndexURL    string
	ViewURL     string
	RawURL      string
	DownloadURL string
	EditURL     string
	SaveURL     string
	RenderURL   string
	ToggleURL   string // url to flip between rendered and markup views
	ToggleLabel string // label shown on the toggle button
	ShowSave    bool
	Markup      bool
	Version     string
}

type indexItem struct {
	Rel  string
	Name string
	URL  string
	Size int64
}

type indexData struct {
	Title   string
	Path    string
	Files   []indexItem
	Count   int
	Version string
}

// Backwards-compat shims for the existing CLI and tests.

// FileHandler returns a handler that serves mdPath as a single file.
func FileHandler(mdPath string) http.Handler {
	s, err := NewServer(mdPath, walk.Options{})
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		})
	}
	return s.Handler()
}

// ServeFile binds addr and serves mdPath as a single file.
func ServeFile(addr, mdPath string) error {
	s, err := NewServer(mdPath, walk.Options{})
	if err != nil {
		return err
	}
	return s.Serve(addr)
}
