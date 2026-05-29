# Header layout — nesting mirrors scope

*The bar's structure follows one rule: **the UI/DOM hierarchy mirrors the UX
hierarchy, which mirrors the project / function / scope hierarchy** of what each
control acts on. Things that share a scope (and a display fate) nest together;
each control sits next to what it governs.*

**Status:** firm. Seeded 2026-05-28 (v0.6.7).

---

## The structure

```
<header class="bar">                      flex row, top-aligned
  <button id="sidebar-toggle">            ← project-nav scope: far left, by the sidebar
  <div class="browser">                   ← the permanent (file-)browser wrapper, flex:1
    <div class="browser-loc">             ← location (present in every view)
      <div class="parent-hint">           context path, stacked atop…
      <nav class="crumbs">                 …project name + breadcrumbs
    <div class="browser-controls">        ← dir-view ONLY: filter, count, class/hidden chips
  <nav class="bar-actions">               ← document actions + global theme, far right
```

### Why each placement

- **Sidebar toggle → far left.** The sidebar is a project-wide navigator that
  slides in from the left; its toggle belongs *next to it*, not buried among
  right-side actions. Scope: navigation across the project.
- **`.browser` is permanent; `.browser-controls` is conditional.** Crumbs
  (location) appear in every view, so they live in the always-present wrapper.
  The filter/count/chips only make sense over a *listing*, so they render only
  in dir/index view — their **display fate matches their scope**. A file view
  emits `.browser` with just the location and no controls.
- **Crumbs get the bar's free width.** `.browser` is a *column*: location on
  one row, controls on the next. So deep breadcrumbs no longer wrap against the
  filter row — the thing that motivated this layout. The controls dropping to
  their own row is the scope boundary made visible.
- **`.bar-actions` → far right.** Raw/Download/Edit (or View/Save) act on the
  *current document*, a different scope from browsing; the global theme toggle
  is app-wide. Both are "act on what's shown," distinct from "where am I /
  filter the listing," so they sit apart on the right.

## The principle (project design value)

> The UI hierarchy should match the UX hierarchy, and the project / function /
> scope hierarchy of the features it implements and the status it displays.

Controls live next to what they scope; co-scoped things nest and share a fate
(shown/hidden, enabled/disabled together). This is the least-surprise reading:
a viewer infers what's global, what's contextual, and what's grouped from the
nesting alone. Apply it whenever adding a surface — ask "what scope does this
belong to?" and nest accordingly, rather than appending to whatever row has
room.

## Verification

`go run ./cmd/glow-web web . --palette`:
- Index: ☰ at the left edge; context path stacked over crumbs; a second row
  with filter + count + chips; theme at the right. Deep paths keep crumbs on
  their own line.
- A file view: same left ☰ and crumbs, **no** filter row; Raw/Download/Edit +
  theme on the right.
Tests: `TestIndex_BrowserHeaderLayout`, `TestView_BrowserHeaderNoDirControls`.
