---
title: Unified listener — many glow-web projects on one host port, under /<name>/
date: 2026-11-15
kind: draft-spec
status: draft — proposals with open forks; nothing implemented
relates:
  - ../docs/research/2026-11-15-unified-listener-prior-art.md (prior art, full report)
  - ~/dev-llm/lmux/docs/specs/socket-auto-serve.md (dir-of-sockets convention, lmux side)
  - ~/dev-llm/lmux/docs/specs/project-tools.md (glow-web mounted at /glow-web/ per project host)
  - ./spec.md § Browser-local UI state (KeyPrefix — already origin-share-safe)
  - ../sessions/2026-05-18-requests.md (GLOW_WEB_URL_PREFIX env; refuse X-Forwarded-Prefix)
tldr: >
  Today every glow-web is its own TCP port. Proposal ladder, each rung
  useful alone: (A) `--listen unix:PATH`; (B) `glow-web hub` serving many
  roots in-process at /<name>/, fed by a registry dir of symlinks
  ("publish a directory" = `ln -s`); (C) session instances register a
  socket in that same dir and the hub proxies /<name>/ to it, verbatim;
  (D) first-listener-wins — cheap because the registry is on disk and
  the router keeps no state of its own; (E) lmux mounts the hub once. Recommended: A → B → C → D,
  with E as one lmux route. Basic auth is one hub middleware, later.
---

# Unified listener

## 1. Problem

One glow-web per project, per session, each on its own port. Tabs are
found by port number; nothing on the host lists what is being served;
lmux spawns its own copies on further loopback ports. The ask: one
listener, one doc site, each project at a stable path, and standalone use
still working with no daemon at all.

## 2. What already makes this cheap (verified in code, 2026-11-15)

- `Server.Handler()` wraps in `http.StripPrefix(URLPrefix)`; every
  generated link is prefix-aware. A `Server` is a value — N of them can
  be mounted in one `http.ServeMux` today with no refactor.
- localStorage keys are namespaced `glow-<fnv(root)>-…`, so many
  projects on one origin do not trample theme/scroll/sort state.
- lmux already forwards unstripped to a prefix-aware glow-web and has a
  draft dir-of-sockets convention (`socket-auto-serve.md`).
- Missing: a non-TCP listener (`Serve` hard-codes `net.Listen("tcp")`),
  any multi-root entry point, and a name rule.

## 3. Names (shared by every proposal)

- Default name = lmux's rule: `kebab(basename(root))` (CamelCase →
  kebab, lowercase, collapse `-`). `ClaudeCollab` → `claude-collab`.
  Same project, same name in glow-web and in lmux.
- Override: `--name NAME` / `GLOW_WEB_NAME`. Validated
  `^[a-z0-9][a-z0-9._-]{0,62}$` — never a `/`, never `..`, never `_`
  first (reserves `/_/` for the hub's own routes).
- Collision: **fork N1** —
  (a) refuse the second with a message naming the holder and suggesting
      `--name` (least surprise; recommended);
  (b) auto-suffix with the parent `<name>.<group>` (lmux's group) and
      warn;
  (c) Claude Code-style full-path slug (`-Users-x-dev-foo`) as canonical
      with basename as alias — unique but ugly in URLs.

URL: `http://127.0.0.1:<hubport>/<name>/…`. Instance runs with
`--url-prefix /<name>` — set by the hub/registration path, not by a
request header (per the 2026-05-18 request: never trust
`X-Forwarded-Prefix`).

## 4. Proposals

Each is a rung: shippable alone, and the next one builds on it.

### A. `--listen unix:PATH` (and `--addr` stays)

`--addr` keeps meaning TCP. New `--listen` accepts `tcp:HOST:PORT` or
`unix:PATH`; `--addr X` ≡ `--listen tcp:X`. Socket created 0600 in a
0700 dir, stale file removed only if `connect` fails.

- Unblocks: Caddy `reverse_proxy unix//…`, lmux's socket-auto-serve
  spec, and everything below. ~40 lines + tests.
- Cost: none to standalone users.

### B. `glow-web hub` — many roots, one process, registry dir

```
glow-web hub [--addr 127.0.0.1:7171] [--registry DIR]
glow-web web a/ b/ c/                # sugar: ephemeral hub over these roots
```

The hub mounts one `Server` per root at `/<name>/` in one process and
serves `/` as an index of mounts (name, root, kind, live). Mounts come
from flags **and** a registry directory (default
`~/.local/state/glow-web/mounts/`, 0700), re-read on each index request
and on a cheap mtime check (no fsnotify dep needed at first):

```
mounts/
  glow-web      -> /Users/me/dev-llm/glow-web        # symlink = pinned mount
  claude-collab -> /Users/me/dev-llm/ClaudeCollab
```

"Publish a directory" is `ln -s ~/notes ~/.local/state/glow-web/mounts/notes`
(or `glow-web publish ~/notes [--name notes]`, `glow-web unpublish notes`,
`glow-web ls` — verbs and their inverses, thin wrappers over `ln`/`rm`/`ls`).
It survives the session that made it — the "host-local glow-web keeps
serving it" mode. Prior art: pow/puma-dev `~/.pow` symlinks, Valet `link`.

- No IPC, no proxying: narrowest possible path.
- Trade-off: every pinned mount runs the hub's binary and flags (one
  version, one `--read-only` policy). Per-mount flags, if ever needed,
  go in an optional `mounts/<name>.toml` beside the link — **fork N2**:
  start without.

### C. Session instances register a socket; hub proxies verbatim

A plain `glow-web web .` that finds a hub (registry dir has
`_hub.sock` that connects) does instead of binding a TCP port:

1. listen on `unix:$RUNTIME/glow-web/<name>.sock` with
   `--url-prefix /<name>` (A);
2. drop `mounts/<name>.sock` → that socket (symlink, or the socket
   itself if `$RUNTIME` = registry dir);
3. print `http://127.0.0.1:7171/<name>/` as its URL.

The hub routes `/<name>/…` to `httputil.ReverseProxy` over a unix
`DialContext`, **path unstripped, no body/header rewriting** beyond the
stdlib's hop-by-hop handling. Liveness = the socket connects; on dial
failure the hub returns a 502 card naming the root and removes the
dangling entry. Lifetime = the session: socket dies with the process.

So the registry entry's *type* is the whole policy:

| entry in `mounts/` | hub does | lives until |
|---|---|---|
| symlink → dir | serves in-process (B) | `unpublish` |
| `*.sock` (or link → sock) | proxies to it (C) | the session exits |

- Why proxy rather than "session just leases a dir the hub serves":
  the session keeps its own binary + flags (e.g. `glow-web-dev` while
  hacking glow-web itself), and the hub never needs to know them.
  **Fork N3**: also offer lease-only (`mounts/<name>` symlink +
  `mounts/<name>.pid`; hub serves while pid lives) — cheaper, but
  session flags are lost. Recommend: not at first.
- `--standalone` (or `GLOW_WEB_HUB=off`) forces today's behaviour.
  **Fork N4**: should registering be the default when a hub is found,
  or opt-in (`--hub`)? Recommend opt-in for one release, then default.

### D. First listener wins (no daemon required)

`glow-web web .` with hub mode on tries to bind the hub port. If it
wins, it *is* the hub (serves its own root in-process plus the
registry). If the port is held by a glow-web hub (probe
`GET /_/hub` → JSON), it registers as in C. If the hub process exits,
sessions notice (their hub-probe goroutine, every few seconds) and race
to bind; the winner re-reads the registry — sockets and symlinks
persist on disk, so URLs come back on the same port and paths.

- Prior art: Emacs server, VS Code single-instance socket, Jupyter
  runtime dir.
- What makes D cheap: **all routing state is on disk** (the registry
  dir), so the hub process holds nothing a successor must recover — it
  is a stateless router any instance can become, by plain `bind()`. No
  fd passing (tableflip/Envoy), no SO_REUSEPORT (it load-balances, it
  cannot route by path), no re-registration protocol. The emacsclient
  `-a ''` idiom: connect, else become.
- Cost: a probe/retake tick in every session, and "which process is the
  hub right now" is less visible (`glow-web ls` should say). Pinned
  mounts are served only while some glow-web runs — tmux's
  `exit-empty` semantics. **Recommend** D as the no-daemon default,
  with the same `glow-web hub` code optionally under launchd for an
  always-on site; one registry, so no design fork between them.

### E. lmux mounts the hub once

With B+C, lmux stops spawning per-project glow-web for docs and adds
one route: `lmux.test/docs/…` (or `docs.lmux.test`) → `unix//…/_hub.sock`,
hub run with `--url-prefix /docs`. Per-project hosts keep `/glow-web/`
as a 302 to `lmux.test/docs/<slug>/` or proxy `/glow-web/` →
hub `/<slug>/` (**fork N5**, lmux's call; url-stability §8 says
redirect). TLS, DNS and future auth stay lmux's job; glow-web hub
stays plain HTTP on loopback/unix.

### F. Alternative axis — host routing instead of path

`<name>.localhost:7171` instead of `127.0.0.1:7171/<name>/`. Chrome and
Firefox resolve `*.localhost` to loopback with no hosts edit; each
project gets its own origin (storage, cookies, service workers
isolated for free). This is why pow, puma-dev and portless route by host.
Costs: Safari/CLI resolver behaviour is uneven, and lmux already owns
host-based naming under `.lmux.test`. glow-web's KeyPrefix already
solves the storage half, so **recommend path routing** (curl-able and
bookmarkable); the hub can accept both later with no change to
instances, since the prefix is all the instance knows about.

## 5. Hub hygiene (applies to B–D)

- Check `Host` against `localhost`, `127.0.0.1`, `[::1]`, `*.localhost`
  and an allow-list flag, in the hub only (DNS-rebinding guard).
- Refuse a registry/runtime dir that is group- or world-writable or
  not owned by `$UID` (portless does this).
- Proxy with `FlushInterval: -1` (future SSE/live-reload), `Rewrite`
  hook, path unstripped, no HTML or `Location` rewriting.
- Liveness: socket connects. Optional `<name>.json` sidecar (root,
  pid, started, binary version) for `glow-web ls`; an `flock` on it
  beats pid checks (pids get reused, the kernel drops locks on crash).

## 6. Auth (later, not now)

One middleware at the hub — the single gate every mount passes
through (security-design: one decision point). `--auth-htpasswd FILE`
(bcrypt; `golang.org/x/crypto/bcrypt` is the one new dep) and
optionally per-mount `require` in `mounts/<name>.toml`. Behind lmux,
prefer lmux's gate and leave the hub's off. Edit/save endpoints on a
shared port are the first thing worth gating; pinned mounts could
default `--read-only` (**fork N6**).

## 7. Paths & files

| thing | default (macOS) | XDG |
|---|---|---|
| registry | `~/.local/state/glow-web/mounts/` | `$XDG_STATE_HOME/glow-web/mounts/` |
| session sockets | `mounts/` itself (sockets beside symlinks) | `$XDG_RUNTIME_DIR/glow-web/` |
| hub socket | `mounts/_hub.sock` | same |
| hub port | `127.0.0.1:7171` (**fork N7**: pick) | — |

macOS caps `sun_path` at 104 bytes and `$TMPDIR` is a long
`/var/folders/…` path, so keep sockets under `~/.local/state/glow-web/`
with short names (`<name>.sock`, name ≤ 63 chars, or hashed).

## 8. Recommended order

1. A (`--listen unix:`) — tiny, unblocks lmux immediately.
2. B (`hub` + symlink registry + `publish/unpublish/ls`) — the doc site.
3. C (session sockets) — "open while the session is open".
4. E in lmux — one route.
5. D (first listener wins) once C is in. With an on-disk registry it
   is a small addition, not a protocol. Auth when anything leaves loopback.

Tests per rung (red first): A — serve over a temp unix socket, GET
via a unix-dial client; B — hub with two temp roots, index lists both,
`/<a>/` links carry `/<a>` prefix, invalid names rejected; C — fake
session socket, hub proxies verbatim (headers/path/body echo),
dead socket → 502 + entry pruned.
