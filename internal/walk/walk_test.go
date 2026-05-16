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

// --- classification + include-toggles ---

func TestFiles_ClassDefaultsToMdOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "x")
	mustWrite(t, filepath.Join(dir, "conf.toml"), "x")
	mustWrite(t, filepath.Join(dir, "blob.bin"), "x")

	files, _ := walk.Files(walk.Options{Root: dir})
	if got := relPaths(files); !equal(got, []string{"doc.md"}) {
		t.Errorf("default class filter should keep only md, got %v", got)
	}
	if files[0].Class != "md" {
		t.Errorf("doc.md Class = %q, want md", files[0].Class)
	}
}

func TestFiles_IncludeTextAddsTextClass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "x")
	mustWrite(t, filepath.Join(dir, "conf.toml"), "x")
	mustWrite(t, filepath.Join(dir, "Makefile"), "x")
	mustWrite(t, filepath.Join(dir, "blob.bin"), "x")

	files, _ := walk.Files(walk.Options{Root: dir, IncludeText: true})
	got := relPaths(files)
	want := []string{"Makefile", "conf.toml", "doc.md"}
	if !equal(got, want) {
		t.Errorf("IncludeText: got %v want %v", got, want)
	}
	classes := map[string]string{}
	for _, f := range files {
		classes[f.Rel] = f.Class
	}
	if classes["doc.md"] != "md" || classes["conf.toml"] != "text" || classes["Makefile"] != "text" {
		t.Errorf("unexpected classes: %v", classes)
	}
}

func TestFiles_IncludeAllAddsOtherClass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "x")
	mustWrite(t, filepath.Join(dir, "blob.bin"), "x")

	files, _ := walk.Files(walk.Options{Root: dir, IncludeAll: true})
	if got := relPaths(files); !equal(got, []string{"blob.bin", "doc.md"}) {
		t.Errorf("IncludeAll: got %v", got)
	}
	for _, f := range files {
		if f.Rel == "blob.bin" && f.Class != "other" {
			t.Errorf("blob.bin Class = %q, want other", f.Class)
		}
	}
}

func TestFiles_HiddenExcludedByDefault(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "visible.md"), "x")
	mustWrite(t, filepath.Join(dir, ".hidden.md"), "x")
	mustMkdir(t, filepath.Join(dir, ".dotdir"))
	mustWrite(t, filepath.Join(dir, ".dotdir", "buried.md"), "x")

	files, _ := walk.Files(walk.Options{Root: dir})
	if got := relPaths(files); !equal(got, []string{"visible.md"}) {
		t.Errorf("default should hide dotfiles + dot-dirs, got %v", got)
	}
}

func TestFiles_IncludeHiddenExposesDotfiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "visible.md"), "x")
	mustWrite(t, filepath.Join(dir, ".hidden.md"), "x")
	mustMkdir(t, filepath.Join(dir, ".dotdir"))
	mustWrite(t, filepath.Join(dir, ".dotdir", "buried.md"), "x")

	files, _ := walk.Files(walk.Options{Root: dir, IncludeHidden: true})
	got := relPaths(files)
	want := []string{".dotdir/buried.md", ".hidden.md", "visible.md"}
	if !equal(got, want) {
		t.Errorf("IncludeHidden: got %v want %v", got, want)
	}
	for _, f := range files {
		expectHidden := f.Rel != "visible.md"
		if f.Hidden != expectHidden {
			t.Errorf("%s Hidden = %v, want %v", f.Rel, f.Hidden, expectHidden)
		}
	}
}

func TestFiles_IncludeHiddenStillSkipsDotGit(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".git", "objects"))
	mustWrite(t, filepath.Join(dir, ".git", "leak.md"), "x")
	mustWrite(t, filepath.Join(dir, "ok.md"), "x")

	files, _ := walk.Files(walk.Options{Root: dir, IncludeHidden: true, Gitignore: false})
	if got := relPaths(files); !equal(got, []string{"ok.md"}) {
		t.Errorf(".git must always be pruned, got %v", got)
	}
}

func TestFiles_IncludeIgnoredKeepsAndTagsRows(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "private.md\n")
	mustWrite(t, filepath.Join(dir, "public.md"), "x")
	mustWrite(t, filepath.Join(dir, "private.md"), "x")

	files, _ := walk.Files(walk.Options{Root: dir, Gitignore: true, IncludeIgnored: true})
	if got := relPaths(files); !equal(got, []string{"private.md", "public.md"}) {
		t.Errorf("IncludeIgnored: got %v", got)
	}
	tags := map[string]bool{}
	for _, f := range files {
		tags[f.Rel] = f.Ignored
	}
	if !tags["private.md"] || tags["public.md"] {
		t.Errorf("Ignored tagging wrong: %v", tags)
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
