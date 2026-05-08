# Feature toggles — composable, layered, simple

A design memo on how glow-web should let users specify which optional
features are on, as the surface grows beyond the handful of `--flag`s
we have today (`--palette`, `--readonly`, `--markup`, `--gitignore`,
`--url-prefix`, …).

> Status: meta-spec. Captures the design space, picks a tentative path,
> doesn't commit to an implementation yet. Re-read before adding the
> next non-trivial toggle.

## The pressure

Optional features tend to accrete: the palette, default-markup view,
read-only mode, gitignore semantics, link-rewriting, command-K, the
inevitable "live-reload" follow-up, the hypothetical "auth", the soft
link-resolution gate, etc. As that list grows, three forces pull
against each other:

- **Power.** Users want to combine features without restraint.
- **Flexibility.** Different machines, different projects, different
  invocations want different defaults.
- **Simplicity.** No new mental models for someone who just wants to
  serve a directory once.

You can't optimise for all three at one layer. You *can* across
layers.

## Patterns evaluated

### 1. One boolean flag per feature *(today)*

`--palette`, `--readonly`, `--markup`, `--gitignore=false`, …

- *Pros:* discoverable in `--help`, type-safe, idiomatic, zero new
  concepts. Each flag is its own contract.
- *Cons:* cognitive load grows linearly with feature count. Six-flag
  invocations become illegible. No way to share defaults across
  invocations.

### 2. Comma-list of features

`--features=palette,markup` or `--features=-readonly,+palette` for diff
syntax.

- *Pros:* dense in scripts. Composes well when a user has "their set".
- *Cons:* typos silent — `--features=palettte` flips nothing and warns
  nobody. Docs harder. Partial-set semantics ambiguous (does
  `--features=palette` *only* enable palette and disable everything
  else, or does it add palette to the defaults?).

### 3. Named profiles

`--profile=remote-readonly` curates a set of features.

- *Pros:* one-flag UX for common combinations. Users discover them via
  `glow-web profiles`. Easy to share profiles with teams.
- *Cons:* profile authorship is its own product. Users still need an
  escape hatch when a profile is 90% right ("…but I want
  `--no-palette`"). Smells like premature configuration.

### 4. Config file

TOML or YAML, located at `$XDG_CONFIG_HOME/glow-web/config.toml` for
user-wide defaults, or `.glow-web.toml` for project-local overrides.

```toml
[ui]
palette = true
markup  = false

[security]
readonly = false
```

- *Pros:* persistent per-environment defaults, easy to sync via
  dotfiles. Project-local config addresses "this repo's docs are
  source-leaning so default the markup view." Survives across
  invocations.
- *Cons:* introduces a parser dep (TOML is small but non-zero).
  Resolution order needs documenting. Project-local risks being
  committed accidentally — a security/privacy concern if it
  inadvertently flips `--readonly off` for a published doc tree.

### 5. Environment variables

`GLOW_PALETTE=true`, `GLOW_URL_PREFIX=/docs`, …

- *Pros:* zero-dep, integrates with shell rc files and process
  supervisors (systemd, launchd, k8s). Twelve-factor-app friendly.
- *Cons:* implicit — you forget it's set, then debug for ten minutes
  before checking `env`. Poor discoverability in `--help`. Naming
  conventions and bool-coercion (is `0` false?) need pinning.

### 6. Stacked precedence

Compose multiple of the above with a documented order:

> built-in defaults < config file (user) < config file (project) <
> environment variables < command-line flags

- *Pros:* each layer addresses a different scope (system → user →
  project → session → invocation). The user can pick their preferred
  layer per knob.
- *Cons:* "why is feature X on?" needs an introspection answer.
  Without a `glow-web config show` command that prints provenance
  per field, debugging gets surprising fast.

## Recommendation

### Now (status quo, keep)

Stick with flags. Five toggles isn't enough surface to justify a config
system. **What to add**: a test that pins the default value of each
flag so a silent flip during refactoring fails CI. This is the cheapest
hedge against "mystery default change" regressions.

### Soon (when we cross ~8 toggles or first user complains)

Add a TOML config at `$XDG_CONFIG_HOME/glow-web/config.toml`. Field
names mirror flag names verbatim:

```toml
palette  = true
markup   = false
readonly = false
url_prefix = "/docs"
```

Resolution order: **defaults → user config → env (`GLOW_*`) → flags.**
Project-local config (`.glow-web.toml`) is **opt-in** via
`--config=PATH` only — never auto-discovered, because auto-discovery
is the layer where confused-deputy surprises live.

Add `glow-web config` subcommand that prints the resolved config with
per-field provenance:

```
palette  = true        (config: ~/.config/glow-web/config.toml)
markup   = false       (default)
readonly = true        (env: GLOW_READONLY)
addr     = ":9090"     (flag)
```

This single subcommand is the "why is X on?" answer that makes the
layered system safe to use.

### Later, only on demand

Profiles. Don't preempt: only add when actual usage shows a small set
of combinations users keep retyping (and you've measured this, not
guessed). Profiles are the configuration-system equivalent of
premature abstraction — easy to add, hard to remove once any user
relies on a name.

### Always avoid

- **Comma-list `--features` syntax.** Silent typos are a UX hostility.
  If you really want a multi-set knob later, take repeatable flags
  (`--enable=palette --enable=markup`) so each token validates
  individually.
- **Auto-discovered project config without an introspection command.**
  Adds magic without the tool to debug it.
- **Two parsers for two formats.** TOML *or* YAML. Pick one and ship.

## On the principle

The pull is toward "flexible, powerful, simple — pick three." You
can't, at one layer. But *layered* configuration gets close: simple at
any single layer (one flag, one env var, one config field) and
flexible across them.

The architectural cost is a small `config.Load(defaults, files, env,
flags)` that returns *one* struct; everything downstream consumes that
struct, not raw flag values. That single internal contract is what
keeps "more knobs" from compounding into branching spaghetti through
the codebase.

In Go terms:

```go
type Config struct {
    Palette       bool
    Markup        bool
    ReadOnly      bool
    URLPrefix     string
    // …
}

type Source int
const (
    SourceDefault Source = iota
    SourceUserConfig
    SourceProjectConfig
    SourceEnv
    SourceFlag
)

// Provenance: parallel struct mapping each field to the source that
// last set it. Drives `glow-web config`.
type Provenance struct{ Palette, Markup, ReadOnly, URLPrefix Source }

func Load(args []string, env []string) (Config, Provenance, error) { … }
```

That's the contract. The cost is one new package; the reward is that
*every* future toggle plugs into the same machinery without touching
any handler.

## Open questions

- **Project-local config security.** If `.glow-web.toml` in the served
  directory could disable `--readonly`, an attacker who can write
  files could escape the safety knob. Mitigation: project-local config
  is read-only-ish (can't *relax* security flags, only tighten them),
  or simply gated by `--allow-project-config`.
- **Per-document overrides.** Some docs may want
  `default-markup-view: true` set in their frontmatter. That's a
  document-scope config, distinct from server-scope. Worth a separate
  layer if it ever becomes a pattern.
- **Hot-reload of config.** Nice-to-have, not foundational. fsnotify on
  the config file is straightforward; the cost is making the entire
  Server struct goroutine-safe with respect to swap-in updates.
- **Naming convention drift.** `url-prefix` in flags becomes
  `url_prefix` in TOML and `GLOW_URL_PREFIX` in env. Document this
  mapping once, in `glow-web config` help.
