---
title: Write policy — read-only by default, --writable=no|md|yes per mount
date: 2026-11-16
kind: spec
status: accepted; building
relates:
  - ./unified-listener.md §4.B2, §6 (fork N6: resolved here)
  - ./spec.md § Subcommand surface
tldr: >
  glow-web is read-only unless a mount's owner says otherwise. One policy
  value, --writable=no|md|yes (default no; --readonly = --writable=no).
  Policy belongs to the mount, not to whichever process holds the hub role:
  pins store it in <state>/writable/<name>, leases carry it in their run
  record, proxies and standalone servers use their own flag. Unknown values
  fail closed to no. The grammar leaves room for path filters and per-user
  rules later.
---

# Write policy

## 1. Why

Editing in the browser is a write capability on the served tree. On one
standalone server it was opt-out (`--readonly`), which was tolerable. With a
hub on a well-known port serving every project, opt-out means one mistake
exposes everything, and a policy that comes from the hub *process* changes
whenever a different session wins the election (the same class as the auth
handover bug, unified-listener §6). So writing becomes opt-in, the
default can only be loosened by the mount's own owner, and any handover can
only make things stricter.

## 2. The value

`--writable=VALUE` (alias `--writeable`):

| value | can save | notes |
|---|---|---|
| `no` | nothing | **default**; no Edit link, `POST` save → 403 |
| `md` | markdown files | exactly what the editor handled before this change |
| `yes` | every class the editor supports | today that is `md`; it grows when the editor does, `md` won't |

- `--readonly` stays as an explicit alias for `--writable=no`. Combining it
  with any other `--writable` value is an error, never a silent pick.
- **Unknown values fail closed.** On the command line they are an error that
  lists the valid values. In a run record or pin file (perhaps written by a
  newer glow-web, e.g. a future `glob:docs/**`) they mean `no`, and the hub
  index shows the raw value with "(read-only: unrecognized)".
- **Room to grow:** the grammar is reserved for comma-separated terms, e.g.
  `md,glob:notes/**` or `user:ann`, once auth identities and path filters
  exist. This build accepts exactly one of `no|md|yes`.

## 3. Who decides, per mount kind

| mount | policy source | default |
|---|---|---|
| standalone `web PATH` | its `--writable` | `no` |
| `web a/ b/`, `hub a/ b/` (arg roots) | that process's `--writable` | `no` |
| pin | `glow-web publish PATH --writable=V` → file `<state>/writable/<name>` holding `V` | `no` (no file) |
| lease | the session's `--writable`, in its run record (`"writable"`; lease records are **contract 2**, so pre-policy hubs refuse rather than serve them writable) | `no` |
| proxy | the session serves itself with its own `--writable` | `no` |

- A hub never applies its *own* `--writable` to pins or leases, only to its
  arg roots. An elected session hub therefore can't widen anyone's mounts.
- `<state>/writable/` is the visible-attenuation surface:
  `ls ~/.local/state/glow-web/writable/` lists exactly which pins accept
  writes. `rm` reverts one to read-only; `unpublish` removes it with the pin;
  `publish --writable=no` deletes it.
- `glow-web ls` and the hub index show each mount's effective policy.

## 4. Behaviour change

`glow-web web .` is read-only by default (breaking: pre-1.0, minor). Use
`--writable=md` (or `yes`) to get the edit page back. lmux spawns glow-web
without flags, so its per-project servers become read-only too until lmux
passes a value.

## 5. Tests

- policy: parse `no|md|yes`; unknown and empty are errors on the CLI; the
  lenient parse (records, pin files) maps unknown to `no`.
- web: a zero-value Server is read-only (save 403, no Edit link); `md`
  allows md saves and shows Edit.
- registry: `publish --writable` writes and removes `writable/<name>`;
  `unpublish` removes it; Pins report it.
- hub: pins default read-only; a pin's file grants; a lease's record grants;
  a hub Configure that sets `yes` does **not** widen pins or leases; an
  unknown record value is read-only.
- cli: `--readonly` with `--writable=md` is an error.
