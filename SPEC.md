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

- Editing. This is a reader / extractor, not an editor.
- Live-reload / collaborative editing. Maybe later.
- A custom markdown parser. We stand on [goldmark][gm] and Charmbracelet's
  [glamour][gl] for the styled TUI render.
- Authentication / multi-user. The web server is single-user, localhost-first.

## Architecture

```
cmd/glow-web/             entry point + subcommand dispatch
internal/mdcore/          goldmark wiring, AST helpers, frontmatter
internal/slice/           semantic ops on the AST (heading-path, code-blocks, …)
internal/render/          output adapters: html, ansi (via glamour), json-ast
internal/web/             net/http server, embedded assets/templates
internal/tui/             bubble tea program (placeholder in v0)
testdata/                 fixtures shared across packages
```

The AST flows: `bytes → mdcore.Parse → ast.Node → {render,slice} → output`.

## Subcommand surface (v0)

```
glow-web web [PATH] [--addr :8080] [--root .]    serve PATH or directory
glow-web tui [PATH]                              styled TUI reader
glow-web slice PATH --heading "Foo / Bar"        extract a section by path
glow-web slice PATH --code-lang go               extract code blocks by lang
glow-web slice PATH --frontmatter                emit just the frontmatter
glow-web render PATH --format html|ansi|json     one-shot render to stdout
```

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
