package main

import (
	"flag"
	"strings"

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
		readonly:        fs.Bool("readonly", false, "disable the edit page and save endpoint"),
		markup:          fs.Bool("markup", false, "default to hybrid markup view (toggle with ?view=rendered)"),
		palette:         fs.Bool("palette", true, "enable ⌘K / Ctrl-K command palette (use --palette=false to disable)"),
		theme:           fs.String("theme", "auto", "default theme: auto | light | dark (browser toggle overrides)"),
		titlePrefix:     fs.String("title-prefix", "auto", `<title> rendered as "<doc> · <prefix>". "auto" expands to "glow-web[:port]"; "" disables; any other value passes through verbatim. Under a hub the default is the mount name.`),
		titlePrefixPort: fs.String("title-prefix-port", "auto", `port handling when --title-prefix=auto: "auto" (include if known), "never" (omit, useful behind a reverse proxy), "always".`),
		logLevel:        fs.String("log-level", "dot", `per-request access log: "dot" (default) marks "|" per primary page view and "." per internal request, e.g. "|.....|..|....."; "off" silent; "info" one line per request (client method path status bytes duration).`),
	}
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
	explicitTitle := f.set("title-prefix")
	return func(s *web.Server) {
		s.ReadOnly = *f.readonly
		s.DefaultMarkup = *f.markup
		s.CommandPalette = *f.palette
		s.Theme = *f.theme
		if !underHub || explicitTitle {
			s.TitlePrefix = *f.titlePrefix
		}
		s.TitlePort = *f.titlePrefixPort
		s.LogLevel = lvl
	}, nil
}
