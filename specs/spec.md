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
    --addr 127.0.0.1:8080                        loopback by default; ":8080" or
                                                 "0.0.0.0:8080" exposes on LAN
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

### Browser-local UI state

Three pieces of UI state persist in `localStorage` (no cookies, never sent
to the server):

| Key                       | Purpose                                              |
| ------------------------- | ---------------------------------------------------- |
| `glow-theme`              | `light` / `dark` / `auto` — overrides `--theme` flag |
| `glow-palette-open`       | `1` / `0` — sidebar open state restored on each page |
| `glow-scroll:<pathname>`  | scroll position as fraction `0..1` per file          |

The sidebar is left-anchored, slides in over the page, and shifts main
content right via a body class. It opens on ⌘K / Ctrl-K, closes on Esc
or another ⌘K, and stays pinned across page navigations so jump-back-
and-forth between files doesn't require re-summoning it. Scroll memory
restores on load except when the URL carries a `#anchor` (anchor scroll
wins).

## Intra-project link rewriting

When a markdown link's destination resolves to a file in the same project, the
HTML view rewrites the `href` to the served URL of that file — so clicking a
relative `[foo](../sub/foo.md)` navigates inside the running server instead of
404'ing in the browser.

**What we rewrite (v0):**

- Pure relative paths (`foo.md`, `../sub/foo.md`) joined to the current
  document's directory, then path-cleaned.
- Root-relative paths (`/sub/foo.md`) anchored at the project root.
- Query and fragment suffixes (`b.md#section`, `b.md?raw=1`) survive intact.
- The rewrite carries `--url-prefix` automatically.

**What we leave alone:**

- Anything with a URL scheme (`http:`, `https:`, `mailto:`, …) or
  protocol-relative (`//host/…`) — these are external by definition.
- Anchor-only refs (`#section`).
- Targets that don't appear in the walk-result allowlist (so gitignored or
  non-markdown files stay un-linked, matching the security boundary the
  rest of the server already enforces).
- Targets that escape the project root (`../../etc/passwd`) — left as the
  original literal string, which will simply 404 if clicked.

**Security model:**

- The walk-result allowlist gates link rewriting the same way it gates file
  serving. A gitignored file cannot be turned into a clickable served URL.
- Path resolution uses `path.Clean` and rejects any cleaned form that starts
  with `..`, so relative paths cannot pivot outside the served tree.
- URL-encoded paths are decoded once before resolution to avoid
  double-decoding bypasses.
- We never *fetch* the target — the user must click. Browsers' same-origin
  policy still applies; rewriting changes only the `href` attribute.

**Future axes (not v0):**

- Image rewriting (`*ast.Image` parallel of the link transform) for
  intra-project image references. Same allowlist, plus consideration for
  raw bytes vs. data-uri inlining.
- Heading-anchor verification (rewrite `b.md#bad-anchor` to `b.md` if the
  anchor isn't present in `b.md`'s rendered output).
- Reference-style links and link definitions (`[a]: foo.md`).
- Soft-resolution: if `[foo](foo)` doesn't match `foo` but matches `foo.md`,
  rewrite. Adds least-surprise but can introduce ambiguity; gate behind a
  flag.
- Inverse map: a link-graph view (`?backlinks=1` shows which docs link
  *to* the current doc).

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
