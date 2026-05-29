// Package version reports the running glow-web build's version.
//
// It is the single, project-global source of truth for the version string:
// the CLI banner, `glow-web version`, and the page footer (templates' .Version)
// all call String(). New surfaces should call String() rather than re-deriving.
//
// Resolution order:
//   - build-injected `git describe` (Makefile / release builds) — carries the
//     nearest tag, commits-since, short-sha, and ".dirty";
//   - the module version when built via `go install …@vX.Y.Z`;
//   - a development fallback suffixed with the short VCS revision (and
//     ".dirty") for plain `go build` / `go run`.
package version

import "runtime/debug"

// injected is the authoritative version when the binary was built through the
// project build (Makefile / release), via
//
//	-ldflags "-X github.com/mcint/glow-web/internal/version.injected=$(git describe --tags --always --dirty)"
//
// e.g. "v0.6.4" on a tag or "v0.6.4-3-gabc1234-dirty" three commits later.
// Empty for plain `go build` / `go run`, which fall back to the logic below.
var injected string

// Fallback is the development line shown for plain `go build` / `go run` builds
// that carry neither an injected value nor a module version (go reports
// "(devel)"). Bump on release so dev builds keep reporting the current minor.
const Fallback = "v0.6.4-dev"

// String returns the human-facing version (see package doc for resolution).
func String() string {
	if injected != "" {
		return injected
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Fallback
	}
	v := info.Main.Version
	// Tagged or pseudo-versioned builds already carry full identifying info
	// (including +dirty) — don't double-suffix.
	if v != "" && v != "(devel)" {
		return v
	}
	v = Fallback
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				rev = s.Value[:7]
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev != "" {
		v += "+" + rev
		if dirty {
			v += ".dirty"
		}
	}
	return v
}
