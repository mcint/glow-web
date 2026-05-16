// Package gitstatus surfaces a compact git working-tree summary, keyed by
// repo-relative forward-slash paths, for the web index. It shells to the
// git binary (no library dep) and silently returns an empty map when the
// directory isn't a git repo or `git` isn't on PATH — the caller treats
// "no git info" the same as "clean".
package gitstatus

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Entry summarises one file's git state.
type Entry struct {
	// IndexLetter and WorktreeLetter are the two columns of `git status
	// --porcelain=v1` (X and Y). A space byte (0x20) means "unchanged in
	// that column". '?' in both columns means untracked.
	IndexLetter    byte
	WorktreeLetter byte
	// Adds and Dels come from `git diff HEAD --numstat`. Both zero when
	// the file is unchanged vs HEAD or when numstat reports "-" (binary).
	Adds int
	Dels int
}

// XY renders the two-column status code, mirroring `git status --short`.
func (e Entry) XY() string {
	x := byte(' ')
	if e.IndexLetter != 0 {
		x = e.IndexLetter
	}
	y := byte(' ')
	if e.WorktreeLetter != 0 {
		y = e.WorktreeLetter
	}
	return string([]byte{x, y})
}

// Status returns a map of repo-relative path -> Entry. If root isn't a git
// repo, or the `git` binary is missing, returns an empty map and nil error
// so callers can treat the result as "no info" without branching on error.
func Status(root string) (map[string]Entry, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Entry)
	if _, err := exec.LookPath("git"); err != nil {
		return out, nil
	}
	// 2s is plenty for `git status` on any reasonable repo. If it ever
	// isn't, the index render falls back to "no git info" silently.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := loadStatus(ctx, abs, out); err != nil {
		// Not a git repo, or git refused — treat as no info.
		return map[string]Entry{}, nil
	}
	loadNumstat(ctx, abs, out) // best-effort; failures don't blank XY

	return out, nil
}

// loadStatus runs `git status --porcelain=v1 -z` and populates XY codes.
// The -z form is NUL-delimited and emits both the new and (for renames)
// the old path; we keep only the destination.
func loadStatus(ctx context.Context, root string, out map[string]Entry) error {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z")
	data, err := cmd.Output()
	if err != nil {
		return err
	}
	// Each record: "XY <path>\x00", with renames adding a second NUL-terminated
	// "<oldpath>\x00" after. We split on NUL and walk records sequentially.
	parts := strings.Split(string(data), "\x00")
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if len(rec) < 4 {
			continue
		}
		x, y := rec[0], rec[1]
		// rec[2] is the separating space; the path starts at index 3.
		path := rec[3:]
		// Renames (R/C) emit "<new>\x00<old>" — skip the next part (old path).
		if x == 'R' || x == 'C' {
			i++
		}
		out[path] = Entry{IndexLetter: x, WorktreeLetter: y}
	}
	return nil
}

// loadNumstat runs `git diff HEAD --numstat -z` to attach adds/dels. Each
// record is "ADDS\tDELS\t<path>\x00" (NUL terminator) where binary files
// report "-\t-\t…". Failures are silent — XY data from loadStatus stands.
func loadNumstat(ctx context.Context, root string, out map[string]Entry) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "HEAD", "--numstat", "-z")
	data, err := cmd.Output()
	if err != nil {
		return
	}
	for _, rec := range strings.Split(string(data), "\x00") {
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		adds, _ := strconv.Atoi(fields[0])
		dels, _ := strconv.Atoi(fields[1])
		path := fields[2]
		e := out[path]
		e.Adds = adds
		e.Dels = dels
		out[path] = e
	}
}
