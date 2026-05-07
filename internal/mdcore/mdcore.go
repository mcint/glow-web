// Package mdcore wires goldmark with the project's chosen extensions and
// exposes a single Parse entrypoint shared by every output adapter
// (web/tui/slice). All consumers operate on the same AST.
package mdcore

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// New returns a goldmark.Markdown configured with the project's standard
// extension set. Callers needing renderer output should use the returned
// Markdown directly; callers needing AST access should use Parse.
func New() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)
}

// Parse returns the document AST node for src. The reader is retained on the
// returned context-free node via positions; callers must keep src alive while
// they walk the tree (goldmark stores byte segments referencing the source).
func Parse(src []byte) ast.Node {
	p := New().Parser()
	return p.Parse(text.NewReader(src))
}

// Walk is a thin convenience over ast.Walk that ignores the WalkStatus return
// path most callers don't care about. It still propagates errors.
func Walk(n ast.Node, fn func(n ast.Node, entering bool) (ast.WalkStatus, error)) error {
	return ast.Walk(n, fn)
}

