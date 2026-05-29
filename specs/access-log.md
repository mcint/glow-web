# Access logging — `--log-level`

The `web` server shows a terse per-request heartbeat by default (`dot`).
`--log-level` tunes that single channel: silence it (`off`) or expand it to a
structured line (`info`).

## Levels

| Level   | What it writes per request                                | Use it for                                  |
| ------- | --------------------------------------------------------- | ------------------------------------------- |
| `off`   | nothing                                                   | quiet local reading                         |
| `dot`   | `\|` for a primary page view, `.` otherwise (no newline)  | **default**; "is anything happening?" heartbeat |
| `info`  | `<client> <method> <path> <status> <bytes> <duration>\n`  | debugging traffic, basic access-log shape   |

`off`, `none`, `silent`, and the empty string are accepted as synonyms for
disabling.

## `dot`: primary vs internal

`dot` writes one character per request, so a stream of requests reads as a
heartbeat. A request is **primary** — a top-level page view the user navigated
to (index, rendered/text file view, raw/download) — or **internal**: a helper
fetch under the reserved `/_/` namespace (`/_/files` for the palette, `/_/render`
for the edit preview). Primary requests mark `|`; internal ones mark `.`, so a
page load and its follow-on fetches punctuate visibly:

```
|.....|..|.......|..
```

Each `|` is a page the user opened; the `.`s after it are that page's internal
chatter. The URL prefix (when mounted behind a proxy) is trimmed before the
`/_/` test, so classification is identical with or without `--url-prefix`.

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

`logMiddleware(level, out, urlPrefix, next)` lives in `internal/web/log.go`
and is applied as the outermost wrapper in `Server.Handler()`. `LogOff`
short-circuits to return `next` unchanged so the no-log path has zero
allocation overhead. The middleware sits *outside* `http.StripPrefix` so
log lines show the externally-visible URL (with prefix), not the
internally-rewritten one. `isPrimaryRequest(r, urlPrefix)` trims the prefix
and tests for the `/_/` namespace to pick the `|` vs `.` mark.

## Future axes

- A `debug` level that adds `User-Agent` and referer.
- Carrying the primary/internal distinction into `info` too (e.g. a column or
  a suppress-internal flag). `dot` already separates them via `|` vs `.`.
- A `--log-format=common|combined|json` knob if `info` ever stops being
  enough — split format from level once the dimensions actually disagree.
