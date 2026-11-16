// Command glow-web is the entry point for the web/tui/slice/render/version
// subcommands. Each subcommand owns its own flag.FlagSet; the top-level
// dispatcher stays a switch.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mcint/glow-web/internal/auth"
	"github.com/mcint/glow-web/internal/hub"
	"github.com/mcint/glow-web/internal/registry"
	"github.com/mcint/glow-web/internal/render"
	"github.com/mcint/glow-web/internal/slice"
	"github.com/mcint/glow-web/internal/version"
	"github.com/mcint/glow-web/internal/web"
)

const usage = `glow-web — markdown reader for web, TUI, and CLI slicing

usage: glow-web <command> [options] [path]
       glow-web --version

commands:
  web      serve a markdown file or directory as HTML (several PATHs: one
           port, each at /<name>/)
  hub      serve every published dir at /<name>/ on one port (default
           127.0.0.1:7171) and on <state>/run/hub.sock
  publish  pin PATH to the hub at /<name>/ until unpublished
  unpublish NAME   remove a pin (never touches the dir itself)
  ls       list published mounts and whether the hub runs
  passwd USER      print an htpasswd (bcrypt) line; append it to
           <state>/htpasswd to turn on hub auth
  slice    extract a section by heading path
  render   render a markdown file to stdout (html | markup)
  name     print the mount name derived from PATH (default .)
  tui      open the markdown file in a terminal UI (stub in v0)
  version  print the build version
  help     print this message

run 'glow-web <command> -h' for command-specific help.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "web":
		runWeb(args)
	case "hub":
		runHub(args)
	case "publish":
		runPublish(args)
	case "unpublish":
		runUnpublish(args)
	case "ls":
		runLs(args)
	case "passwd":
		runPasswd(args)
	case "name":
		runName(args)
	case "slice":
		runSlice(args)
	case "render":
		runRender(args)
	case "tui":
		fmt.Fprintln(os.Stderr, "glow-web: tui subcommand is a v0 stub; use 'glow-web web' for now")
		os.Exit(1)
	case "version", "--version", "-v":
		fmt.Println("glow-web", version.String())
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "glow-web: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", `address to listen on (default loopback only; use ":8080" or "0.0.0.0:8080" to expose on LAN)`)
	listenSpec := fs.String("listen", "", `listen spec, overrides --addr: "tcp:HOST:PORT" or "unix:PATH" (socket mode 0600; a dead socket file is replaced, a live one is refused)`)
	prefix := fs.String("url-prefix", "", `mount under URL prefix, e.g. "/docs"`)
	hubMode := fs.String("hub", "off", `join the host's glow-web hub instead of binding a port: "off" (default) | "lease" (the hub serves PATH at /<name>/ while this process runs) | "proxy" (this process serves PATH on its own socket; the hub forwards /<name>/ to it) | "auto" (lease when the hub runs this exact build, else proxy)`)
	name := fs.String("name", "", "mount name under the hub (default: derived from PATH; see `glow-web name`)")
	hubAddr := fs.String("hub-addr", envOr("GLOW_WEB_HUB_ADDR", DefaultHubAddr), "where an elected hub listens (env GLOW_WEB_HUB_ADDR)")
	elect := fs.Bool("hub-elect", true, "with --hub: become the hub when none is running, and take over when it exits")
	authFile := fs.String("auth-htpasswd", "", "htpasswd file (bcrypt) for HTTP Basic on a TCP listener; standalone and `web a/ b/` only (a hub uses <state>/htpasswd)")
	sf := addServerFlags(fs)
	parseIntermixed(fs, args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "glow-web web: missing PATH (file or directory)")
		os.Exit(2)
	}
	if err := checkAuthFlag(*hubMode, *authFile); err != nil {
		die(err)
	}
	var el *hub.Elector
	if *hubMode != "off" {
		el = sessionElector(registry.DefaultState(), *hubAddr, sf, *elect)
	}
	if *hubMode == "auto" {
		st := registry.DefaultState()
		hs, up := hubStatus(st)
		if !up && el != nil {
			// No hub: try to be it. Then the hub is this exact build.
			if won, _ := el.TryOnce(); won {
				hs, up = hub.Status{Version: version.String()}, true
			}
		}
		*hubMode = autoMode(up, hs.Version, version.String())
		fmt.Fprintf(os.Stderr, "glow-web: --hub=auto → %s\n", *hubMode)
	}
	switch *hubMode {
	case "off":
	case "lease":
		if fs.NArg() > 1 {
			die(fmt.Errorf("--hub=lease takes one PATH"))
		}
		runLease(fs.Arg(0), *name, *sf.readonly, el)
		return
	case "proxy":
		if fs.NArg() > 1 {
			die(fmt.Errorf("--hub=proxy takes one PATH"))
		}
		runProxy(fs.Arg(0), *name, sf, el)
		return
	default:
		die(fmt.Errorf("--hub=%q: want off, lease, proxy or auto", *hubMode))
	}
	spec := *addr
	if *listenSpec != "" {
		spec = *listenSpec
	}

	// Several roots: an ephemeral hub over just these, each at /<name>/.
	// It reads no registry and binds no hub.sock.
	if fs.NArg() > 1 {
		extra, err := argMounts(fs.Args())
		if err != nil {
			die(err)
		}
		h, err := newHub(nil, extra, *prefix, sf)
		if err != nil {
			die(err)
		}
		die(serveEphemeral(h, spec, standaloneGate(*authFile)))
	}

	s, err := web.NewServer(fs.Arg(0), sf.walk())
	if err != nil {
		die(err)
	}
	cfg, err := sf.apply(false)
	if err != nil {
		die(err)
	}
	cfg(s)
	s.URLPrefix = normalizePrefix(*prefix)
	if g := standaloneGate(*authFile); g != nil {
		s.TCPGate = g.Wrap
		authNotice(g, spec)
	}
	if err := s.Serve(spec); err != nil {
		die(err)
	}
}

// runName prints the name a root mounts under: lmux's kebab(basename),
// made URL-safe (specs/unified-listener.md §3).
func runName(args []string) {
	fs := flag.NewFlagSet("name", flag.ExitOnError)
	parseIntermixed(fs, args)
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		die(err)
	}
	fmt.Println(registry.NameFor(abs))
}

func runSlice(args []string) {
	fs := flag.NewFlagSet("slice", flag.ExitOnError)
	heading := fs.String("heading", "", `heading path, e.g. "Architecture / Slicing"`)
	parseIntermixed(fs, args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "glow-web slice: missing PATH")
		os.Exit(2)
	}
	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		die(err)
	}
	path := parseHeadingPath(*heading)
	out, ok := slice.BySection(src, path)
	if !ok {
		fmt.Fprintf(os.Stderr, "glow-web slice: no section matched %q\n", *heading)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(out)
}

func runRender(args []string) {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	format := fs.String("format", "html", "output format (html | markup)")
	parseIntermixed(fs, args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "glow-web render: missing PATH")
		os.Exit(2)
	}
	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		die(err)
	}
	switch *format {
	case "html":
		out, err := render.HTML(src)
		if err != nil {
			die(err)
		}
		_, _ = os.Stdout.Write(out)
	case "markup":
		_, _ = os.Stdout.Write(render.HTMLMarkup(src))
	default:
		fmt.Fprintf(os.Stderr, "glow-web render: unsupported format %q (try 'html' or 'markup')\n", *format)
		os.Exit(2)
	}
}

func parseHeadingPath(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizePrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

// parseIntermixed lets users put flags before or after positional args. Stdlib
// flag.Parse stops at the first positional, which is hostile for CLIs where
// `cmd PATH --flag` is a natural order. We pre-split known flag tokens (bool
// flags consume zero values, others consume one) and feed them to fs.Parse,
// then re-attach positionals via fs.Parse for NArg/Arg access.
func parseIntermixed(fs *flag.FlagSet, args []string) {
	bools := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		if g, ok := f.Value.(flag.Getter); ok {
			if _, isBool := g.Get().(bool); isBool {
				bools[f.Name] = true
			}
		}
	})
	var flagArgs, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if bools[name] {
			continue
		}
		if i+1 < len(args) {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}
	_ = fs.Parse(append(flagArgs, positional...))
}

// standaloneGate is auth for a non-hub listener: only when asked for.
func standaloneGate(path string) *auth.Gate {
	if path == "" {
		return nil
	}
	g := &auth.Gate{Path: path}
	if err := g.Check(); err != nil {
		die(err)
	}
	return g
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "glow-web: %v\n", err)
	os.Exit(1)
}
