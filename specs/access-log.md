# Access logging — `--log-level`

The `web` server is silent by default (only the boot notice goes to stderr).
`--log-level` opens a single per-request channel for users who want to *see*
traffic — either as a terse heartbeat or as a structured line.

## Levels

| Level   | What it writes per request                                | Use it for                                  |
| ------- | --------------------------------------------------------- | ------------------------------------------- |
| `off`   | nothing                                                   | default; quiet local reading                |
| `dot`   | `.` (no newline)                                          | "is anything happening?" heartbeat          |
| `info`  | `<client> <method> <path> <status> <bytes> <duration>\n`  | debugging traffic, basic access-log shape   |

`off`, `none`, `silent`, and the empty string are accepted as synonyms for
disabling — the default flag value is `off` so unconfigured behavior is
identical to pre-flag behavior.

## Why three levels and not a count

We could have done `-v`, `-vv`, `-vvv`. We didn't, because:

- The user's mental model here is *what comes out*, not *how loud*. `dot`
  vs `info` is a format choice, not an intensity dial.
- `dot` is a format that has no "less" — going below it lands on `off`.
  Numeric ladders imply you can keep climbing in either direction.
- `--log-level` matches the idiom users see in nginx, caddy, systemd, etc.

## Format details

`info` line shape:

```
127.0.0.1:54321 GET /alpha.md 200 2148 1.234ms
```

- *client* is `r.RemoteAddr` verbatim, or `-` if empty (httptest etc.).
- *path* is `r.URL.RequestURI()` so the query string survives.
- *status* is captured via a tiny `ResponseWriter` wrapper; defaults to
  200 if the handler never called `WriteHeader` (the stdlib's behavior).
- *bytes* counts response body bytes only (not headers).
- *duration* is `time.Since(start).Round(time.Microsecond)`.

Output goes to `s.LogOut`, which the CLI leaves nil → `os.Stderr`. Tests
inject a `*bytes.Buffer` to assert without racing against stderr.

## Non-goals (v0)

- Structured/JSON logs. Add when something downstream needs to parse them
  (`jq`, Loki, etc.) — premature today.
- Log rotation, file destinations, syslog. The CLI invocation can redirect
  stderr; the server doesn't open files.
- Request-body logging or header dumps. That's a debugger, not an access
  log; would need its own knob with a clearer security story.
- Sampling / rate-limiting. The current shape is one writer call per
  request; if it ever shows up as a hotspot we can wrap with `bufio.Writer`
  before adding sampling.

## Implementation

`logMiddleware(level, out, next)` lives in `internal/web/log.go` and is
applied as the outermost wrapper in `Server.Handler()`. `LogOff`
short-circuits to return `next` unchanged so the no-log path has zero
allocation overhead. The middleware sits *outside* `http.StripPrefix` so
log lines show the externally-visible URL (with prefix), not the
internally-rewritten one.

## Future axes

- A `debug` level that adds `User-Agent` and referer.
- Per-route filtering (suppress `/_/files` polling from the palette).
- A `--log-format=common|combined|json` knob if `info` ever stops being
  enough — split format from level once the dimensions actually disagree.
