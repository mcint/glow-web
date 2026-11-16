package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mcint/glow-web/internal/hub"
	"github.com/mcint/glow-web/internal/listen"
	"github.com/mcint/glow-web/internal/registry"
	"github.com/mcint/glow-web/internal/version"
)

// DefaultHubAddr is the hub's TCP listener (fork N7 in
// specs/unified-listener.md: 7171 until someone objects).
const DefaultHubAddr = "127.0.0.1:7171"

func runHub(args []string) {
	fs := flag.NewFlagSet("hub", flag.ExitOnError)
	addr := fs.String("addr", DefaultHubAddr, "TCP address for the hub (loopback by default)")
	listenSpec := fs.String("listen", "", `listen spec, overrides --addr: "tcp:HOST:PORT" or "unix:PATH"`)
	prefix := fs.String("url-prefix", "", `mount the whole hub under a prefix, e.g. "/docs" (mounts then live at /docs/<name>/)`)
	allowHost := fs.String("allow-host", "", "comma-separated extra Host names the TCP listener accepts (loopback names always are)")
	sf := addServerFlags(fs)
	parseIntermixed(fs, args)

	st := registry.DefaultState()
	if err := st.Ensure(); err != nil {
		die(err)
	}
	extra, err := argMounts(fs.Args())
	if err != nil {
		die(err)
	}
	h, err := newHub(&st, extra, *prefix, sf)
	if err != nil {
		die(err)
	}
	spec := *addr
	if *listenSpec != "" {
		spec = *listenSpec
	}
	sock := filepath.Join(st.RunDir(), "hub.sock")
	die(serveHub(h, spec, sock, splitComma(*allowHost), st.Dir))
}

func newHub(st *registry.State, extra []hub.Mount, prefix string, sf *serverFlags) (*hub.Hub, error) {
	cfg, err := sf.apply(true)
	if err != nil {
		return nil, err
	}
	return &hub.Hub{
		State:     st,
		Extra:     extra,
		URLPrefix: normalizePrefix(prefix),
		Walk:      sf.walk(),
		Configure: cfg,
	}, nil
}

// serveHub binds the main listener (Host-checked when TCP) and, when sock
// is non-empty, the hub's unix socket too, then blocks. A live hub.sock
// means another hub is running: that is an error, not a takeover.
func serveHub(h *hub.Hub, spec, sock string, allow []string, stateDir string) error {
	ln, err := listen.Listen(spec)
	if err != nil {
		return err
	}
	var sockLn net.Listener
	if sock != "" {
		if sockLn, err = listen.Listen("unix:" + sock); err != nil {
			ln.Close()
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "glow-web hub %s", version.String())
	if stateDir != "" {
		fmt.Fprintf(os.Stderr, " — state %s", stateDir)
	}
	fmt.Fprintln(os.Stderr)
	handler := h.Handler()
	if ln.Addr().Network() == "tcp" {
		fmt.Fprintf(os.Stderr, "    http://%s%s/\n", ln.Addr(), h.URLPrefix)
		handler = hub.HostCheck(handler, allow)
	} else {
		fmt.Fprintf(os.Stderr, "    unix:%s\n", ln.Addr())
	}
	errc := make(chan error, 2)
	if sockLn != nil {
		fmt.Fprintf(os.Stderr, "    unix:%s  (curl --unix-socket %s http://localhost%s/)\n", sock, sock, h.URLPrefix)
		go func() { errc <- http.Serve(sockLn, h.Handler()) }()
	}
	go func() { errc <- http.Serve(ln, handler) }()
	return <-errc
}

// argMounts names positional roots with NameFor, refusing duplicates.
func argMounts(paths []string) ([]hub.Mount, error) {
	var out []hub.Mount
	seen := map[string]string{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, err
		}
		n := registry.NameFor(abs)
		if prev, dup := seen[n]; dup {
			return nil, fmt.Errorf("%s and %s both mount as %q; publish one under another name (glow-web publish PATH --name NAME)", prev, abs, n)
		}
		seen[n] = abs
		out = append(out, hub.Mount{Name: n, Root: abs})
	}
	return out, nil
}

func runPublish(args []string) {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	name := fs.String("name", "", "mount name (default: derived from PATH; see `glow-web name`)")
	parseIntermixed(fs, args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: glow-web publish PATH [--name NAME]")
		os.Exit(2)
	}
	st := registry.DefaultState()
	n, err := st.Publish(fs.Arg(0), *name)
	if err != nil {
		die(err)
	}
	fmt.Println(n)
	fmt.Fprintf(os.Stderr, "published: http://%s/%s/ (when `glow-web hub` runs on the default address)\n", DefaultHubAddr, n)
}

func runUnpublish(args []string) {
	fs := flag.NewFlagSet("unpublish", flag.ExitOnError)
	parseIntermixed(fs, args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: glow-web unpublish NAME")
		os.Exit(2)
	}
	if err := registry.DefaultState().Unpublish(fs.Arg(0)); err != nil {
		die(err)
	}
}

// runLs prints the registry: one line per mount, tab-aligned. It reads the
// state dir directly; no hub needs to be running.
func runLs(args []string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	parseIntermixed(fs, args)
	st := registry.DefaultState()
	pins, err := st.Pins()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		die(err)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tLIVE\tROOT")
	for _, p := range pins {
		live := "yes"
		if !p.Live {
			live = "missing"
		}
		fmt.Fprintf(tw, "%s\tpin\t%s\t%s\n", p.Name, live, p.Root)
	}
	tw.Flush()
	hubLive := "not running"
	if listen.Alive(filepath.Join(st.RunDir(), "hub.sock")) {
		hubLive = "running"
	}
	fmt.Fprintf(os.Stderr, "state %s · hub %s\n", st.Dir, hubLive)
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
