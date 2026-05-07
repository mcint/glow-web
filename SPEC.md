# glow-web — spec

## What

A markdown reader that renders the same content three ways:

- **`web`** — themed HTML over HTTP, navigable in a browser.
- **`tui`** — terminal UI styled à la [Glow](https://github.com/charmbracelet/glow).
- **`slice`** — semantic operations over markdown structure for CLI/pipeline use
  (extract section by heading path, list code blocks, dump frontmatter, …).

One binary, three subcommands, one shared parsing/AST core.

## Why

Glow is a great TUI reader but lives only in the terminal. The same parsed
representation can drive a web view *and* `jq`-for-markdown semantic slicing —
the latter being especially useful for piping markdown into LLMs (extract
just `## API` from a 2000-line spec) or grep-style workflows over docs trees.

## Non-goals (v0)

- Authentication / multi-user. The web server trusts whoever can reach the
  socket — pair with reverse-proxy auth for remote use.
- Live collaborative editing (a la HedgeDoc). The single-user edit page with
  live preview + save covers the immediate need; CRDT sync is later.
- A custom markdown parser. We stand on [goldmark][gm] and Charmbracelet's
  [glamour][gl] for the styled TUI render.

## Architecture

```
cmd/glow-web/             entry point + subcommand dispatch
internal/mdcore/          goldmark wiring, AST helpers, frontmatter
internal/slice/           semantic ops on the AST (heading-path, code-blocks, …)
internal/render/          output adapters: html, hybrid markup, ansi (later)
internal/walk/            gitignore-aware markdown discovery
internal/web/             net/http server, embedded templates
internal/version/         build identification (debug.ReadBuildInfo + vcs)
internal/tui/             bubble tea program (placeholder in v0)
testdata/                 fixtures shared across packages
```

The AST flows: `bytes → mdcore.Parse → ast.Node → {render,slice} → output`.

## Subcommand surface (v0)

```
glow-web web PATH [flags]                        serve file OR directory
    --addr :8080
    --url-prefix /docs                           mount under prefix (reverse-proxy)
    --gitignore=true                             honor .gitignore (dir mode)
    --ignore-files .rgignore,.glowignore         additional ignore-files
    --readonly                                   disable edit + save
    --markup                                     default to hybrid markup view

glow-web tui PATH                                styled TUI reader (stub in v0)
glow-web slice PATH --heading "Foo / Bar"        extract a section by path
glow-web render PATH --format html|markup        one-shot render
glow-web version                                 print build version
```

### Web routes

```
GET  /                       index of discovered files (dir mode) or the file
GET  /<rel>                  rendered HTML view
GET  /<rel>?view=markup      hybrid view (markup chars muted, content styled)
GET  /<rel>?view=rendered    explicit override of --markup default
GET  /<rel>?raw=1            text/plain
GET  /<rel>?download=1       text/markdown attachment
GET  /<rel>?edit=1           edit page (split-pane, live preview)
POST /<rel>                  save (text/markdown body)
POST /_/render               live-preview helper: source body → HTML
```

When `--url-prefix /P` is set, all routes mount under `/P`. The walk-result
list is the security boundary: requests for files not in the list 404, so
gitignored secrets stay un-served.

## Semantic slicing — addressing model

A heading path is a slash-separated list of heading texts, matched
case-insensitively from the top of the document down. `# Intro / ## Goals`
selects everything under the `## Goals` heading inside the `# Intro` section,
up to the next sibling-or-shallower heading.

Future axes (not v0): nth-occurrence (`## Goals[2]`), regex (`~/^Goals/`),
xpath-style ancestors, by line range, by frontmatter selector.

## Testing

Each semantic operation ships with a fixture under `testdata/` and a snapshot
test. Red-green: write the test first, regenerate the snapshot once intent is
correct.

## Status

v0 spike. Subject to change.

[gm]: https://github.com/yuin/goldmark
[gl]: https://github.com/charmbracelet/glamour
