package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/mcint/glow-web/internal/policy"
	"github.com/mcint/glow-web/internal/walk"
	"github.com/mcint/glow-web/internal/web"
)

// serverFlags are the per-root rendering flags that `web` and `hub` share.
// Keeping them in one place keeps the two subcommands from drifting.
type serverFlags struct {
	fs              *flag.FlagSet
	gitignore       *bool
	ignoreFiles     *string
	readonly        *bool
	writable        *string // --writable and its alias --writeable share it
	markup          *bool
	palette         *bool
	theme           *string
	titlePrefix     *string
	titlePrefixPort *string
	logLevel        *string
}

func addServerFlags(fs *flag.FlagSet) *serverFlags {
	return &serverFlags{
		fs:              fs,
		gitignore:       fs.Bool("gitignore", true, "honor .gitignore (dir mode); use --gitignore=false to disable"),
		ignoreFiles:     fs.String("ignore-files", "", "comma-separated additional ignore-files (relative to PATH or absolute)"),
		readonly:        fs.Bool("readonly", false, "same as --writable=no (the default); kept as an explicit spelling"),
		writable:        writableFlag(fs),
		markup:          fs.Bool("markup", false, "default to hybrid markup view (toggle with ?view=rendered)"),
		palette:         fs.Bool("palette", true, "enable ⌘K / Ctrl-K command palette (use --palette=false to disable)"),
		theme:           fs.String("theme", "auto", "default theme: auto | light | dark (browser toggle overrides)"),
		titlePrefix:     fs.String("title-prefix", "auto", `<title> rendered as "<doc> · <prefix>". "auto" expands to "glow-web[:port]"; "" disables; any other value passes through verbatim. Under a hub the default is the mount name.`),
		titlePrefixPort: fs.String("title-prefix-port", "auto", `port handling when --title-prefix=auto: "auto" (include if known), "never" (omit, useful behind a reverse proxy), "always".`),
		logLevel:        fs.String("log-level", "dot", `per-request access log: "dot" (default) marks "|" per primary page view and "." per internal request, e.g. "|.....|..|....."; "off" silent; "info" one line per request (client method path status bytes duration).`),
	}
}

func writableFlag(fs *flag.FlagSet) *string {
	v := new(string)
	*v = "no"
	usage := "what browsers may save: " + policy.Values + ` (default "no": read-only). "md" = markdown files; "yes" = every class the editor supports`
	fs.StringVar(v, "writable", "no", usage)
	fs.StringVar(v, "writeable", "no", "alias for --writable")
	return v
}

// writePolicy resolves --writable / --writeable / --readonly
// (specs/write-policy.md §2). Conflicts are errors, never a silent pick.
func (f *serverFlags) writePolicy() (policy.Write, error) {
	if f.set("writable") && f.set("writeable") {
		return policy.No, fmt.Errorf("--writable and --writeable are the same flag; give it once, not both")
	}
	w, err := policy.Parse(*f.writable)
	if err != nil {
		return policy.No, fmt.Errorf("--writable: %w", err)
	}
	if *f.readonly && w != policy.No {
		return policy.No, fmt.Errorf("--readonly conflicts with --writable=%s", w)
	}
	return w, nil
}

func (f *serverFlags) walk() walk.Options {
	o := walk.Options{Gitignore: *f.gitignore}
	for _, p := range strings.Split(*f.ignoreFiles, ",") {
		if p = strings.TrimSpace(p); p != "" {
			o.IgnoreFiles = append(o.IgnoreFiles, p)
		}
	}
	return o
}

func (f *serverFlags) set(name string) bool {
	found := false
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == name {
			found = true
		}
	})
	return found
}

// apply returns a function that configures a server from the flags. With
// underHub, --title-prefix is applied only when given explicitly, so the
// hub's per-mount default (the mount name) stands.
func (f *serverFlags) apply(underHub bool) (func(*web.Server), error) {
	lvl, err := web.ParseLogLevel(*f.logLevel)
	if err != nil {
		return nil, err
	}
	wp, err := f.writePolicy()
	if err != nil {
		return nil, err
	}
	explicitTitle := f.set("title-prefix")
	return func(s *web.Server) {
		s.DefaultMarkup = *f.markup
		s.CommandPalette = *f.palette
		s.Theme = *f.theme
		if !underHub || explicitTitle {
			s.TitlePrefix = *f.titlePrefix
		}
		s.TitlePort = *f.titlePrefixPort
		s.LogLevel = lvl
		// Under a hub this is overridden per mount by the mount's owner
		// (hub.server); it matters for standalone and proxy servers.
		s.Writable = wp
	}, nil
}
