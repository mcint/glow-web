# Versioning — one source, embedded at build, shown everywhere

*SemVer tags, a patch per commit for now, and a single embedded version string
that every surface (CLI, footer, future ones) reads from `version.String()`.
Built binaries report an exact `git describe`; dev runs report the current
line.*

**Status:** firm. Seeded 2026-05-28 (v0.6.4).

---

## The scheme

- **SemVer, pre-1.0.** Minors may break; patches won't (per CHANGELOG header).
- **A patch per commit, for now.** Every commit that lands gets an annotated
  tag (`git tag -a vX.Y.Z`), with a one-line description. Minors bump at a
  feature threshold (e.g. v0.5.0 "index as a table", v0.6.0 "text, diffs,
  logs"). This is deliberately fine-grained while the project is young — easy
  to coarsen later by simply tagging less often. The tag annotation carries
  the per-commit story; the CHANGELOG carries the per-minor narrative.
- **Tags are additive and, once pushed, immutable.** `v0.1.0`–`v0.4.0` were
  pushed early; later history was back-labelled `v0.4.1`…`v0.6.3` without
  rewriting them. Don't move a pushed tag.

## One embedded source

`internal/version.String()` is the **single source of truth**. The CLI banner,
`glow-web version`, and the page footer (templates' `.Version`) all call it.
**Add a new surface by calling `String()`, never by re-deriving a version.**

Resolution order (most to least authoritative):

1. **Build-injected** `git describe` — set by the Makefile via
   `-ldflags "-X github.com/mcint/glow-web/internal/version.injected=$(git describe --tags --always --dirty)"`.
   `make build` / `make install` use it. Reports the exact tag on a release, or
   `<tag>-<n>-g<sha>` `-dirty` between tags. This is the release path.
2. **Module version** — `go install github.com/mcint/glow-web/...@vX.Y.Z`
   carries the tag in `debug.BuildInfo.Main.Version`.
3. **Dev fallback** — plain `go build` / `go run` with no ldflags. `go build`
   on Go ≥1.24 stamps VCS info into `Main.Version` itself (e.g. `v0.6.3+dirty`);
   `go run` doesn't, so it shows `version.Fallback` (the current dev line, e.g.
   `v0.6.4-dev`). Bump `Fallback` on release so dev runs keep reporting the
   current minor.

### Release checklist

1. `make check` green.
2. Commit, well-scoped, message explains *why*.
3. `git tag -a vX.Y.Z -m "vX.Y.Z — <one-line scope>"`.
4. Bump `version.Fallback` if the minor advanced (its own commit/patch is fine).
5. `make build && ./bin/glow-web version` → confirm the tag shows.
6. Push commits **and** tags (`git push && git push --tags`) when ready.

## Decisions (values)

- **`git describe` injection over a hand-edited constant.** Prioritizes
  *accuracy + low-friction* (the build tells the truth automatically) over the
  *zero-tooling* of a bare const. Excluded marginally: pure `go run` accuracy —
  it can't see `git describe`, so it shows the dev line without commit/dirty.
  Reversal: a `go generate`d version file would close that gap if it matters.
- **Patch-per-commit now.** Prioritizes *granular, bisectable, descriptive
  history* over *changelog tidiness*. Coarsen later by tagging less; the
  CHANGELOG already summarises at the minor level so it stays readable.
