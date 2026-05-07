// Command glow-web is the entry point for the web/tui/slice/render
// subcommands. Each subcommand owns its own flag.FlagSet; the top-level
// dispatcher stays a switch.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mcint/glow-web/internal/render"
	"github.com/mcint/glow-web/internal/slice"
	"github.com/mcint/glow-web/internal/web"
)

const usage = `glow-web — markdown reader for web, TUI, and CLI slicing

usage: glow-web <command> [options] [path]

commands:
  web      serve a markdown file as HTML over HTTP
  slice    extract a section by heading path
  render   render a markdown file to stdout (html only in v0)
  tui      open the markdown file in a terminal UI (stub in v0)

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
	case "slice":
		runSlice(args)
	case "render":
		runRender(args)
	case "tui":
		fmt.Fprintln(os.Stderr, "glow-web: tui subcommand is a v0 stub; use 'glow-web web' for now")
		os.Exit(1)
	case "-h", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "glow-web: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	parseIntermixed(fs, args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "glow-web web: missing PATH")
		os.Exit(2)
	}
	if err := web.ServeFile(*addr, fs.Arg(0)); err != nil {
		die(err)
	}
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
	format := fs.String("format", "html", "output format (html)")
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
	default:
		fmt.Fprintf(os.Stderr, "glow-web render: unsupported format %q (try 'html')\n", *format)
		os.Exit(2)
	}
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

func die(err error) {
	fmt.Fprintf(os.Stderr, "glow-web: %v\n", err)
	os.Exit(1)
}
