// Package walk discovers files under a root directory, applying optional
// gitignore-style filtering and class-based inclusion (markdown by default;
// optionally text/config and everything else). The walk result drives the
// web index AND is the security allowlist for serving content — callers
// that opt in to non-md inclusion remain responsible for gating *serving*
// on File.Class == "md" (the web server does this in findFile/serveOne).
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

// File describes one discovered file.
type File struct {
	Abs     string    // absolute path on disk
	Rel     string    // path relative to the walk root (forward slashes)
	Name    string    // filepath.Base(Abs)
	Size    int64     // byte size
	ModTime time.Time // last modification time
	Class   string    // "md" | "text" | "other" — see classify()
	Hidden  bool      // any path segment starts with "."
	Ignored bool      // matched a gitignore pattern but kept due to IncludeIgnored
}

// Options controls the walk.
type Options struct {
	// Root is the directory to walk. Required. Should be absolute for
	// stable Rel values across calls.
	Root string

	// Extensions is the case-insensitive set of file extensions counted
	// as the "md" class (e.g. ".md", ".markdown"). When empty, defaults
	// to {".md", ".markdown"}. Files with other extensions are classed
	// as "text" or "other" via the built-in classifier.
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

	// IncludeText keeps "text" class files in the result (configs, source,
	// plain text). Class is set per the built-in classifier.
	IncludeText bool

	// IncludeAll keeps every classified file (including "other"). Implies
	// IncludeText.
	IncludeAll bool

	// IncludeHidden keeps dotfiles and dot-directories. Does not affect
	// the always-excluded .git directory.
	IncludeHidden bool

	// IncludeIgnored bypasses gitignore + IgnoreFiles + ExtraLines for
	// inclusion. Matching entries are still tagged Ignored=true so the UI
	// can mark them. .git/ remains excluded regardless.
	IncludeIgnored bool
}

// Files walks Root and returns every file matching the active class +
// hidden + ignored filters, sorted by Rel for stable output. Returned Rel
// paths use forward slashes regardless of OS.
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

		// .git/ is always pruned regardless of other options — it's huge,
		// noisy, and never useful to surface.
		if d.IsDir() && (d.Name() == ".git") {
			return fs.SkipDir
		}

		hidden := hasHiddenSegment(slashRel)
		if hidden && !opts.IncludeHidden {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		var ignored bool
		if ig != nil {
			if d.IsDir() && (ig.MatchesPath(slashRel) || ig.MatchesPath(slashRel+"/")) {
				if !opts.IncludeIgnored {
					return fs.SkipDir
				}
				ignored = true
			} else if !d.IsDir() && ig.MatchesPath(slashRel) {
				if !opts.IncludeIgnored {
					return nil
				}
				ignored = true
			}
		}

		if d.IsDir() {
			return nil
		}

		class := classify(d.Name(), exts)
		switch class {
		case "md":
			// always kept (subject to hidden/ignored above)
		case "text":
			if !opts.IncludeText && !opts.IncludeAll {
				return nil
			}
		default: // "other"
			if !opts.IncludeAll {
				return nil
			}
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
			Class:   class,
			Hidden:  hidden,
			Ignored: ignored,
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

// textExtensions: the case-insensitive extension set we class as "text".
// Keep deliberately broad but not unbounded — additions are cheap, but the
// classifier should never make a network call or open the file.
var textExtensions = map[string]struct{}{
	".txt": {}, ".text": {},
	".toml": {}, ".yaml": {}, ".yml": {}, ".json": {}, ".jsonc": {},
	".xml": {}, ".html": {}, ".htm": {}, ".css": {}, ".scss": {},
	".js": {}, ".ts": {}, ".jsx": {}, ".tsx": {}, ".mjs": {}, ".cjs": {},
	".go": {}, ".rs": {}, ".py": {}, ".rb": {}, ".sh": {}, ".bash": {},
	".zsh": {}, ".fish": {}, ".pl": {}, ".lua": {}, ".sql": {},
	".c": {}, ".h": {}, ".cc": {}, ".cpp": {}, ".hpp": {}, ".java": {},
	".kt": {}, ".swift": {}, ".m": {}, ".mm": {}, ".php": {},
	".conf": {}, ".cfg": {}, ".ini": {}, ".env": {}, ".properties": {},
	".csv": {}, ".tsv": {}, ".log": {}, ".diff": {}, ".patch": {},
	".dockerfile": {}, ".gitignore": {}, ".gitattributes": {},
	".editorconfig": {}, ".lock": {},
}

// textNames: extensionless filenames we class as "text" by convention.
var textNames = map[string]struct{}{
	"Makefile": {}, "Dockerfile": {}, "Rakefile": {}, "Gemfile": {},
	"LICENSE": {}, "COPYING": {}, "NOTICE": {}, "README": {},
	"CHANGELOG": {}, "AUTHORS": {}, "CONTRIBUTORS": {}, "TODO": {},
	".gitignore": {}, ".gitattributes": {}, ".editorconfig": {},
	".env": {}, ".dockerignore": {},
}

// classify returns the File.Class for a leaf filename. The mdExts argument
// is the configured "md" set so callers can override the default.
func classify(name string, mdExts []string) string {
	if hasExt(name, mdExts) {
		return "md"
	}
	if _, ok := textNames[name]; ok {
		return "text"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := textExtensions[ext]; ok {
		return "text"
	}
	return "other"
}

// hasHiddenSegment reports whether any segment of a slash-separated path
// begins with ".". A leaf dotfile counts; so does a file nested under a
// dot-directory like ".github/workflows/ci.yml".
func hasHiddenSegment(slashRel string) bool {
	for _, seg := range strings.Split(slashRel, "/") {
		if seg != "" && seg[0] == '.' {
			return true
		}
	}
	return false
}
