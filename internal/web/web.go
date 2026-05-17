// Package web serves rendered markdown over HTTP. It supports two modes —
// single-file and directory — both honoring an optional URL prefix so the
// server can be reverse-proxied under a subpath. In dir mode the walk-result
// list is the security boundary: files not in the list 404.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mcint/glow-web/internal/gitstatus"
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
	Mode           Mode
	Root           string       // absolute path: a file (ModeFile) or dir (ModeDir)
	Walk           walk.Options // populated when Mode == ModeDir
	URLPrefix      string       // mount point, e.g. "/docs"; empty means no prefix
	ReadOnly       bool         // disables edit + save (also hides Edit link)
	DefaultMarkup  bool         // when true, default view is hybrid markup; ?view=rendered overrides
	CommandPalette bool         // when true, ⌘K / Ctrl-K opens a fuzzy file palette on every page
	Theme          string       // "auto" (default), "light", or "dark"; user toggle in UI overrides
	TitlePrefix    string       // appended to <title>; "auto" = "glow-web:<port>", "" disables, else literal
	TitlePort      string       // "auto" (port if known), "never" (omit), "always" — only affects "auto" prefix
	Addr           string       // bound address (e.g. "127.0.0.1:8080"); set by CLI and updated after net.Listen
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
	mux.HandleFunc("/_/files", s.handleFiles)
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
// The Server's Addr field is updated to the bound address so any
// "auto"-style title prefix can interpolate the real port.
func (s *Server) Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.Addr = ln.Addr().String()
	fmt.Fprintf(os.Stderr, "glow-web %s — serving %s\n", version.String(), s.Root)
	for _, line := range s.listenerLines(s.Addr) {
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

// urlForIndex builds the URL for the index page, optionally filtered to files
// whose Rel begins with prefixFilter. prefixFilter uses forward slashes and
// always ends with a slash (e.g. "sub/", "sub/deep/"); empty means no filter.
func (s *Server) urlForIndex(prefixFilter string) string {
	base := s.URLPrefix
	if base == "" {
		base = ""
	}
	if prefixFilter == "" {
		if base == "" {
			return "/"
		}
		return base + "/"
	}
	q := url.Values{}
	q.Set("prefix", prefixFilter)
	root := base
	if root == "" {
		root = ""
	}
	return root + "/?" + q.Encode()
}

// crumb is one segment of a breadcrumb nav. URL == "" means current page.
type crumb struct {
	Name string
	URL  string
}

// crumbsForFile builds breadcrumbs for a file view: project-name-or-file →
// each path segment → the file itself. In ModeFile we have no index to link
// back to, so the file is the only crumb.
func (s *Server) crumbsForFile(rel, displayName string) []crumb {
	if s.Mode == ModeFile {
		return []crumb{{Name: displayName}}
	}
	out := []crumb{{Name: filepath.Base(s.Root), URL: s.urlForIndex("")}}
	if rel == "" {
		return out
	}
	parts := strings.Split(rel, "/")
	var accum string
	for i, p := range parts {
		last := i == len(parts)-1
		if last {
			out = append(out, crumb{Name: p})
			continue
		}
		accum += p + "/"
		out = append(out, crumb{Name: p, URL: s.urlForIndex(accum)})
	}
	return out
}

// crumbsForIndex builds breadcrumbs for the index view at a given prefix
// filter. Empty prefix returns just the project root crumb (current).
func (s *Server) crumbsForIndex(prefixFilter string) []crumb {
	rootName := filepath.Base(s.Root)
	if prefixFilter == "" {
		return []crumb{{Name: rootName}}
	}
	out := []crumb{{Name: rootName, URL: s.urlForIndex("")}}
	cleaned := strings.TrimSuffix(prefixFilter, "/")
	parts := strings.Split(cleaned, "/")
	var accum string
	for i, p := range parts {
		last := i == len(parts)-1
		accum += p + "/"
		if last {
			out = append(out, crumb{Name: p})
		} else {
			out = append(out, crumb{Name: p, URL: s.urlForIndex(accum)})
		}
	}
	return out
}

// displayRoot returns a friendly form of an absolute path: "~/foo" if it lies
// under the user's home, otherwise the absolute path verbatim.
func displayRoot(abs string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return abs
	}
	if abs == home {
		return "~"
	}
	if strings.HasPrefix(abs, home+string(os.PathSeparator)) {
		return "~" + abs[len(home):]
	}
	return abs
}

// parentHint returns the display-form path of the directory above the serving
// root, anchoring the breadcrumb in the surrounding filesystem context.
// Empty string when the root is "/" (no parent) or when filepath.Dir reports
// the same path (root is a filesystem root).
func (s *Server) parentHint() string {
	parent := filepath.Dir(s.Root)
	if parent == s.Root || parent == "" {
		return ""
	}
	return displayRoot(parent)
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
		body, err = render.HTMLWithOptions(raw, render.Options{
			LinkResolver: s.linkResolver(rel),
		})
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
		ParentHint:  s.parentHint(),
		Crumbs:      s.crumbsForFile(rel, displayName),
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
	data.Palette = s.CommandPalette
	data.FilesURL = s.utilityURL("/_/files")
	data.ServerTheme = s.serverTheme()
	data.KeyPrefix = s.keyPrefix()
	data.TitlePrefix = s.resolvedTitlePrefix()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "view.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: view template: %v\n", err)
	}
}

func (s *Server) serveEdit(w http.ResponseWriter, src []byte, displayName, rel string) {
	selfURL := s.urlFor(rel)
	data := pageData{
		Title:       displayName + " (edit)",
		Name:        displayName,
		Path:        displayPath(displayName, rel),
		ParentHint:  s.parentHint(),
		Crumbs:      s.crumbsForFile(rel, displayName),
		Source:      string(src),
		ViewURL:     selfURL,
		SaveURL:     selfURL,
		RenderURL:   s.utilityURL("/_/render"),
		FilesURL:    s.utilityURL("/_/files"),
		ShowSave:    true,
		Palette:     s.CommandPalette,
		ServerTheme: s.serverTheme(),
		KeyPrefix:   s.keyPrefix(),
		TitlePrefix: s.resolvedTitlePrefix(),
		Version:     version.String(),
	}
	if s.Mode == ModeDir {
		data.IndexURL = s.urlFor("")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "edit.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: edit template: %v\n", err)
	}
}

// utilityURL builds an externally-visible URL for an in-server endpoint
// (e.g. /_/render, /_/files), accounting for the URL prefix mount.
func (s *Server) utilityURL(p string) string {
	if s.URLPrefix == "" {
		return p
	}
	return s.URLPrefix + p
}

// serverTheme normalises the configured Theme into one of "auto", "light",
// or "dark". The value is baked into the theme-init script so the page can
// pick the right colour scheme before paint.
func (s *Server) serverTheme() string {
	switch s.Theme {
	case "light", "dark":
		return s.Theme
	default:
		return "auto"
	}
}

// resolvedTitlePrefix returns the literal string to render after the doc
// title. The sentinel "auto" expands to "glow-web" plus the bound port
// (or not) per s.TitlePort: "auto"/"always" include the port when known,
// "never" omits it. Empty TitlePrefix disables the suffix entirely; any
// other literal value passes through verbatim regardless of TitlePort.
func (s *Server) resolvedTitlePrefix() string {
	if s.TitlePrefix == "" {
		return ""
	}
	if s.TitlePrefix != "auto" {
		return s.TitlePrefix
	}
	if s.TitlePort == "never" {
		return "glow-web"
	}
	port := s.boundPort()
	if port == "" {
		return "glow-web"
	}
	return "glow-web:" + port
}

// boundPort extracts the port from s.Addr. Returns "" when Addr isn't set
// (e.g. tests using Handler() without Serve()).
func (s *Server) boundPort() string {
	if s.Addr == "" {
		return ""
	}
	_, port, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return ""
	}
	return port
}

// keyPrefix is the localStorage namespace for this server's UI state. Two
// glow-web instances running on the same browser origin (same host:port)
// would otherwise collide on `glow-theme`, `glow-palette-open`,
// `glow-scroll:<path>`, etc. Hashing the absolute root gives a stable per-
// project namespace without leaking the path itself into the page source.
//
// FNV-1a (32-bit, 8 hex chars) is plenty: collision likelihood across the
// handful of projects on one machine is negligible, and the hash isn't a
// security boundary — just a cache key.
func (s *Server) keyPrefix() string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s.Root))
	return fmt.Sprintf("glow-%08x-", h.Sum32())
}

// displayPath returns the friendly "/foo.md" or "/sub/foo.md" string shown in
// the bar and footer. For ModeFile (rel == "") we fall back to the basename.
func displayPath(displayName, rel string) string {
	if rel == "" {
		return "/" + displayName
	}
	return "/" + rel
}

// linkResolver returns a function that maps a markdown link destination to
// the served URL of the same file in this project, when the destination is
// (a) relative, (b) resolvable to a path inside Root, and (c) corresponds to
// a file in the walk result. Otherwise the destination is left unchanged so
// external/anchor/non-md links continue to work.
//
// In ModeFile we have no project to resolve into, so the resolver is nil
// (links pass through to the rendered HTML verbatim).
func (s *Server) linkResolver(currentRel string) func(string) (string, bool) {
	if s.Mode != ModeDir {
		return nil
	}
	files, err := walk.Files(s.Walk)
	if err != nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(files))
	for _, f := range files {
		if f.Class != "md" {
			continue
		}
		allowed[f.Rel] = struct{}{}
	}
	return func(href string) (string, bool) {
		if href == "" || isExternalLink(href) || strings.HasPrefix(href, "#") {
			return "", false
		}
		// Strip and preserve query/fragment for re-attachment.
		bare, suffix := splitURLSuffix(href)
		decoded, err := url.PathUnescape(bare)
		if err != nil {
			decoded = bare
		}
		target := resolveRelativeLink(currentRel, decoded)
		if target == "" {
			return "", false
		}
		if _, ok := allowed[target]; !ok {
			return "", false
		}
		return s.urlFor(target) + suffix, true
	}
}

// isExternalLink returns true for hrefs that should never be rewritten —
// anything with a URL scheme, a protocol-relative form, or a mailto/tel/etc.
func isExternalLink(href string) bool {
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "//") {
		return true
	}
	// Detect "scheme:" prefix: a colon before any '/', '?', or '#'.
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c == ':' {
			return true
		}
		if c == '/' || c == '?' || c == '#' {
			return false
		}
	}
	return false
}

// splitURLSuffix separates a bare path from its query and/or fragment, so
// the path can be resolved on its own and the suffix reattached intact.
func splitURLSuffix(href string) (bare, suffix string) {
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		return href[:i], href[i:]
	}
	return href, ""
}

// resolveRelativeLink joins href to currentRel's directory (for pure relative
// hrefs) or to the project root (for "/" anchored hrefs), then path-cleans the
// result. Anything that escapes the project root after cleaning is rejected
// (returns "") — the caller treats that as "leave the link unchanged."
func resolveRelativeLink(currentRel, href string) string {
	if href == "" {
		return ""
	}
	var base string
	if strings.HasPrefix(href, "/") {
		base = strings.TrimPrefix(href, "/")
	} else {
		dir := path.Dir(currentRel)
		if dir == "." || dir == "" {
			base = href
		} else {
			base = dir + "/" + href
		}
	}
	cleaned := path.Clean(base)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "..") {
		return ""
	}
	return cleaned
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
	// Widen the walk for the index view: include text, "other", hidden,
	// and gitignored files so the client-side toggles have every row to
	// show or hide. The serving allowlist (findFile + link rewriting)
	// keeps requiring Class == "md", so widening here does not relax
	// what we actually serve.
	indexOpts := s.Walk
	indexOpts.IncludeText = true
	indexOpts.IncludeAll = true
	indexOpts.IncludeHidden = true
	indexOpts.IncludeIgnored = true
	files, err := walk.Files(indexOpts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Second walk with the user's actual options: this is the set whose
	// rows get a clickable link. Anything in the wide walk but not the
	// narrow walk is listing-only (no <a>), regardless of its class.
	narrow, err := walk.Files(s.Walk)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	servable := make(map[string]struct{}, len(narrow))
	for _, f := range narrow {
		if f.Class == "md" {
			servable[f.Rel] = struct{}{}
		}
	}
	// gitstatus is best-effort: missing repo, missing `git`, or a timeout
	// all yield an empty map, and the columns just render blank.
	gits, _ := gitstatus.Status(s.Root)
	prefixFilter := normalizePrefixFilter(r.URL.Query().Get("prefix"))

	items := make([]indexItem, 0, len(files))
	visible := 0 // count of rows the default toggle state (md/normal) will show
	for _, f := range files {
		if prefixFilter != "" && !strings.HasPrefix(f.Rel, prefixFilter) {
			continue
		}
		url := ""
		if _, ok := servable[f.Rel]; ok {
			url = s.urlFor(f.Rel)
		}
		var xy string
		var adds, dels int
		if g, ok := gits[f.Rel]; ok {
			xy = g.XY()
			adds = g.Adds
			dels = g.Dels
		}
		items = append(items, indexItem{
			Rel:      f.Rel,
			Display:  strings.TrimPrefix(f.Rel, prefixFilter),
			Name:     f.Name,
			URL:      url,
			Size:     f.Size,
			Mtime:    f.ModTime.Unix(),
			MtimeRel: relTimeShort(time.Since(f.ModTime)),
			Class:    f.Class,
			Hidden:   f.Hidden,
			Ignored:  f.Ignored,
			GitXY:    xy,
			GitAdds:  adds,
			GitDels:  dels,
		})
		if f.Class == "md" && !f.Hidden && !f.Ignored {
			visible++
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := indexData{
		Title:       filepath.Base(s.Root),
		Path:        displayRoot(s.Root),
		ParentHint:  s.parentHint(),
		Crumbs:      s.crumbsForIndex(prefixFilter),
		Filter:      prefixFilter,
		Files:       items,
		Count:       visible,
		FilesURL:    s.utilityURL("/_/files"),
		Palette:     s.CommandPalette,
		ServerTheme: s.serverTheme(),
		KeyPrefix:   s.keyPrefix(),
		TitlePrefix: s.resolvedTitlePrefix(),
		Version:     version.String(),
	}
	if err := pageTmpl.ExecuteTemplate(w, "index.html.tmpl", data); err != nil {
		fmt.Fprintf(os.Stderr, "glow-web: index template: %v\n", err)
	}
}

// relTimeShort renders a duration in the compact "5m" / "3h" / "2d" form
// the index/palette JS uses, so the server-rendered initial paint matches
// what the client would compute on its own.
func relTimeShort(d time.Duration) string {
	s := d.Seconds()
	if s < 0 {
		s = 0
	}
	switch {
	case s < 45:
		return "now"
	case s < 3600:
		return fmt.Sprintf("%dm", int(s/60+0.5))
	case s < 86400:
		return fmt.Sprintf("%dh", int(s/3600+0.5))
	case s < 86400*7:
		return fmt.Sprintf("%dd", int(s/86400+0.5))
	case s < 86400*30:
		return fmt.Sprintf("%dw", int(s/(86400*7)+0.5))
	case s < 86400*365:
		return fmt.Sprintf("%dmo", int(s/(86400*30)+0.5))
	default:
		return fmt.Sprintf("%dy", int(s/(86400*365)+0.5))
	}
}

// normalizePrefixFilter cleans a user-supplied prefix into the canonical
// "sub/" or "sub/deep/" form (forward slashes, trailing slash, no leading
// slash, no dotty traversal).
func normalizePrefixFilter(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "/")
	cleaned := path.Clean(s)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return ""
	}
	if !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned
}

// handleFiles emits the project's discovered file list as JSON, used as the
// data source for the command palette. Walk-result-based, so gitignored
// files don't leak (matches the index and link-rewriting allowlist).
//
// mtime ships as Unix seconds so the filter can render a relative-time
// hint without another round-trip.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type entry struct {
		Rel   string `json:"rel"`
		URL   string `json:"url"`
		Mtime int64  `json:"mtime"`
	}
	var out []entry
	if s.Mode == ModeDir {
		files, err := walk.Files(s.Walk)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = make([]entry, len(files))
		for i, f := range files {
			out[i] = entry{Rel: f.Rel, URL: s.urlFor(f.Rel), Mtime: f.ModTime.Unix()}
		}
	} else {
		// Single-file mode: one entry, the file itself.
		var mt int64
		if info, err := os.Stat(s.Root); err == nil {
			mt = info.ModTime().Unix()
		}
		out = []entry{{Rel: filepath.Base(s.Root), URL: s.urlFor(""), Mtime: mt}}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
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
// attempts before consulting the list and refuses anything whose Class
// isn't "md" — defense-in-depth so widening Walk options elsewhere can
// never accidentally make non-markdown files servable. Raw-text serving
// for "text" class files is a future feature with its own gate.
func findFile(files []walk.File, rel string) *walk.File {
	clean := path.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		return nil
	}
	for i := range files {
		if files[i].Rel == clean && files[i].Class == "md" {
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
	ParentHint  string // path above the serving root, e.g. "~/dev-llm"; empty at "/"
	Crumbs      []crumb
	Body        template.HTML
	Source      string // edit page only
	IndexURL    string
	ViewURL     string
	RawURL      string
	DownloadURL string
	EditURL     string
	SaveURL     string
	RenderURL   string
	FilesURL    string // /_/files endpoint, used by command palette
	ToggleURL   string // url to flip between rendered and markup views
	ToggleLabel string // label shown on the toggle button
	ShowSave    bool
	Markup      bool
	Palette     bool   // include command palette overlay + script
	ServerTheme string // "auto" | "light" | "dark"; baked into theme-init script
	KeyPrefix   string // localStorage namespace, scoped per project root
	TitlePrefix string // prepended to <title>; empty disables
	Version     string
}

type indexItem struct {
	Rel      string
	Display  string // path with current prefix-filter trimmed off
	Name     string
	URL      string // empty when Class != "md" — template renders plain text instead of <a>
	Size     int64
	Mtime    int64  // Unix seconds, surfaced as data-mtime on the row for client-side sort/filter
	MtimeRel string // compact relative form ("5m", "3d", …) for initial paint; JS recomputes on filter
	Class    string // "md" | "text" | "other" — drives data-class for the class-cycle toggle
	Hidden   bool   // dotfile or under a dot-dir; drives data-hidden for the hidden-cycle toggle
	Ignored  bool   // matched gitignore but kept due to IncludeIgnored; UI dims the row
	GitXY    string // two-char porcelain status ("M ", " M", "??", …); empty when clean / no repo
	GitAdds  int    // numstat additions vs HEAD; 0 when clean / not in repo / binary
	GitDels  int    // numstat deletions vs HEAD
}

type indexData struct {
	Title       string
	Path        string  // home-relative project root, shown in footer
	ParentHint  string  // path above the serving root, e.g. "~/dev-llm"; empty at "/"
	Crumbs      []crumb // breadcrumb nav for current prefix filter
	Filter      string  // current prefix filter (e.g. "sub/"); empty means root
	Files       []indexItem
	Count       int
	FilesURL    string // /_/files endpoint, used by command palette
	Palette     bool   // include command palette overlay + script
	ServerTheme string // "auto" | "light" | "dark"; baked into theme-init script
	KeyPrefix   string // localStorage namespace, scoped per project root
	TitlePrefix string // prepended to <title>; empty disables
	Version     string
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
