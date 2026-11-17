// Package policy holds glow-web's write policy (specs/write-policy.md):
// what a mount lets a browser save. The zero value is read-only.
package policy

import "fmt"

// Write is a mount's write policy.
type Write uint8

const (
	No  Write = iota // the zero value: nothing is writable
	MD               // markdown files
	Yes              // every file class the editor supports (today: md)
)

// Values is the accepted CLI grammar, for messages and --help.
const Values = "no|md|yes"

// Parse reads a CLI value strictly: anything but no|md|yes is an error.
// The grammar is reserved for comma-separated terms later (path filters,
// per-user rules), so it isn't widened now.
func Parse(s string) (Write, error) {
	switch s {
	case "no":
		return No, nil
	case "md":
		return MD, nil
	case "yes":
		return Yes, nil
	}
	return No, fmt.Errorf("invalid write policy %q: want %s", s, Values)
}

// Lenient reads a stored value (a run record or pin file, possibly written
// by a newer glow-web). It fails closed: anything unrecognized is No.
func Lenient(s string) Write {
	w, err := Parse(s)
	if err != nil {
		return No
	}
	return w
}

// Allows reports whether a file of the given class (walk class, e.g. "md")
// may be saved.
func (w Write) Allows(class string) bool {
	switch w {
	case Yes:
		return class == "md" // every class the editor supports; today only md
	case MD:
		return class == "md"
	}
	return false
}

func (w Write) String() string {
	switch w {
	case MD:
		return "md"
	case Yes:
		return "yes"
	}
	return "no"
}
