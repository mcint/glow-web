package gitstatus_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mcint/glow-web/internal/gitstatus"
)

func TestStatus_NonGitDirReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "hi")

	m, err := gitstatus.Status(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("non-git dir should yield empty map, got %v", m)
	}
}

func TestStatus_CleanRepo(t *testing.T) {
	skipIfNoGit(t)
	dir := initRepo(t)
	mustWrite(t, filepath.Join(dir, "a.md"), "hi\n")
	mustGit(t, dir, "add", "a.md")
	mustGit(t, dir, "commit", "-m", "init")

	m, _ := gitstatus.Status(dir)
	if len(m) != 0 {
		t.Errorf("clean repo should have no entries, got %v", m)
	}
}

func TestStatus_ModifiedFileShowsWorktreeLetterAndNumstat(t *testing.T) {
	skipIfNoGit(t)
	dir := initRepo(t)
	mustWrite(t, filepath.Join(dir, "a.md"), "one\ntwo\nthree\n")
	mustGit(t, dir, "add", "a.md")
	mustGit(t, dir, "commit", "-m", "init")
	// Append two lines, delete none — numstat should report +2 -0 against HEAD.
	mustWrite(t, filepath.Join(dir, "a.md"), "one\ntwo\nthree\nfour\nfive\n")

	m, _ := gitstatus.Status(dir)
	e, ok := m["a.md"]
	if !ok {
		t.Fatalf("a.md missing from status, have %v", m)
	}
	if e.WorktreeLetter != 'M' || e.IndexLetter != ' ' {
		t.Errorf("a.md XY = %q, want \" M\"", e.XY())
	}
	if e.Adds != 2 || e.Dels != 0 {
		t.Errorf("a.md numstat = +%d -%d, want +2 -0", e.Adds, e.Dels)
	}
}

func TestStatus_UntrackedFileShowsQuestionMarks(t *testing.T) {
	skipIfNoGit(t)
	dir := initRepo(t)
	mustWrite(t, filepath.Join(dir, "seed.md"), "seed\n")
	mustGit(t, dir, "add", "seed.md")
	mustGit(t, dir, "commit", "-m", "seed")
	mustWrite(t, filepath.Join(dir, "new.md"), "new\n")

	m, _ := gitstatus.Status(dir)
	e, ok := m["new.md"]
	if !ok {
		t.Fatalf("new.md missing from status, have %v", m)
	}
	if e.XY() != "??" {
		t.Errorf("new.md XY = %q, want \"??\"", e.XY())
	}
}

func TestStatus_StagedFileShowsIndexLetter(t *testing.T) {
	skipIfNoGit(t)
	dir := initRepo(t)
	mustWrite(t, filepath.Join(dir, "seed.md"), "seed\n")
	mustGit(t, dir, "add", "seed.md")
	mustGit(t, dir, "commit", "-m", "seed")
	// Modify and stage — index letter should be M, worktree should be space.
	mustWrite(t, filepath.Join(dir, "seed.md"), "seed updated\n")
	mustGit(t, dir, "add", "seed.md")

	m, _ := gitstatus.Status(dir)
	e := m["seed.md"]
	if e.IndexLetter != 'M' || e.WorktreeLetter != ' ' {
		t.Errorf("seed.md XY = %q, want \"M \"", e.XY())
	}
	// Numstat against HEAD: +1 line ("seed updated") -1 line ("seed").
	if e.Adds != 1 || e.Dels != 1 {
		t.Errorf("seed.md numstat = +%d -%d, want +1 -1", e.Adds, e.Dels)
	}
}

// helpers

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Configure identity so commit doesn't fail in sandboxed CI.
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
