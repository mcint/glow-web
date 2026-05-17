# Parent-hint above the breadcrumb

A small dim line above the breadcrumb showing the path from the serving
root up to `~` or `/`. Closes the "where am I in the filesystem?" gap when
glow-web only sees what's inside its serving root.

Filed 2026-05-16 from in-use feedback: "I want a small over-title /
over-breadcrumb hint to the pwd from glow-web root serving dir to ~ or /".

## Shape

```
~/dev-llm/                       ← parent-hint  (small, dim)
claude-collab / specs / x.md     ← existing breadcrumb
```

- Server-side: `Server.parentHint()` returns `displayRoot(filepath.Dir(Root))`,
  reusing the existing `~`-normalization. Empty when root is `/`.
- Template: `crumbs.html.tmpl` renders `<div class="parent-hint">` above the
  `<nav class="crumbs">` when non-empty.
- Style: `.parent-hint` is `font-size: .75rem; color: var(--muted); opacity: .7`.

## Behavior matrix

| Root                                       | Parent-hint shown |
| ------------------------------------------ | ----------------- |
| `~/dev-llm/claude-collab`                  | `~/dev-llm/`      |
| `~/dev-llm`                                | `~/`              |
| `~` (user's home)                          | `/Users/`         |
| `/var/log`                                 | `/var/`           |
| `/`                                        | _(hidden)_        |

(macOS path examples; same shape on Linux.)

## Why above, not in the title

The page `<title>` is for tab-strip and history; it already includes
`title-prefix` (port/host) for reverse-proxy contexts. Filesystem context
belongs visually next to the breadcrumb it anchors — same horizontal
position, different vertical band. Tightly coupled, not buried in `<title>`.

## What this doesn't try to do

- **Not clickable.** The parent dirs aren't inside the serving root, so
  glow-web can't render them. Inline navigation would imply otherwise.
- **No port/host info.** That's `title-prefix`'s job; this is purely
  filesystem context.
- **No truncation policy.** Long paths CSS-truncate via the existing
  `.crumbs` overflow rules. Edge case; revisit if it bites.
