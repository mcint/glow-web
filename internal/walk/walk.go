// Package walk discovers markdown files under a root directory, applying
// optional gitignore-style filtering. The output drives the web index and is
// also the security boundary for serving file content: requests for files
// not in the walk result are 404'd.
package walk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ignore "github.com/sabhiram/go-gitignore"
)

// File describes one discovered markdown file.
type File struct {
	Abs     string    // absolute path on disk
	Rel     string    // path relative to the walk root (forward slashes)
	Name    string    // filepath.Base(Abs)
	Size    int64     // byte size
	ModTime time.Time // last modification time
}

// Options controls the walk.
type Options struct {
	// Root is the directory to walk. Required. Should be absolute for
	// stable Rel values across calls.
	Root string

	// Extensions is the case-insensitive set of file extensions to include
	// (e.g. ".md", ".markdown"). When empty, defaults to {".md", ".markdown"}.
	Extensions []string

	// Gitignore enables reading <Root>/.gitignore and implicitly excluding
	// the .git directory.
	Gitignore bool

	// IgnoreFiles names additional ignore-style files to read, relative to
	// Root or absolute. Useful for .ignore (ripgrep), custom .glowignore,
	// etc. Files that don't exist are silently skipped.
	IgnoreFiles []string

	// ExtraLines is appended to the assembled ignore patterns. Use it to
	// inject patterns programmatically without writing a file.
	ExtraLines []string
}

// Files walks Root and returns every markdown file that survives filtering,
// sorted by Rel for stable output. Returned Rel paths use forward slashes
// regardless of OS.
func Files(opts Options) ([]File, error) {
	if opts.Root == "" {
		return nil, errors.New("walk: Root is required")
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}

	exts := opts.Extensions
	if len(exts) == 0 {
		exts = []string{".md", ".markdown"}
	}
	for i, e := range exts {
		exts[i] = strings.ToLower(e)
	}

	ig, err := buildIgnore(root, opts)
	if err != nil {
		return nil, err
	}

	var out []File
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slashRel := filepath.ToSlash(rel)

		if d.IsDir() {
			if ig != nil && (ig.MatchesPath(slashRel) || ig.MatchesPath(slashRel+"/")) {
				return fs.SkipDir
			}
			return nil
		}
		if ig != nil && ig.MatchesPath(slashRel) {
			return nil
		}
		if !hasExt(d.Name(), exts) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, File{
			Abs:     path,
			Rel:     slashRel,
			Name:    d.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

func buildIgnore(root string, opts Options) (*ignore.GitIgnore, error) {
	var lines []string

	if opts.Gitignore {
		if data, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil {
			lines = append(lines, splitLines(data)...)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		// Always exclude the .git directory itself when gitignore is on.
		lines = append(lines, ".git/", ".git")
	}

	for _, p := range opts.IgnoreFiles {
		path := p
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		lines = append(lines, splitLines(data)...)
	}

	lines = append(lines, opts.ExtraLines...)
	if len(lines) == 0 {
		return nil, nil
	}
	return ignore.CompileIgnoreLines(lines...), nil
}

func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(s, "\n")
}

func hasExt(name string, lowerExts []string) bool {
	n := strings.ToLower(name)
	for _, e := range lowerExts {
		if strings.HasSuffix(n, e) {
			return true
		}
	}
	return false
}
