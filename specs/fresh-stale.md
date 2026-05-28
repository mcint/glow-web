# Freshness — fresh/stale indicator + live file updates

*Tell the reader whether what they're looking at still matches disk, without
being chatty, and without breaking when the server goes away. Phased: P1 is a
manual refresh + freshness indicator (client-only, no deps); P2 layers
server-push (SSE + fsnotify) on top.*

**Status:** P1 spec firm; P2 sketch. Seeded 2026-05-28.

Origin: `sessions/2026-05-28-requests.md §3`.

---

## Design force

A docs viewer is read-mostly and often left open while files change underneath
it (an editor saves, a build regenerates, `git pull` lands). The reader should
be able to *tell* when the page is stale — but glow-web is a **text viewer
first** (per the 2026-05-25 status note), so freshness signalling must be
**subtle and on-demand**, not a stream of toasts.

The other hard constraint: **keep files open if the server went away.** A
disconnected backend must never blank or break the page — the content already
in the DOM stays readable; only the freshness indicator changes state.

---

## P1 — manual refresh + freshness indicator (client-only)

Leans entirely on the existing `GET /_/files` endpoint, which already returns
`[{rel, url, mtime}]` JSON for the served tree. No server change.

### Behavior

1. **Snapshot on load.** Build a `Map<rel, mtime>` from the rendered index
   rows (rows already carry `data-rel` and `data-mtime`). Record a load
   timestamp.
2. **Freshness indicator** in `.bar-actions`: a small element showing
   `updated <relative> ago` (e.g. "updated 2m ago"), derived from the load /
   last-refresh timestamp. A status dot conveys connection state:
   - `● fresh` (neutral/muted) — last check matched, or just loaded.
   - `● stale` (accent) — a refresh found drift, or a refresh failed.
3. **Refresh action** (`refresh-files`): a button (and a keybinding TBD) that
   `fetch`es `/_/files` and diffs the returned set against the snapshot:
   - **new** — `rel` present in response, absent from snapshot.
   - **changed** — `rel` in both, mtime increased.
   - **gone** — `rel` in snapshot, absent from response.
   It updates the indicator to `N new · M changed · K gone` (omit zero
   categories; "in sync" when all zero) and marks the affected existing rows
   (changed → subtle tint/badge; gone → struck/dimmed). New/gone rows can't be
   fully rendered with the rich columns (git XY, numstat) from `/_/files`
   alone, so the indicator offers **reload** to get a fresh server render.
4. **Server-gone handling.** If the `fetch` rejects or is non-2xx, the DOM is
   left untouched, the dot goes `stale`, and the label reads
   `offline — showing last load`. A later successful refresh clears it. The
   page never navigates or blanks.

### Decisions (values)

- **Detect-and-signal, not live-rebuild.** P1 marks drift and offers reload; it
  does not patch git/numstat columns in place. Prioritizes *correctness +
  simplicity* over *immediacy* — a half-rendered rich row is worse than an
  honest "reload to refresh." Reversal: P2's push may rebuild rows if it proves
  worthwhile. (Excluded — marginally: instant in-place updates.)
- **mtime, not content hash.** Freshness keys off `/_/files` mtime, already
  free. Excludes detecting same-mtime content changes (rare; not worth a hash
  walk). Reversal: add an ETag/hash field to `/_/files` if mtime proves coarse.
- **No polling in P1.** Refresh is manual ("not chatty"). Auto-refresh is P2's
  job via push, not a client timer. Excluded — intentionally: background
  polling, which is the chattiness the request explicitly wanted to avoid.

### Verification (P1)

1. `go test ./...` green (server unchanged; assert `/_/files` still shape-stable
   and the index renders the indicator + refresh control).
2. `go run ./cmd/glow-web web .`: load index → "updated just now". Touch a
   file → refresh → "1 changed", row tinted. Add a file → "1 new", reload
   offered. Delete a file → "1 gone", row struck.
3. Kill the server, click refresh → dot `stale`, label `offline — showing last
   load`, DOM intact. Restart, refresh → clears.

---

## P2 — server-push (SSE + fsnotify), sketch

Layer onto P1: replace manual refresh with server-pushed change events.

- **Transport: SSE** (`text/event-stream`) over WS/long-poll. Flow is
  one-directional (server → client "something changed"); `EventSource` gives
  native auto-reconnect and works over plain HTTP. One new route, e.g.
  `GET /_/events`.
- **Watch: `github.com/fsnotify/fsnotify`** on the served root (debounced;
  respect the same walk filters as the index — ignore `.git/`, honor
  gitignore/hidden unless toggled). On a change, emit an event; the client
  re-runs the P1 diff (or, if cheap, re-fetches `/_/files`) and updates the
  indicator. Optionally trigger an auto-reload for the index, or just flip the
  dot to `stale` and let the reader choose.
- **Keep-open guarantee carries over:** `EventSource.onerror` → dot `stale`,
  DOM untouched; the browser retries automatically. No connection → P1 manual
  refresh is still the floor.
- **Decisions deferred to P2 spec:** debounce window; per-file vs coarse
  "tree changed" events; whether the file *view* page (not just the index)
  subscribes; auth/scope of the events route under `--url-prefix`.

Dependency note: P2 introduces the first runtime dep (`fsnotify`). P1 stays
dependency-free, which is why it ships first.
