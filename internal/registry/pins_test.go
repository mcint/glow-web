package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/registry"
)

func newState(t *testing.T) registry.State {
	t.Helper()
	st := registry.State{Dir: filepath.Join(t.TempDir(), "glow-web")}
	if err := st.Ensure(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestState_DirFromEnv(t *testing.T) {
	t.Setenv("GLOW_WEB_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got := registry.DefaultState().Dir; got != "/xdg/state/glow-web" {
		t.Errorf("XDG: got %q", got)
	}
	t.Setenv("GLOW_WEB_STATE_DIR", "/explicit")
	if got := registry.DefaultState().Dir; got != "/explicit" {
		t.Errorf("explicit: got %q", got)
	}
	t.Setenv("GLOW_WEB_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	if got := registry.DefaultState().Dir; got != filepath.Join(home, ".local/state/glow-web") {
		t.Errorf("fallback: got %q", got)
	}
}

func TestState_EnsureCreatesPrivateDirs(t *testing.T) {
	st := newState(t)
	for _, d := range []string{st.Dir, st.MountsDir(), st.RunDir()} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s perm = %o, want 700", d, fi.Mode().Perm())
		}
	}
}

func TestState_EnsureRefusesLooseDir(t *testing.T) {
	st := newState(t)
	if err := os.Chmod(st.MountsDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	err := st.Ensure()
	if err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("Ensure on 0777 mounts dir: err = %v, want refusal", err)
	}
}

func TestPublish_ListUnpublish(t *testing.T) {
	st := newState(t)
	root := t.TempDir()

	name, err := st.Publish(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if name != registry.NameFor(root) {
		t.Errorf("default name = %q, want %q", name, registry.NameFor(root))
	}
	// Re-publishing the same root under the same name is a no-op.
	if _, err := st.Publish(root, name); err != nil {
		t.Errorf("idempotent publish: %v", err)
	}

	pins, err := st.Pins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0].Name != name || pins[0].Root != root || !pins[0].Live {
		t.Fatalf("pins = %+v", pins)
	}

	if err := st.Unpublish(name); err != nil {
		t.Fatal(err)
	}
	if pins, _ := st.Pins(); len(pins) != 0 {
		t.Errorf("after unpublish pins = %+v", pins)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("unpublish touched the root: %v", err)
	}
}

func TestPublish_CollisionNamesHolder(t *testing.T) {
	st := newState(t)
	a, b := t.TempDir(), t.TempDir()
	if _, err := st.Publish(a, "docs"); err != nil {
		t.Fatal(err)
	}
	_, err := st.Publish(b, "docs")
	if err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("collision err = %v; want it to name %s and suggest --name", err, a)
	}
}

func TestPublish_RejectsBadNameAndMissingRoot(t *testing.T) {
	st := newState(t)
	if _, err := st.Publish(t.TempDir(), "_hub"); err == nil {
		t.Error("reserved name accepted")
	}
	if _, err := st.Publish("/no/such/dir", "x"); err == nil {
		t.Error("missing root accepted")
	}
}

func TestUnpublish_RefusesNonSymlink(t *testing.T) {
	st := newState(t)
	p := filepath.Join(st.MountsDir(), "real")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := st.Unpublish("real"); err == nil {
		t.Error("unpublish removed a non-symlink")
	}
	if err := st.Unpublish("absent"); err == nil {
		t.Error("unpublish of absent name: want error")
	}
}

func TestPins_DanglingIsNotLive_InvalidNamesSkipped(t *testing.T) {
	st := newState(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Symlink(gone, filepath.Join(st.MountsDir(), "gone")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(st.MountsDir(), "Bad Name")); err != nil {
		t.Fatal(err)
	}
	pins, err := st.Pins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 || pins[0].Name != "gone" || pins[0].Live {
		t.Fatalf("pins = %+v", pins)
	}
}
