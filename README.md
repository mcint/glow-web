# glow-web

Markdown reader: web, TUI, and CLI semantic slicing — one binary, one AST.

See [`SPEC.md`](SPEC.md) for vision, scope, and architecture.

## Quick start

```sh
go build -o glow-web ./cmd/glow-web

# Render to HTML on stdout
./glow-web render README.md --format html

# Serve a markdown file or directory at http://localhost:8080
./glow-web web README.md

# Extract a section by heading path
./glow-web slice SPEC.md --heading "Architecture"

# Extract all Go code blocks
./glow-web slice docs/cookbook.md --code-lang go
```

## Status

v0 spike. See [`SPEC.md`](SPEC.md) for the planned surface.
