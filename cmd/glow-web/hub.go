package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mcint/glow-web/internal/hub"
	"github.com/mcint/glow-web/internal/listen"
	"github.com/mcint/glow-web/internal/registry"
	"github.com/mcint/glow-web/internal/version"
	"github.com/mcint/glow-web/internal/web"
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
	// The dedicated hub takes the same election lock as sessions do (rung
	// 7), so it and an elected session can never both serve.
	el := &hub.Elector{State: st, Addr: spec, AllowHost: splitComma(*allowHost), NewHub: func() *hub.Hub { return h }}
	won, err := el.TryOnce()
	if err != nil {
		die(err)
	}
	if !won {
		die(fmt.Errorf("a hub is already running for %s (pid %d); `glow-web ls` shows it", st.Dir, st.HubLockHolder()))
	}
	fmt.Fprintf(os.Stderr, "glow-web hub %s — state %s\n", version.String(), st.Dir)
	if h.BaseURL != "" {
		fmt.Fprintf(os.Stderr, "    %s%s/\n", h.BaseURL, h.URLPrefix)
	}
	sock := filepath.Join(st.RunDir(), "hub.sock")
	fmt.Fprintf(os.Stderr, "    unix:%s  (curl --unix-socket %s http://localhost%s/)\n", sock, sock, h.URLPrefix)
	die(el.Wait())
}

// sessionElector lets a hub-aware session become the hub when none is
// running, and take over when the hub exits (rung 7). It returns nil when
// election is off.
func sessionElector(st registry.State, addr string, sf *serverFlags, on bool) *hub.Elector {
	if !on {
		return nil
	}
	cfg, err := sf.apply(true)
	if err != nil {
		die(err)
	}
	return &hub.Elector{
		State:  st,
		Addr:   addr,
		NewHub: func() *hub.Hub { return &hub.Hub{Walk: sf.walk(), Configure: cfg} },
		Log:    func(s string) { fmt.Fprintln(os.Stderr, s) },
	}
}

// joinHub runs the election for the session's lifetime: one synchronous try
// (so the URL printed next is right), then a retry every 2s. The returned
// func stops it and releases the hub.
func joinHub(el *hub.Elector) func() {
	if el == nil {
		return func() {}
	}
	_, _ = el.TryOnce()
	stop := make(chan struct{})
	go el.Run(2*time.Second, stop)
	return func() { close(stop); el.Close() }
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

// serveEphemeral serves an ephemeral hub (`web a/ b/`): no registry, no
// hub.sock, no election. Just these roots on one listener.
func serveEphemeral(h *hub.Hub, spec string) error {
	ln, err := listen.Listen(spec)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "glow-web %s — %d roots on one listener\n", version.String(), len(h.Extra))
	handler := h.Handler()
	if ln.Addr().Network() == "tcp" {
		fmt.Fprintf(os.Stderr, "    http://%s%s/\n", ln.Addr(), h.URLPrefix)
		handler = hub.HostCheck(handler, nil)
	} else {
		fmt.Fprintf(os.Stderr, "    unix:%s\n", ln.Addr())
	}
	return http.Serve(ln, handler)
}

// runLease registers root with the hub as a lease (rung 4) and holds it
// until SIGINT/SIGTERM: the hub serves root at /<name>/ exactly while this
// process lives. Nothing is bound here.
func runLease(root, name string, el *hub.Elector) {
	abs, err := filepath.Abs(root)
	if err != nil {
		die(err)
	}
	if _, err := os.Stat(abs); err != nil {
		die(err)
	}
	if name == "" {
		name = registry.NameFor(abs)
	}
	st := registry.DefaultState()
	lease, err := st.Register(registry.Record{Name: name, Root: abs, Version: version.String()})
	if err != nil {
		die(err)
	}
	fmt.Fprintf(os.Stderr, "glow-web %s — leased %s as %q (served by the hub while this runs; Ctrl-C to release)\n", version.String(), abs, name)
	leave := joinHub(el)
	printHubURL(st, name)
	waitForSignal()
	lease.Close()
	leave()
}

// runProxy serves root itself on run/<name>.sock and registers it, so the
// hub forwards /<name>/ here verbatim (rung 5). The session keeps its own
// binary and flags, e.g. glow-web-dev beside a stable hub.
func runProxy(root, name string, sf *serverFlags, el *hub.Elector) {
	abs, err := filepath.Abs(root)
	if err != nil {
		die(err)
	}
	if name == "" {
		name = registry.NameFor(abs)
	}
	if err := registry.ValidName(name); err != nil {
		die(err)
	}
	st := registry.DefaultState()
	if err := st.Ensure(); err != nil {
		die(err)
	}
	s, err := web.NewServer(abs, sf.walk())
	if err != nil {
		die(err)
	}
	s.TitlePrefix = name
	cfg, err := sf.apply(true)
	if err != nil {
		die(err)
	}
	cfg(s)
	// The instance must know its full public prefix: the hub's (if it
	// runs under one) + /<name>. Read from the hub, never from a header.
	hubPrefix := ""
	if hs, ok := hubStatus(st); ok {
		hubPrefix = hs.Prefix
	}
	s.URLPrefix = hubPrefix + "/" + name
	s.HubURL = hubPrefix + "/"

	sockName := st.SockName(name)
	ln, err := listen.Listen("unix:" + filepath.Join(st.RunDir(), sockName))
	if err != nil {
		die(err)
	}
	lease, err := st.Register(registry.Record{Name: name, Root: abs, Version: version.String(), Sock: sockName})
	if err != nil {
		ln.Close()
		die(err)
	}
	go func() { die(s.ServeListener(ln)) }()
	leave := joinHub(el)
	printHubURL(st, name)
	waitForSignal()
	lease.Close()
	ln.Close()
	leave()
}

// autoMode picks the --hub=auto behaviour. Lease only when the hub runs
// this exact build: it renders a lease with its own code. Otherwise proxy,
// which works with any hub on the same contract major (§4.V).
func autoMode(hubUp bool, hubVersion, mine string) string {
	if hubUp && hubVersion == mine {
		return "lease"
	}
	return "proxy"
}

func printHubURL(st registry.State, name string) {
	if hs, ok := hubStatus(st); ok && hs.URL != "" {
		fmt.Fprintf(os.Stderr, "    %s/%s/\n", hs.URL, name)
		return
	}
	fmt.Fprintf(os.Stderr, "    no hub reachable yet (another hub-aware session or `glow-web hub` will serve it; this one keeps trying)\n")
}

func waitForSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}

// hubStatus asks a running hub, over run/hub.sock, for its /_/hub status.
func hubStatus(st registry.State) (hub.Status, bool) {
	sock := filepath.Join(st.RunDir(), "hub.sock")
	c := &http.Client{Timeout: time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := c.Get("http://hub/_/hub")
	if err != nil {
		return hub.Status{}, false
	}
	defer resp.Body.Close()
	var s hub.Status
	if json.NewDecoder(resp.Body).Decode(&s) != nil || s.Hub != "glow-web" {
		return hub.Status{}, false
	}
	return s, true
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
	for _, r := range st.Records() {
		kind := r.Mode()
		if !r.Usable() {
			kind = fmt.Sprintf("contract-%d?", r.Contract)
		}
		fmt.Fprintf(tw, "%s\t%s\tpid %d\t%s\n", r.Name, kind, r.PID, r.Root)
	}
	tw.Flush()
	hubLive := "not running"
	if listen.Alive(filepath.Join(st.RunDir(), "hub.sock")) {
		hubLive = fmt.Sprintf("running (pid %d)", st.HubLockHolder())
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
