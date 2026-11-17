package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/mcint/glow-web/internal/policy"
)

// State is the per-user state dir: ~/.local/state/glow-web/ by default.
// Pins (mounts/) and live run records (run/) share it on purpose, so the
// host's whole doc site moves or is wiped as one unit
// (specs/unified-listener.md §7).
type State struct {
	Dir string
}

// DefaultState resolves the dir: $GLOW_WEB_STATE_DIR, else
// $XDG_STATE_HOME/glow-web, else ~/.local/state/glow-web.
func DefaultState() State {
	if d := os.Getenv("GLOW_WEB_STATE_DIR"); d != "" {
		return State{Dir: d}
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return State{Dir: filepath.Join(d, "glow-web")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return State{Dir: filepath.Join(home, ".local", "state", "glow-web")}
}

func (s State) MountsDir() string { return filepath.Join(s.Dir, "mounts") }
func (s State) RunDir() string    { return filepath.Join(s.Dir, "run") }

// WritableDir holds pins' write policies: writable/<name> contains the
// policy value (specs/write-policy.md §3). No file means read-only.
func (s State) WritableDir() string { return filepath.Join(s.Dir, "writable") }

// Ensure creates Dir, mounts/ and run/ at 0700 and refuses any of them
// that is group- or world-writable or owned by another user: anyone who
// can write there can publish a path onto this user's doc site.
func (s State) Ensure() error {
	for _, d := range []string{s.Dir, s.MountsDir(), s.RunDir(), s.WritableDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := checkPrivate(d); err != nil {
			return err
		}
	}
	return nil
}

func checkPrivate(d string) error {
	fi, err := os.Stat(d)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group- or world-writable (%o); refusing to use it (chmod 700 %s)", d, fi.Mode().Perm(), d)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d, not %d; refusing to use it", d, st.Uid, os.Getuid())
	}
	return nil
}

// Pin is a persistent mount: mounts/<name> → root (a symlink).
type Pin struct {
	Name string `json:"name"`
	Root string `json:"root"`
	Live bool   `json:"live"` // the root exists and is a dir or file
	// Writable is the raw policy from writable/<name> ("" = none, i.e.
	// read-only). Readers parse it leniently (unknown → read-only).
	Writable string `json:"writable,omitempty"`
}

// Pins lists mounts/ symlinks with valid names, sorted by name. Entries
// that aren't symlinks or have invalid names are skipped, not errors:
// the dir is hand-editable.
func (s State) Pins() ([]Pin, error) {
	ents, err := os.ReadDir(s.MountsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Pin
	for _, e := range ents {
		if e.Type()&os.ModeSymlink == 0 || ValidName(e.Name()) != nil {
			continue
		}
		target, err := os.Readlink(filepath.Join(s.MountsDir(), e.Name()))
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(s.MountsDir(), target)
		}
		_, statErr := os.Stat(target)
		w, _ := os.ReadFile(filepath.Join(s.WritableDir(), e.Name()))
		out = append(out, Pin{Name: e.Name(), Root: target, Live: statErr == nil, Writable: strings.TrimSpace(string(w))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Publish pins root under name (NameFor(root) when name is ""), and
// returns the name used. Pinning the same root under the same name again
// is a no-op. A name held by another root is refused, naming the holder.
func (s State) Publish(root, name string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	if name == "" {
		name = NameFor(abs)
	}
	if err := ValidName(name); err != nil {
		return "", err
	}
	if err := s.Ensure(); err != nil {
		return "", err
	}
	link := filepath.Join(s.MountsDir(), name)
	if cur, err := os.Readlink(link); err == nil {
		if cur == abs {
			return name, nil
		}
		return "", fmt.Errorf("name %q is already published for %s; pick another with --name", name, cur)
	} else if _, statErr := os.Lstat(link); statErr == nil {
		return "", fmt.Errorf("%s exists and is not a symlink; refusing to replace it", link)
	}
	return name, os.Symlink(abs, link)
}

// Unpublish removes the pin. It removes only a symlink, never what it
// points at, and never a real file or dir someone put in mounts/.
func (s State) Unpublish(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	link := filepath.Join(s.MountsDir(), name)
	fi, err := os.Lstat(link)
	if err != nil {
		return fmt.Errorf("%q is not published", name)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a symlink; refusing to remove it", link)
	}
	if err := os.Remove(link); err != nil {
		return err
	}
	// The policy goes with the pin: a later publish of the same name
	// starts read-only again.
	if err := os.Remove(filepath.Join(s.WritableDir(), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SetPinWritable sets a pin's write policy. "no" removes the file, so the
// default (read-only) needs no file at all. The value is checked strictly.
func (s State) SetPinWritable(name, value string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	w, err := policy.Parse(value)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(s.MountsDir(), name)); err != nil {
		return fmt.Errorf("%q is not published", name)
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	f := filepath.Join(s.WritableDir(), name)
	if w == policy.No {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(f, []byte(w.String()+"\n"), 0o600)
}
