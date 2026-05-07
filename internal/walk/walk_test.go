package walk_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mcint/glow-web/internal/walk"
)

func TestFiles_FindsAllMarkdownRecursively(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "a")
	mustMkdir(t, filepath.Join(dir, "sub", "deep"))
	mustWrite(t, filepath.Join(dir, "sub", "b.md"), "b")
	mustWrite(t, filepath.Join(dir, "sub", "deep", "c.md"), "c")
	mustWrite(t, filepath.Join(dir, "ignore-me.txt"), "skip")

	files, err := walk.Files(walk.Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(files); !equal(got, []string{"a.md", "sub/b.md", "sub/deep/c.md"}) {
		t.Errorf("got %v", got)
	}
}

func TestFiles_GitignoreExcludesPatterns(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.md\nbuild/\n")
	mustWrite(t, filepath.Join(dir, "keep.md"), "keep")
	mustWrite(t, filepath.Join(dir, "ignored.md"), "no")
	mustMkdir(t, filepath.Join(dir, "build"))
	mustWrite(t, filepath.Join(dir, "build", "out.md"), "out")
	mustWrite(t, filepath.Join(dir, "src", "ok.md"), "ok") // mkdir implicit via Write helper? no, we MkdirAll inline
	mustMkdir(t, filepath.Join(dir, "src"))
	mustWrite(t, filepath.Join(dir, "src", "ok.md"), "ok")

	files, err := walk.Files(walk.Options{Root: dir, Gitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(files); !equal(got, []string{"keep.md", "src/ok.md"}) {
		t.Errorf("got %v", got)
	}
}

func TestFiles_GitDirImplicitlyIgnored(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git", "objects"))
	mustWrite(t, filepath.Join(dir, ".git", "leak.md"), "leak")
	mustWrite(t, filepath.Join(dir, ".git", "objects", "deep.md"), "leak2")
	mustWrite(t, filepath.Join(dir, "ok.md"), "ok")

	files, err := walk.Files(walk.Options{Root: dir, Gitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(files); !equal(got, []string{"ok.md"}) {
		t.Errorf("expected only ok.md, got %v", got)
	}
}

func TestFiles_GitignoreOffIncludesEverything(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "a.md\n")
	mustWrite(t, filepath.Join(dir, "a.md"), "a")
	mustWrite(t, filepath.Join(dir, "b.md"), "b")

	files, _ := walk.Files(walk.Options{Root: dir, Gitignore: false})
	if got := relPaths(files); !equal(got, []string{"a.md", "b.md"}) {
		t.Errorf("got %v", got)
	}
}

func TestFiles_ExtraIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".rgignore"), "secret.md\n")
	mustWrite(t, filepath.Join(dir, "secret.md"), "shh")
	mustWrite(t, filepath.Join(dir, "public.md"), "ok")

	files, err := walk.Files(walk.Options{
		Root:        dir,
		IgnoreFiles: []string{".rgignore"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := relPaths(files); !equal(got, []string{"public.md"}) {
		t.Errorf("got %v", got)
	}
}

func TestFiles_CustomExtensions(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.markdown"), "a")
	mustWrite(t, filepath.Join(dir, "b.md"), "b")
	mustWrite(t, filepath.Join(dir, "c.txt"), "c")

	files, _ := walk.Files(walk.Options{Root: dir, Extensions: []string{".markdown"}})
	if got := relPaths(files); !equal(got, []string{"a.markdown"}) {
		t.Errorf("got %v", got)
	}
}

// helpers

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func relPaths(files []walk.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Rel
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
