// Package slice implements semantic operations over the markdown AST: the
// "jq for markdown" that powers `glow-web slice`. Operations return original
// source bytes wherever possible so output is itself valid markdown.
package slice

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/mcint/glow-web/internal/mdcore"
)

// BySection returns the markdown source under the section identified by
// path, where path is an ordered list of heading texts from outermost to
// innermost (e.g. {"Architecture", "Slicing"}). Matching is case-insensitive
// on trimmed heading text. The returned bytes start at the matched heading
// line and end immediately before the next heading whose level is less than
// or equal to the matched heading's level (or at EOF).
//
// An empty path returns src unchanged. ok is false if no section matches.
func BySection(src []byte, path []string) (out []byte, ok bool) {
	if len(path) == 0 {
		return src, true
	}
	headings := collectHeadings(src)
	matchIdx := matchPath(headings, path)
	if matchIdx < 0 {
		return nil, false
	}
	matched := headings[matchIdx]
	end := len(src)
	for j := matchIdx + 1; j < len(headings); j++ {
		if headings[j].level <= matched.level {
			end = headings[j].start
			break
		}
	}
	return src[matched.start:end], true
}

type heading struct {
	level int
	text  string
	start int // byte offset in src of the line containing this heading
}

func collectHeadings(src []byte) []heading {
	var out []heading
	doc := mdcore.Parse(src)
	_ = mdcore.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		hd, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		lines := hd.Lines()
		if lines.Len() == 0 {
			return ast.WalkContinue, nil
		}
		seg := lines.At(0)
		out = append(out, heading{
			level: hd.Level,
			text:  string(hd.Text(src)),
			start: lineStart(src, seg.Start),
		})
		return ast.WalkContinue, nil
	})
	return out
}

func matchPath(headings []heading, path []string) int {
	var active []int // indices of headings matched so far (one per path step)
	for i, h := range headings {
		// Close any matched ancestors whose section ends at or before this
		// heading (sibling or shallower).
		for len(active) > 0 && h.level <= headings[active[len(active)-1]].level {
			active = active[:len(active)-1]
		}
		next := len(active)
		if next >= len(path) {
			continue
		}
		if eqHeading(h.text, path[next]) {
			active = append(active, i)
			if len(active) == len(path) {
				return i
			}
		}
	}
	return -1
}

func eqHeading(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func lineStart(src []byte, off int) int {
	if off > len(src) {
		off = len(src)
	}
	i := bytes.LastIndexByte(src[:off], '\n')
	if i < 0 {
		return 0
	}
	return i + 1
}
