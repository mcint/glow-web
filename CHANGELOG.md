# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While we're pre-1.0, minors are allowed to break things; patches will not.

## [Unreleased]

### Added

- Index page is now a sortable table with five columns: name, size,
  mtime, git status (porcelain XY), and a ±N numstat for the diff
  vs HEAD. Click any column header to cycle sort asc → desc →
  default; sort state persists per-project in `localStorage`.
  Spec: `specs/index-table-and-git.md`.
- Class-cycle toggle on the index — `[md]` → `[+text]` → `[all]` —
  to widen the listing from markdown to common text/config files
  to everything. Non-md rows render as listing-only (no link); the
  serving allowlist remains markdown-only. Persisted in
  `localStorage` under `index-class`.
- Hidden-cycle toggle on the index — `[normal]` → `[+hidden]` →
  `[+ignored]` — to expose dotfiles and gitignored files.
  Orthogonal to the class toggle; persisted under `index-hidden`.
- Per-row git status when the served directory is a git repo:
  shells once to `git status --porcelain=v1 -z` and once to
  `git diff HEAD --numstat -z`. Missing `.git` or missing `git`
  binary silently leaves the columns blank.
- New `internal/gitstatus` package wraps the git invocations with
  a 2s timeout and returns a `map[string]Entry` keyed by repo-
  relative path.
- `walk.Options` gains `IncludeText`, `IncludeAll`, `IncludeHidden`,
  and `IncludeIgnored`. `walk.File` gains `Class` (`md` / `text` /
  `other`), `Hidden`, and `Ignored` fields. `.git/` is always
  pruned regardless of options.
- `/_/files` JSON entries now carry `mtime` (Unix seconds). Index
  page `<li>` elements get `data-mtime` for the same reason.
- Filter rendering shows a relative-time hint (`5m` / `3h` / `2d` /
  `4w` / …) next to each matched result in both the inline index
  filter and the ⌘K sidebar.
- Sub-path coloring: every filter result splits the rel into a dim
  directory portion and a normal-weight filename. Match highlights
  span both halves correctly so multi-segment hits stay legible.
- Multi-token fuzzy match: whitespace in the query splits it into
  ordered tokens that each subsequence-match in turn, advancing
  past the previous token. Lets you type `cc 5w se` to find
  `cc/5wh/seed.md` without typing slashes. Word-boundary bonus on
  each token's first char rewards clean segment-aligned matches.

## [0.4.0] — 2026-05-07

The "polish + identity" cut: titles are recognisable, multiple
instances are distinguishable, the dev loop is one Makefile target.

### Added

- `--title-prefix` flag (default `"auto"`) and template that renders
  `<title>` as `<doc> · <prefix>`. The `auto` sentinel expands to
  `glow-web:<port>` so two instances on the same browser origin get
  distinguishable tabs.
- `--title-prefix-port=auto|never|always` sub-flag to tune port
  inclusion. `never` is the reverse-proxy answer — the bound port is
  internal, public-facing port is whatever the proxy fronts.
- `Makefile` mirroring the commit-gate idiom: `make` runs
  `fmt-check + vet + test`. `make screenshots` regenerates README
  PNGs via playwright + system chromium.
- `specs/session-2026-05-07.md` — single-day build journal.

### Changed

- `localStorage` keys are namespaced with `glow-<8hex>-` (FNV-1a of
  absolute root) so two glow-web instances on the same browser origin
  can't trample each other's UI state. Theme, sidebar open-state, and
  scroll positions all live under the per-project prefix.
- Spec reorganised into `specs/spec.md` + `specs/feature-toggles.md`;
  former root `SPEC.md` and `docs/notes/` consolidated.

## [0.3.0] — 2026-05-07

The "reading affordances" cut: the page now adapts to your
preferences and keeps your place.

### Added

- Dark mode with a `◐` UI toggle that cycles auto → light → dark →
  auto and persists in `localStorage`. `--theme` flag seeds the
  default; the browser toggle wins for that user once they click.
- Persistent left sidebar (replaces the modal palette overlay) — opens
  on ⌘K / Ctrl-K, sticks across page navigations via
  `glow-palette-open` in `localStorage`. Esc closes when the input is
  focused; click-outside-to-close was intentionally removed so users
  can read while the sidebar is pinned.
- Per-file scroll-position memory in `localStorage` keyed by
  `pathname`. Restores on load except when the URL has a `#anchor`
  (anchor-scroll wins).

## [0.2.0] — 2026-05-07

The "navigation" cut: the page knows where you are in the project,
and you can jump anywhere fast.

### Added

- Click-navigable breadcrumbs in the bar (`project / sub / file`).
  Clicking a directory crumb lands on the index filtered to that
  prefix.
- `?prefix=sub/` sub-dir filter on the index. Path-traversal attempts
  in the prefix query are normalised away.
- Intra-project link rewriting: `[foo](../sub/foo.md)` becomes a
  served URL via an AST pass over `*ast.Link`. Walk-result allowlist
  gates rewriting, so gitignored files cannot be turned into clickable
  URLs even via a relative reference.
- Home-relative footer paths (`~/dev-llm/glow-web` instead of the
  full filesystem path).
- ⌘K / Ctrl-K command palette — fzf-style fuzzy scoring (contiguous +
  word-boundary bonuses, earlier-is-better penalty) over a new
  `/_/files` JSON endpoint. Always-visible inline filter input on the
  index page uses the same scoring.
- `--palette` flag (default on).
- `docs/notes/feature-toggles.md` (since moved to `specs/`) — design
  memo on how config will scale beyond per-flag bools.
- Listener URLs in the boot notice are clickable. Wildcard binds
  expand to localhost + LAN IPv4s, RFC 5737 placeholders in tests.
- README expansion with charmbracelet/glow link + five Chromium-
  headless screenshots.

### Changed

- `--addr` default flipped from wildcard `:8080` (all interfaces) to
  loopback `127.0.0.1:8080`. Public-by-default is the wrong least-
  surprise choice for "I want to read a doc locally"; the escape
  hatch is one explicit flag.

## [0.1.0] — 2026-05-07

First usable cut. The CLI surface and the dir-mode web server reach
a complete shape.

### Added

- `cmd/glow-web` dispatcher with `web`, `slice`, `render`, `tui`,
  `version` subcommands. stdlib `flag` with an intermixed-args shim
  so `cmd PATH --flag` works either way.
- `web` subcommand: single-file mode and dir mode, autodetected from
  path stat. `--addr`, `--url-prefix`, `--gitignore`, `--ignore-files`,
  `--readonly`, `--markup` flags.
- `slice` subcommand: heading-path semantic slicing with sibling-
  closes-section semantics.
- `render` subcommand: HTML or markup output to stdout.
- `internal/mdcore` — single goldmark config (GFM + footnote +
  typographer + auto heading IDs) shared by every output adapter.
- `internal/render` — HTML adapter and hybrid-markup adapter
  (`?view=markup` toggle exposes the latter on the web).
- `internal/walk` — gitignore-aware markdown discovery using
  sabhiram/go-gitignore. Security boundary for dir-mode serving.
- `internal/web` — `Server` struct with file/dir mode autodetect, per-
  page templates split into partials (style/footer/view/index/edit),
  edit page with split-pane live preview + save, action buttons
  (Raw/Download/Edit), URL prefix mount.
- `internal/version` — runtime build identification with VCS
  revision. Surfaces in the page footer + `glow-web version`.
- `SPEC.md` (root, since moved to `specs/spec.md`) and `README.md`.

[Unreleased]: https://github.com/mcint/glow-web/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/mcint/glow-web/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/mcint/glow-web/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/mcint/glow-web/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/mcint/glow-web/releases/tag/v0.1.0
