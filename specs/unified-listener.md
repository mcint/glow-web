---
title: Unified listener — many glow-web projects on one host port, under /<name>/
date: 2026-11-15
kind: draft-spec
status: accepted; building rung by rung (see §8 Status)
relates:
  - ../docs/research/2026-11-15-unified-listener-prior-art.md (prior art, full report)
  - ~/dev-llm/lmux/docs/specs/socket-auto-serve.md (dir-of-sockets convention, lmux side)
  - ~/dev-llm/lmux/docs/specs/project-tools.md (glow-web mounted at /glow-web/ per project host)
  - ./spec.md § Browser-local UI state (KeyPrefix — already origin-share-safe)
  - ../sessions/2026-05-18-requests.md (GLOW_WEB_URL_PREFIX env; refuse X-Forwarded-Prefix)
tldr: >
  Today every glow-web is its own TCP port. A ladder of rungs, each
  implemented and tested alone: (A) `--listen unix:`; names = lmux's
  kebab(basename); (B1) `glow-web hub` serving pinned mounts (symlinks in
  ~/.local/state/glow-web/mounts); (B2) lease mode: the hub serves a root only
  while that dir's process holds an flock on its run record; (C) proxy
  mode: the session serves its own socket and the hub forwards verbatim;
  (V) a tiny versioned record contract so the hub proxies across
  glow-web version skew and either side upgrades alone; (D) first
  listener wins over the on-disk registry; (E) lmux as an optional
  TLS/DNS front to the hub's socket (glow-web never depends on it); auth last.
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
  Same project, same name in glow-web and in lmux (golden-tested against
  lmux's output). Where lmux's slug isn't a valid URL segment (`.claude`,
  `Foo Bar`), glow-web maps other characters to `-`, trims leading
  `._-` and caps the result at 63 chars; with nothing left, the name is
  `root`. `glow-web name [PATH]` prints it.
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

### B. `glow-web hub` — many roots, one process, two registration modes

```
glow-web hub [--addr 127.0.0.1:7171]
glow-web web a/ b/ c/                # sugar: ephemeral hub over these roots
```

The hub mounts roots at `/<name>/` and serves `/` as an index (name,
root, mode, version, live). It reads two places (§7): **`mounts/`** in
config, which holds what you meant to keep, and **`run/`** in state,
which holds what is alive right now. It re-reads them on each index
request and on a cheap mtime check, so no fsnotify dep is needed at
first.

#### B1. Pinned: serves until you unpublish

```
~/.local/state/glow-web/mounts/
  notes         -> /Users/me/notes
  claude-collab -> /Users/me/dev-llm/ClaudeCollab
```

`glow-web publish ~/notes [--name notes]`, `glow-web unpublish notes`
and `glow-web ls` are verbs, each with an inverse, and thin wrappers
over `ln -s`, `rm` and `ls`. A pinned mount survives the session that
made it (pow `~/.pow`, Valet `link`, `tailscale serve --bg`). It lives
in **state**, beside `run/`: one `glow-web` dir holds everything the
hub serves, so the pins share fate with the hub's records (wipe or move
one dir, and the machine's doc site goes with it). Pins are still never
auto-pruned.

#### B2. Lease: served by the hub only while the dir's process is open

`glow-web web . --hub=lease` binds no port. It writes
`run/<name>.json` (§4.V record) and holds an `flock` on it for as long
as it runs. The hub serves that root **in-process** while the lock is
held. When the process exits or crashes, the kernel drops the lock and
the hub stops serving it (a plain 404; the record is pruned on the next
read, so there's no "last seen" to show) and the name is free again. The session process does nothing but hold the lock:
no listener and no proxying, the narrowest path of all.

- Trade-off: the root is rendered by the **hub's** binary and default
  flags. Per-mount flags, if needed later, go in the record or in a
  `mounts/<name>.toml` beside a pin (**fork N2**: start without).

### C. Proxy: the session serves itself, the hub forwards verbatim

`glow-web web . --hub=proxy`:

1. listens on `unix:run/<name>.sock` with `--url-prefix /<name>` (A);
2. writes `run/<name>.json` with `"sock"` set, and holds the flock;
3. prints `http://127.0.0.1:7171/<name>/` as its URL.

The hub routes `/<name>/…` to `httputil.ReverseProxy` over a unix
`DialContext`, with the **path unstripped and no body or header
rewriting** beyond the stdlib's hop-by-hop handling. Liveness is the
flock plus the socket connecting. On failure the hub returns a 502 card
naming the root and prunes the entry. Use proxy when the session must
keep its own binary or flags (e.g. `glow-web-dev` while hacking
glow-web itself) and for any version skew (§4.V).

| entry | where | hub does | lives until |
|---|---|---|---|
| symlink → dir | `mounts/` | serves in-process (B1) | `unpublish` |
| `<name>.json`, no `sock` | `run/` | serves in-process (B2) | lock dropped |
| `<name>.json` + `<name>.sock` | `run/` | proxies verbatim (C) | lock dropped |

- `--hub=auto` (the eventual default): lease when the versions are
  compatible (§4.V), otherwise proxy. `--hub=off` / `--standalone` /
  `GLOW_WEB_HUB=off` gives today's behaviour. **Fork N4**: keep `off`
  the default for one release, then switch to `auto`.
- A run record shadows a pinned mount of the same name and root; the
  index shows the mode in use. A record with a *different* root under
  the same name is a collision (N1).

### V. Version contract: proxy across skew, upgrade without coordination

The hub and its instances share a **tiny, separately versioned
contract**, not a glow-web version:

```json
// run/<name>.json — contract v1
{ "contract": 1, "name": "glow-web", "root": "/Users/me/dev-llm/glow-web",
  "pid": 4242, "version": "v0.14.2-3-gabc+dirty", "sock": "glow-web.sock",
  "started": "2026-11-15T10:00:00Z" }
```

What v1 promises: the record shape, the flock rule, HTTP/1.1 over the
unix socket, and an instance that serves under `/<name>` (it was told
its prefix). Nothing else. The rendering, templates and routes under
`/<name>/` are the instance's own business.

| situation | hub does |
|---|---|
| same `contract` major, any glow-web version, `sock` set | proxy (always safe: the hub never interprets the content) |
| lease record, hub version == instance version | serve in-process |
| lease record, versions differ | `--hub=auto` instances never write this: they open a socket and go to proxy. If one exists anyway (written with `--hub=lease`), serve it in-process and mark it "rendered by hub vX" in the index |
| unknown `contract` major | don't route; index lists it as "incompatible (contract N)" with both versions |
| unknown fields | ignore (adding fields is a minor change; removing or renaming one is a major change) |

Consequences:

- **Upgrading the hub** (launchd restart, or a D takeover by a newer
  binary) disturbs no proxied session. Sessions older or newer than
  the hub keep working as long as the contract major matches.
- **Upgrading a session** never requires restarting the hub. A
  `glow-web-dev` session beside stable ones is the normal case, not
  an edge case.
- The index shows each mount's version and highlights skew. It does
  not enforce matching versions, which keeps lmux's
  restart-with-approval flow (glow-web-lifecycle.md) a separate,
  optional layer.
- In D, a takeover winner whose contract is older than a record's
  leaves that record alone. A hub never deletes what it cannot read.

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

### E. lmux in front of the hub (the current deployment; optional)

glow-web does all of A–D **on its own**: no lmux dependency, no lmux
awareness in glow-web code. lmux is one deployment shape: a TLS, DNS
and auth front that forwards to glow-web's hub, just as it would to any
other upstream.

- The hub always also listens on `run/hub.sock`, beside its TCP port.
  lmux adds **one** upstream: `docs.lmux.test` → `unix//…/run/hub.sock`,
  forwarded unstripped. glow-web's registry stays the only source of
  truth for what is mounted. lmux does not read `run/` and does not
  spawn glow-web for docs.
- **Use a subdomain, not a path.** With `docs.lmux.test/<name>/`, the
  hub's prefix stays empty, so each instance's prefix is just
  `/<name>` everywhere: the same URLs direct on `:7171` and through
  lmux. A path mount (`lmux.test/docs/`) would give the hub
  `--url-prefix /docs`. Proxied sessions would then need
  `/docs/<name>`, which means reading the hub's prefix from
  `run/hub.json` at start and restarting whenever it changes. That is
  workable, but it is coupling. **Recommend the subdomain.**
- Per-project hosts (`<slug>.lmux.test/glow-web/`) 302 to
  `docs.lmux.test/<slug>/`, or lmux keeps spawning there until the hub
  is trusted (**fork N5**, lmux's call; url-stability §8 favours the
  redirect).
- Not chosen: lmux becomes the hub itself by reading glow-web's `run/`
  records and proxying to session sockets. That puts glow-web's
  contract in a second implementation (Python) and breaks "glow-web
  does it alone". It stays possible later, because contract v1 is
  plain JSON.
- With lmux in front, auth is lmux's gate. glow-web's own §6 middleware
  is for standalone LAN or tailnet use.

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

- Every state-changing request (save, `/_/render`) goes through
  `http.CrossOriginProtection`, in `web.Server.Handler`, so standalone,
  hub-mounted and proxied servers share one gate. A cross-site page can't
  POST into a loopback glow-web. Same-origin fetches pass via
  `Sec-Fetch-Site`, even when a front (lmux) rewrites Host; header-less
  CLI clients pass. Shipped 2026-11-16 after a security review of rung 3.
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

One state dir, `~/.local/state/glow-web/` (`$XDG_STATE_HOME`), with
**fate-sharing** between pins and live records. That was the user's call
(2026-11-16) over the "pins are config" argument (dotfile-syncable intent):

| thing | path | why there |
|---|---|---|
| pinned mounts | `~/.local/state/glow-web/mounts/` | durable and machine-local, with symlinks to absolute paths; never auto-pruned |
| run records, session sockets, `hub.sock`, hub lock | `~/.local/state/glow-web/run/` (`$XDG_RUNTIME_DIR/glow-web/` when set) | live and machine-local. Pruned by the hub, but only after liveness fails |
| hub port | `127.0.0.1:7171` (**fork N7**: pick) | — |

**Not `~/.cache`.** Cache means "safe to delete at any time", and
deleting a live socket silently unroutes a running session.
**Not `$TMPDIR`.** On macOS it is a long `/var/folders/…` path, and
`sun_path` is capped at 104 bytes. Even `~/.local/state/glow-web/run/`
costs ~41 bytes for a typical home, so a socket is `<name>.sock` when
the full path fits in 103 bytes, else `<fnv32(name)>.sock`, with the
record's `sock` field giving the mapping. Both dirs are created 0700;
the hub refuses them if they are group- or world-writable or not owned
by `$UID` (§5).

## 8. Rungs: implement, validate and test one at a time

Each rung is its own branch-sized change: spec delta, red tests, code,
green, a manual check, commit. Later rungs depend only on earlier ones
being *merged*, never on future ones.

| # | rung | red tests first | manual validation |
|---|---|---|---|
| 1 | **A** `--listen unix:` | serve on a temp socket; GET via a unix-dial client; stale socket replaced only when dead; perms 0600 | `glow-web web . --listen unix:/tmp/g.sock & curl --unix-socket /tmp/g.sock http://x/` |
| 2 | **names** (§3) | kebab rule matches lmux's `_kebab` on a fixture list; invalid names rejected; `_` reserved | `glow-web name ~/dev-llm/ClaudeCollab` → `claude-collab` |
| 3 | **B1** hub + pinned mounts + `publish/unpublish/ls` | two temp roots, index lists both, `/<a>/` links carry `/<a>`; unpublish → 404; dir perms enforced; Host check | `glow-web publish ~/notes; glow-web hub; open :7171/` |
| 4 | **B2** lease | flock held → served; process killed → 404 + pruned; record shadows pin | `glow-web web . --hub=lease`, Ctrl-C, reload |
| 5 | **C** proxy | fake backend echoes path/headers/body: verbatim; dead sock → 502 + pruned | `glow-web web . --hub=proxy`; `curl -i :7171/<name>/` |
| 6 | **V** contract + `--hub=auto` | record round-trips; unknown major → "incompatible", not routed; unknown fields ignored; auto chooses lease vs proxy by version | run `glow-web-dev` beside `glow-web` against one hub; index shows the skew, both serve |
| 7 | **D** first listener wins | two in-process "sessions": the first binds, the second registers; kill the first → the second takes the port and serves both from disk | two terminals, kill the first, reload the bookmark |
| 8 | **E** lmux front (lmux repo; glow-web unchanged) | lmux route test against a stub `hub.sock` | `open https://docs.lmux.test/` matches `:7171/` link for link |
| 9 | **auth** (§6) | 401 without creds, 200 with; one realm; applies to every mount kind | `--auth-htpasswd FILE` |

**Status** (2026-11-16): rung 1 done (`--listen`), rung 2 done (`glow-web
name`), rung 3 done (`hub`, `publish`/`unpublish`/`ls`, ephemeral
`web a/ b/`), rung 4 done (`web --hub=lease [--name]`; run records are
written under a hidden temp name, flocked, then `link()`ed into place, so
none is ever visible unlocked and two registrants can't overwrite each other;
crash-tested with a SIGKILLed child). Decided while building: N1 = refuse and name the holder; N7 =
7171. The registry is re-read per request (one ReadDir), not on an mtime
check: simpler, and correct when a pin's target disappears. Under a hub,
`--title-prefix` defaults to the mount name.

Rungs 3–4 and 5 are independent once 2 lands. Rung 6 needs 4 and 5.
Rung 7 needs 3. Rung 8 needs 3 (the hub on `hub.sock`); before that,
lmux can already forward to a single session via rung 1.
