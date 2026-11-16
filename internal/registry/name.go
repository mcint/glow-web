// Package registry holds the on-disk state shared by glow-web processes on
// one host: project names, the state dir layout, pinned mounts and run
// records. See specs/unified-listener.md.
package registry

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// MaxName is the longest valid name.
const MaxName = 63

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ValidName reports why name can't be a mount name (nil when it can).
// Names are one URL path segment: lowercase, no '/', and never leading
// '_' (reserved for the hub's own routes), '.' or '-'.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid name %q: want 1-%d chars of [a-z0-9._-], starting with [a-z0-9]", name, MaxName)
	}
	return nil
}

// Kebab is lmux's _kebab, kept identical so a project has one name across
// both tools: split CamelCase boundaries with '-', '_' → '-', lowercase,
// collapse repeated '-', trim '-' at the ends.
func Kebab(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prev := r[i-1]
			lowerOrDigitBefore := unicode.IsLower(prev) || unicode.IsDigit(prev)
			acronymEnd := unicode.IsUpper(prev) && i+1 < len(r) && unicode.IsLower(r[i+1])
			if lowerOrDigitBefore || acronymEnd {
				b.WriteByte('-')
			}
		}
		if c == '_' {
			c = '-'
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return strings.Trim(collapseDash(b.String()), "-")
}

// NameFor derives the default mount name of a root: Kebab(basename). Where
// that is not already a valid name (a leading '.', a space, non-ASCII), it
// is made one: other characters become '-', leading [._-] are trimmed and
// the result is cut to MaxName. When nothing usable is left, it returns
// "root".
func NameFor(root string) string {
	n := Kebab(filepath.Base(filepath.Clean(root)))
	if ValidName(n) == nil {
		return n
	}
	var b strings.Builder
	for _, c := range n {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	n = strings.TrimLeft(collapseDash(b.String()), "._-")
	n = strings.TrimRight(n, "-")
	if len(n) > MaxName {
		n = strings.TrimRight(n[:MaxName], "-")
	}
	if ValidName(n) != nil {
		return "root"
	}
	return n
}

func collapseDash(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
