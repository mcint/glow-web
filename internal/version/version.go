// Package version reports the running glow-web build's version.
//
// The string surfaces the module version when the binary was built from a
// tagged release; for `go build` / `go run` development builds it falls back
// to a constant suffixed with the short VCS revision (and ".dirty" if the
// working tree had uncommitted changes at build time).
package version

import "runtime/debug"

// Fallback is used when build info is unavailable or the module version is
// the synthetic "(devel)" string that go injects for non-tagged builds.
const Fallback = "v0.1.0-dev"

// String returns the human-facing version, e.g. "v0.1.0-dev+8e9359c.dirty".
func String() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Fallback
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		v = Fallback
	}
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
