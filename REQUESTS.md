# glow-web REQUESTS

Inbox + resolution log per methods/seed.md §4. Captures asks before
they're spec'd. Rows progress Open → In progress → Resolved; resolved
rows cite the shipping commit SHA so `git show <sha>` is the source of
truth.

When a request grows real teeth, write the spec under `specs/` and
update the row to point at it.

Batch intake 2026-08-22, dictated while reading `~/dev-llm/ATProtoPlay`
docs through glow-web under lmux. Evidence rows below were taken from
that live surface, so "Today" describes observed behaviour rather than
a reading of the source.

---

## R-2026-08-22-01 — link resolution: wikilinks, and targets outside the md class

**Status:** Open · narrower than first stated; see *Today*.

**Ask.** Resolve project-root-relative paths in directory mode.

**Today.** Root-relative *already works*: `resolveRelativeLink`
(`internal/web/web.go:683`) strips a leading `/` and resolves against
the project root, so `[x](/docs/specs/foo.md)` rewrites correctly. Two
real gaps sit behind the ask:

1. **Wikilinks are not links.** `[[marginalia/tid]]` renders as literal
   text — verified on `docs/lessons/tid.md`, where the output contains
   `<p>Marginalia: [[marginalia/tid]]` with no anchor. The README
   advertises a "hybrid Obsidian-like markup view", so the gap is
   surprising in exactly the workflow it invites. Any Obsidian- or
   hypothes.is-shaped corpus is unnavigable without this.
2. **Only `md` targets are rewritten.** `linkResolver` builds its
   allowlist from `f.Class != "md" { continue }`
   (`internal/web/web.go:622`), so a link to `scripts/foo.py` or
   `fixtures/a.json` is left alone even when the file is present, walked
   and safe. This is the same decision as R-2026-08-22-05 and should be
   settled once, not twice.

**Design questions.**

- Wikilink target resolution: exact rel path first, then basename match
  across the walk? Obsidian resolves by basename with a
  shortest-unique-path rule. Ambiguity needs a defined loser.
- `[[target|label]]` and `[[target#heading]]` — support now or reject
  cleanly? A silently-dropped label is worse than an unrendered link.
- Unresolvable wikilinks: literal text (today), or a visibly-dead link
  styled as missing? Obsidian shows them as "unresolved" affordances,
  which is what makes a corpus's holes visible. Related: our marginalia
  stubs deliberately link to pages that may not exist yet.
- Does rewriting non-md targets mean *serving* them, or only linking? See
  R-2026-08-22-05; a link that 404s is worse than no link.

---

## R-2026-08-22-02 — reading typography: distinction, and optical weight in dark mode

**Status:** Open · exploratory; wants a few variants to compare, not one answer.

**Ask.** More distinction while reading, and thinner text that still
reads well in dark mode.

**Today.** `internal/web/templates/style.html.tmpl:19` sets
`font: 16px/1.6 -apple-system, …, sans-serif` — the system UI stack, at
default weight, for body copy. Content column is `max-width: 48rem`
(`:123`). Theme tokens (`:7-14`):

| | fg | bg |
|---|---|---|
| light | `#1a1a1a` | `#fafafa` |
| dark | `#e8e8e8` | `#14171a` |

**Why "thinner in dark mode" is a real effect, not a preference.** Light
text on a dark ground blooms optically (irradiation), so the *same*
weight reads heavier inverted. Near-max contrast — `#e8e8e8` on
`#14171a` is close to it — makes it worse. So the fix is genuinely
per-theme, and a single weight cannot serve both.

**Variants worth trying** (the ask is to compare, so build them
switchable rather than picking blind):

1. **Optical weight per theme.** A variable font at `font-weight: 350`
   in dark and `400` in light. Needs a variable face; the system stack
   has no usable sub-400 rung.
2. **Contrast relief.** Drop dark `--fg` toward `#d3d7db` and let the
   apparent weight fall with it. Cheapest — a token change, no font
   work — and often sufficient on its own.
3. **Smoothing.** `-webkit-font-smoothing: antialiased` under dark only.
   Thins strokes noticeably on macOS, does nothing elsewhere; honest
   about being platform-specific.
4. **A reading face for prose.** Keep the UI stack for chrome, give the
   article a distinct text face. This is most of the "distinction"
   half of the ask; chrome-vs-content is the distinction that matters.
5. **Measure and rhythm.** 48rem is wide for 16px; 34–38rem is the
   usual comfort band. Tune with, not after, weight.

**Design questions.**

- Does this become a user-visible setting (the `◐` control already
  cycles auto/light/dark and persists in `localStorage`), or one
  opinionated default? A "reading" toggle beside `◐` is the obvious
  seam, and matches the existing precedent.
- Self-hosted font file, or stay dependency-free with system stacks?
  Bundling a variable face is the only way to get (1), and costs the
  project its zero-asset property.
- Interaction with the markup view, which is deliberately monospace.

---

## R-2026-08-22-03 — YAML frontmatter: parse it, don't leak it

**Status:** Open · straightforward; has a visible bug attached.

**Ask.** Parse and display frontmatter blocks properly, whether fenced
by `---` or by a code fence.

**Today — actively broken, not merely absent.** `mdcore.New()`
(`internal/mdcore/mdcore.go`) configures `extension.GFM`,
`extension.Footnote` and `extension.Typographer`; there is **no
frontmatter extension**. So goldmark reads the block as body markdown,
and on `docs/lessons/tid.md` produces:

```html
<hr>
<h2 id="a-compact-timestamp-based-identifier-for-revisions-and-records">title: “Timestamp Identifiers (TIDs)”</h2>
```

Three distinct failures in one block:

1. The opening `---` becomes a thematic break.
2. The *closing* `---` makes the preceding line a **setext H2**, so a
   `tldr:` value is promoted to a heading.
3. That bogus heading takes an auto-generated `id`, polluting the
   fragment namespace that R-2026-08-22-04 wants to expose — and it
   lands in any generated table of contents.

Frontmatter is load-bearing across these corpora (`title`, `date`,
`kind`, `tldr`, `source`, `generated`), so this is the difference
between a scannable index and noise.

**Design questions.**

- Display mode: hide entirely, render as a styled metadata card above
  the article, or collapse behind a disclosure? A card promotes `tldr`
  to something you actually read — which is the whole point of the
  convention — and a `generated: true` badge would be genuinely useful.
- `---` (YAML) and `+++` (TOML) are conventional; the ask also mentions
  code-fenced blocks. A fenced ```` ```yaml ```` block at the top of a
  file is *not* frontmatter by any standard, and treating it as such
  would silently swallow legitimate example YAML. Recommend: handle
  `---`/`+++` as frontmatter, and treat a leading fenced block as
  frontmatter only behind an explicit opt-in, if at all.
- Slice/TUI adapters share the AST — does frontmatter become queryable
  in the `jq`-for-markdown CLI? That is arguably the strongest reason
  to parse it into structure rather than just hide it.
- Malformed YAML must degrade to "show it verbatim", never to a parse
  error that blanks the page.

---

## R-2026-08-22-04 — fragment links on headings

**Status:** Open · smallest of the batch; IDs already exist.

**Ask.** Fragment links for headers, host-agnostic — relative or
page-rooted, without knowing the host.

**Today.** `parser.WithAutoHeadingID()` is already enabled in
`mdcore.New()`, and the IDs are real: `<h1 id="timestamp-identifiers-tids">`,
`<h2 id="tid-structure">`, `<h3 id="examples">`, verified live. So the
targets exist and `#tid-structure` already works if you type it. What is
missing is only the **affordance** — a visible, clickable way to obtain
the link.

Host-agnostic is free: a bare `href="#tid-structure"` resolves against
the current document, so nothing needs to know the host or the mount
prefix. This matters under lmux, where the same file is reachable at
both `<project>.<group>.lmux.test/glow-web/…` and
`lmux.test/<group>/<project>/glow-web/…`.

**Design questions.**

- Affordance: the usual `¶`/`#` on hover, or make the whole heading a
  link? Hover-only is invisible on touch.
- Click behaviour: navigate, or copy the URL to the clipboard? Copying
  is what people actually want, but needs the absolute URL, which
  reintroduces the host — resolvable in JS via `location.href`, so still
  no server-side host knowledge.
- Depends on R-2026-08-22-03: until frontmatter stops minting headings,
  the fragment namespace contains junk anchors.
- Duplicate heading text across a document — goldmark suffixes, but the
  suffix order is document-order-dependent and therefore unstable across
  edits. Worth stating that anchors are not permanent identifiers.

---

## R-2026-08-22-05 — widen the set of files that can be read

**Status:** Open · settle jointly with R-2026-08-22-01.

**Ask.** Expand the set of files glow-web will read and serve. Open
question in the ask: does it fingerprint by MIME (with the risk that
entails) or by extension?

**Answered: extension, never MIME.** `classify()`
(`internal/walk/walk.go:260-276`) uses `filepath.Ext` against a
configured md set (default `{".md", ".markdown"}`), a `textExtensions`
map and a `textNames` map for extension-less names, falling back to
`other`. There is no `http.DetectContentType` and no sniffing anywhere
in the tree. `walk.go:4` states the walk result "is the security
allowlist for serving content", and serving checks membership in that
walked set rather than re-deriving from the path.

That is the safer of the two designs and worth keeping deliberately: no
sniffing means no content-type confusion, and an allowlist derived from
a gitignore-aware walk means the ignore rules are load-bearing for
security rather than merely cosmetic. Widening should extend the
extension sets — **it should not introduce sniffing.**

**Design questions.**

- Which classes gain what? Three different things are being conflated:
  *listed* in the index, *linkable* from another document, and *served*
  as bytes. They need not move together.
- Rendering per class: syntax-highlighted source, plain `<pre>`, raw
  download? A highlighter is a dependency the project has so far avoided.
- Binary and large files: images inline is the obvious want, but that is
  the first content type where "serve the bytes" has a real
  content-type-header decision, and thus the first place sniffing gets
  proposed. Recommend an extension→content-type table, never sniffing.
- Does `Options.Extensions` (already configurable, `walk.go:40`) become
  a user-facing flag, or stay an API knob?
- `hasHiddenSegment` (`walk.go:277`) hides anything under a dot
  directory. `.github/workflows/*.yml` is a common thing to want to
  read, and today it is hidden — worth deciding explicitly rather than
  inheriting.

---

## Resolved

(none yet — this file just started)
