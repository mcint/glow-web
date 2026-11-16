---
question: How do other tools let many local server instances share one listener (path or host mounts, registration, liveness), and what should glow-web steal?
date: 2026-11-15
kind: full-report
tldr: >
  The winning light pattern: each instance listens on a unix socket in an
  on-disk registry dir (pow/puma-dev/Jupyter/emacs), and a router that keeps
  no state of its own owns the TCP port. Any instance can become that router
  by bind() (emacsclient -a ''). Prefixes are app-aware (Jupyter base_url,
  glow-web --url-prefix), forwarded unstripped, never rewritten. Avoid
  SO_REUSEPORT, fd handoff, and routing state held only in memory. Most peers
  route by host (*.localhost) to dodge origin sharing; glow-web's per-root
  KeyPrefix already covers localStorage, so path routing stays viable.
consumers: ../../specs/unified-listener.md
method: >
  one research subagent; tailscale serve, portless and localias checked on
  the web, the rest from model knowledge (cutoff mid-2026). "(unverified)"
  marks are kept as the agent wrote them.
---

# Prior art: shared listener for many instances

> **Correction from the parent session:** the agent flagged shared-origin
> localStorage as a regression. glow-web already namespaces keys
> `glow-<fnv(root)>-…` (`internal/web/web.go`, `keyPrefix`), so that risk
> is already handled. Cookies (none today) would need `Path=/<name>/`.

## 1. Go building blocks

- **`httputil.ReverseProxy` over a unix socket.** Use a `Transport` whose
  `DialContext` dials `unix` at the socket path, and the `Rewrite` hook
  (`SetURL`, `SetXForwarded`). `X-Forwarded-Prefix` is not set
  automatically. Set `FlushInterval: -1` for streaming. When the dial
  returns ECONNREFUSED, the backend is dead, so prune its entry. This is
  the narrowest pass-through available.
  <https://pkg.go.dev/net/http/httputil#ReverseProxy>
- **Socket activation.** On systemd, `LISTEN_FDS` (via `go-systemd/activation`)
  is trivial. On launchd, a plist `Sockets` entry with
  `launch_activate_socket()` needs cgo (unverified which wrapper is
  maintained). puma-dev uses this for ports 80 and 443. It is worth having
  later for an on-demand daemon, not to start with.
- **SO_REUSEPORT: avoid.** On Linux it load-balances connections across
  processes at random, so it cannot route by path. It is meant for
  zero-downtime restarts.
- **fd passing (SCM_RIGHTS), as in cloudflare/tableflip and Envoy hot
  restart: overkill.** If the routing table lives on disk, a successor can
  just `bind()` once the old router exits.
  <https://github.com/cloudflare/tableflip>

## 2. Local dev multiplexers

| Tool | Registration | Routing | Lifecycle | Steal |
|---|---|---|---|---|
| pow (`~/.pow`) | symlink `<name>` → app dir, or a file holding a port | host `<name>.dev` | spawns apps, reaps them when idle | the filename is the route; `ln -s` is the API |
| puma-dev | same symlink model | host `.test`/`.localhost`; backends on **unix sockets** | starts on demand, idle stop after ~15 min | unix-socket backends; launchd sockets |
| Laravel Valet | `park` (each subdir becomes a site) / `link` (symlink) | nginx + dnsmasq, host-based | always on | the park/link split = "publish a directory" |
| hotel | `~/.hotel/servers/<name>.json` | host-based + dashboard at `/` | daemon spawns and keeps servers | one file per app; index page |
| portless | `portless run`; **name from the project dir**; state dir set by `PORTLESS_STATE_DIR` | host `<name>.localhost`; branch subdomains | **proxy auto-starts on first run**; `prune` checks PIDs and process groups; **refuses a state dir writable by group or others** | dir-derived name, auto-start, state-dir permission check. <https://github.com/vercel-labs/portless> |
| localias | alias→port config file | embedded Caddy | daemon | Caddy embeds as a Go library. <https://github.com/peterldowns/localias> |
| Caddy admin API | `/load`, `/config/…` (admin can listen on a unix socket) | `reverse_proxy unix//…`; `handle_path` strips the prefix, `handle` keeps it | daemon | keep vs strip; atomic config swap |
| Traefik file provider | watched directory of route files | `PathPrefix`; `StripPrefix` sets `X-Forwarded-Prefix` | daemon | a watched dir as the registry |
| tailscale serve | `serve --bg --set-path=/app 8080`; `… off` removes it | longest prefix wins; strips the mount path (per a third-party cheat sheet) | tailscaled keeps the config after the CLI exits | **publish outlives the session; explicit off.** <https://tailscale.com/kb/1242/tailscale-serve> |
| cloudflared / ngrok | YAML ingress rules | host + path regex, no stripping | daemon | ordered rules with a catch-all 404 |
| devd | CLI `/prefix=http://…` | path routes | single process | one-line route syntax |

Most of these route by **host**, which avoids the origin and absolute-link
problems in §5. tailscale serve, devd and Traefik route by path.

## 3. "First instance becomes the server"

- **emacs server.** Socket in `$XDG_RUNTIME_DIR/emacs/` (or
  `/tmp/emacs$UID/`). `emacsclient -a ''` means: connect, and if that
  fails, start a daemon and retry. A stale socket is one where connect
  fails. This is the cleanest "connect or become" idiom.
- **tmux.** Socket at `/tmp/tmux-$UID/default`. Any client starts the
  server if none exists. `exit-empty`: the server exits when its last
  session does.
- **VS Code / Electron single-instance lock.** The socket name is a hash
  of the data dir plus version, which keeps names short. A second instance
  forwards its argv and exits.
- **Jupyter.** Each server writes `jpserver-<pid>.json` (port, token,
  `base_url`, root) into its runtime dir; `jupyter server list` reads them
  and prunes stale ones. It is prefix-aware via `base_url`, so nothing is
  rewritten.
- **Ollama.** A singleton on a fixed port; nothing is multiplexed.
- **browser-sync.** Hops to the next port when one is busy. Avoid: that
  hopping is the sprawl being escaped.

**Failure mode of "first wins":** if the leader keeps routes only in
memory, every route dies when it exits. Keep the registry on disk and
the router has no state, so any instance can take its place.

## 4. A directory as the registry

- `<dir>/<name>.sock` plus an optional `<name>.json` sidecar (root, pid,
  started, owner). Liveness = the socket connects. GC = delete on
  ECONNREFUSED or ENOENT. An `flock` on the sidecar is better than a pid
  check, because the kernel releases the lock on crash and pids get reused.
- **macOS:** `$XDG_RUNTIME_DIR` is unset, `$TMPDIR` is a long
  `/var/folders/…` path, and **`sun_path` is capped at 104 bytes**. Use
  `~/.local/state/glow-web/` or `/tmp/glow-web-$UID/` (0700, with an
  ownership check) and keep socket names short or hashed. macOS has no
  abstract sockets.
- Docker Desktop follows the same pattern: a socket for data, files for
  metadata.
- Sockets at 0600 in a 0700 dir give visible attenuation. Loopback TCP is
  open to every local user and to browsers.

## 5. Path-prefix pitfalls

- **localStorage is per origin** (scheme, host and port). glow-web already
  handles this with `KeyPrefix`; see the correction above. Host routing
  (`<name>.localhost`) isolates storage for free. Chrome and Firefox
  resolve `*.localhost` to loopback; Safari is uneven (unverified,
  current).
- **Cookies ignore the port.** Scope any future cookie with `Path=/<name>/`.
- **Absolute links and redirects.** Use an app-aware prefix with no
  rewriting (Jupyter `base_url`, Caddy `handle`). Don't rewrite HTML or
  `Location` headers in the proxy. Distrust `X-Forwarded-Prefix` from
  non-loopback peers. glow-web's own 2026-05-18 request goes further:
  ignore it entirely and take the prefix from startup config.
- **Service workers** are scoped by path. Register at `/<name>/`, never
  at `/`.
- **DNS rebinding.** Check the `Host` header in the shared router.
  Several dev servers shipped CVEs for missing this check.
- An index at `/` (hotel, Jupyter list) is cheap to build from the
  registry dir.

## 6. Basic auth

- Caddy's `basic_auth` (renamed in 2.8) takes bcrypt hashes from
  `caddy hash-password`, set per route. Traefik's `basicAuth` reads
  htpasswd files. pow, puma-dev and portless have none and rely on
  loopback.
- In Go, `r.BasicAuth()` plus `bcrypt.CompareHashAndPassword` is about 20
  lines. `tg123/go-htpasswd` exists (unverified that it is maintained).
  Use one realm on the shared port; per-prefix realms cause repeated
  prompts.
- Gate in the router only, once. Backends on 0600 sockets need no auth of
  their own.

## Ranked for a light, hackable Go tool

1. Each instance serves a unix socket in the registry dir, and a router
   with no state of its own owns the port. Whoever binds first runs it;
   survivors re-bind on a tick. Unregistering means deleting the file,
   and `ls` is the status command.
2. App-aware prefix, forwarded verbatim (`--url-prefix`).
3. "Publish" = a socket-less entry (symlink or `{root}` sidecar) served
   in-process. Add a `while-any-session` option for tmux-style lifetime.
4. An optional dedicated daemon (launchd/systemd) running the same router
   code over the same registry.
5. Basic auth as router middleware: opt-in, bcrypt, one realm.
6. Host routing (`<name>.localhost`) as an alternative mode.

**Avoid:** SO_REUSEPORT, fd handoff, routing state held only in the
leader's memory, port hopping, and HTML or `Location` rewriting.
