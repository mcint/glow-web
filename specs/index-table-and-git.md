# Index as a table — git status + class & hidden toggles

The `/` index in directory mode is the project's at-a-glance entry
point. Today it shows discovered markdown files as a flat `<ul>` with
name and raw byte size. The view should grow into a richer, sortable
table that surfaces git activity and project shape — without
relaxing the security boundary that gates *serving* content.

## What ships

A `<table class="files">` with five columns:

| Column     | Source                            | Notes                                       |
| ---------- | --------------------------------- | ------------------------------------------- |
| name       | `walk.File.Rel`                   | linked for `Class == "md"`; plain text otherwise |
| size       | `walk.File.Size`                  | byte count, right-aligned                   |
| mtime      | `walk.File.ModTime`               | relative form (`5m` / `3h` / `2d` / …)      |
| git XY     | `git status --porcelain=v1`        | two chars: index letter + worktree letter   |
| git ±lines | `git diff HEAD --numstat`          | `+N` green, `-M` red; muted-zero when clean |

### Column headers are sortable

Click a `<th>` to cycle that column through asc → desc → default
(by `Rel`). One column is active at a time. State persists in
`localStorage` under `K('index-sort')` as `"col:dir"` (e.g.
`"mtime:desc"`); `""` is "default".

### Two segmented toggles in the header bar

Both toggles sit in `.browser-controls` (the dir-view-only row inside the
permanent browser wrapper — see `header-layout.md`) and persist independently
in `localStorage`. Each is a single `<button class="seg-toggle">` — one
click still cycles the aperture wider (and wraps) — rendering all three
strata as `.seg` segments, each carrying a marginal file count:

| Toggle  | Segments (aperture widens left → right)            | Key                |
| ------- | -------------------------------------------------- | ------------------ |
| class   | `md N` · `+text N` · `+all N`                      | `K('index-class')` |
| hidden  | `normal N` · `+hidden N` · `+ignored N`            | `K('index-hidden')`|

The states are **cumulative apertures** (`md ⊆ text ⊆ all`;
`normal ⊆ +hidden ⊆ +ignored`), so each strata's count is the *marginal*
contribution — how many additional rows widening to that stratum admits.

**Selection is a single gestalt, not a contrast gradient.** At rest, *only the
selected* stratum shows its name and carries a filled background
(`.seg.is-current`, `background: var(--accent)`); the others show only their
count. The JS marks exactly the current `.seg`; CSS hides the other
`.seg-label`s. Hovering the chip reveals every name so you can see what a click
would widen to. This replaced an earlier black-active / grey-inactive text
scheme — in a 3-item control a contrast gradient reads poorly; a filled element
reads at a glance. See `cc/design/gestalt-over-subtle-encoding.md`.

The two dimensions are orthogonal in their row-hiding effect — CSS still
hides rows whose `data-class` / `data-hidden` / `data-ignored` don't
match the active states. But the **counts are conditioned on the sibling
toggle**: the class strata are tallied only among files that pass the
current hidden filter, and vice-versa, so a segment's number matches what
widening to it would actually reveal. Changing one toggle repaints the
other's counts. Counts are computed client-side in `paintStrata()`
(`scripts.html.tmpl`) during `rerender`, walking the in-DOM rows once.

**Decision: counts ignore the text-filter query.** The chips describe the
directory's *shape* (a stable legend); the `N files` counter already
reflects the live query. Prioritizes legibility + stability over
per-keystroke accuracy during active filtering — the marginal accuracy
lost there is recovered by the counter. (Reversal condition: if users
read the chip counts as "matches", fold the query in.)

### Git status columns

`git status --porcelain=v1 -z` runs once per index render and is
mapped by repo-relative path. The XY column renders the two-char
porcelain code literally (`MM`, ` M`, `M `, `??`, `A `, etc) so users
who know `git status` recognise it immediately.

`git diff HEAD --numstat -z` runs once for the worktree-vs-HEAD diff
and supplies `+N -M` per changed file. When a file is clean the cell
is empty (no `+0 -0` jitter).

In a non-git directory or when the `git` binary is missing, both
commands silently no-op and the columns render blank. Errors are
*never* surfaced to the user — the index must keep working in a
freshly-`mkdir`'d folder.

### Persistence summary

Three new `localStorage` keys join the existing per-project
`glow-<hash>-` namespace:

| Key suffix       | Purpose                                          |
| ---------------- | ------------------------------------------------ |
| `index-sort`     | active sort column + direction; `""` = default   |
| `index-class`    | `md` \| `text` \| `all`                          |
| `index-hidden`   | `normal` \| `hidden` \| `ignored`                |

## Security boundary — explicitly unchanged

`walk.Files()` is the single allowlist for *serving* content. This
change makes walk *list* more (text files, dotfiles, gitignored
files) but the serve path (`findFile`, the link-rewrite gate, and
the raw/edit/save routes) checks `Class == "md"` before serving
anything. A non-md row appears in the listing without an `<a>`; a
direct request to its URL 404s.

Raw-text serving for non-md files is a follow-up spec, not part of
this batch. That spec needs to define MIME sniffing, size caps, and
how it interacts with the gitignore allowlist.

## File classification

`walk.File.Class` is one of `"md" | "text" | "other"`. Classification
in v0:

- `"md"` — extension in `Extensions` option (defaults `.md` /
  `.markdown`).
- `"text"` — extension in a built-in text/config set: `.txt`,
  `.toml`, `.yaml`, `.yml`, `.json`, `.go`, `.rs`, `.py`, `.sh`,
  `.rb`, `.js`, `.ts`, `.html`, `.css`, `.xml`, `.sql`, `.conf`,
  `.ini`, plus name matches `Makefile`, `Dockerfile`, `LICENSE`,
  `README` (extensionless).
- `"other"` — everything else (binaries, archives, unknown
  extensions).

Content sniffing for extensionless files is a follow-up — it costs
an extra `os.Open` per file and the extension table covers the 90%
case.

## Walk option additions

```go
type Options struct {
    // … existing fields …
    IncludeText    bool // include "text" class files in the result
    IncludeAll     bool // include "other" class files too (implies IncludeText)
    IncludeHidden  bool // include dotfiles / dot-directories
    IncludeIgnored bool // bypass gitignore (still excludes .git/ itself)
}
```

`File.Ignored bool` is set when the file was kept solely because
`IncludeIgnored` allowed it. The UI uses it to dim/mark those rows.

The web server's `serveIndex` walks with all four flags on; the
front-end filters down via CSS. This keeps the JSON for the command
palette consistent and avoids re-walking per toggle change.

## Why shell to git rather than `go-git`

- Zero dependency. `go-git` is ~MB of code we don't need.
- `git status` semantics shift over time; the binary is the spec.
- Performance is fine: one `status` + one `diff --numstat` per index
  render dominates by network latency, not git.
- The wrapper is small enough (~60 lines) that swapping to `go-git`
  later is a contained change behind `gitstatus.Status()`.

Trade-off: requires `git` on `PATH`. Acceptable — the target user is
a developer reading docs in a repo.

## Out of scope (follow-ups)

- **Raw-text serving for non-md files.** Needs its own spec covering
  MIME, size cap, gitignore re-examination, and `<a>` vs download.
- **Binary visualizers.** Image preview, hex dump, perhaps a
  Rust/WASM helper for richer types.
- **Click-to-stage / write-side git.** The index is read-only on the
  git surface; staging or committing belongs in a separate flow.
- **Per-file history / blame.** Deeper git integration, separate UI.
- **Content sniffing for extensionless files.** Light helper; not
  worth the IO until the extension table proves insufficient.

## Verification

After implementation:

1. `go test ./...` green.
2. `go run ./cmd/glow-web web .` against this repo:
   - Index renders as a table; default sort by name.
   - Each `<th>` click reorders rows; reload preserves sort state.
   - Class chip widens `md` → `+text` → `+all`, revealing more rows;
     non-md rows have no link. Each segment shows its marginal count;
     the included strata are full-contrast, the rest desaturated.
   - Hidden chip widens `normal` → `+hidden` → `+ignored`, exposing
     dotfiles then gitignored files. Widening one chip repaints the
     other's marginal counts.
   - Edit `internal/web/web.go` locally → row shows ` M` + `+N -M`.
     `git add` → row shows `M ` + numstat reflects index-vs-HEAD.
3. `go run ./cmd/glow-web web testdata/` (non-git):
   - Git columns render empty, no errors.
4. Hit `/<some-non-md-path>` directly → 404 (allowlist intact).
