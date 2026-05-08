// Package render owns the output adapters for the markdown AST: HTML for the
// web view, ANSI for the TUI, and (eventually) JSON for tooling.
package render

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/mcint/glow-web/internal/mdcore"
)

// Options configures HTMLWithOptions. The zero value is equivalent to HTML.
type Options struct {
	// LinkResolver, if set, is invoked for every Markdown link's destination.
	// Returning ok=true replaces the destination; ok=false leaves it
	// unchanged. The web server uses this to rewrite intra-project relative
	// links to their served URLs.
	LinkResolver func(href string) (newHref string, ok bool)
}

// HTML renders src to HTML using the project's standard goldmark configuration.
// The output is the raw <body>-fragment HTML; wrap it in a page template if you
// need a full document.
func HTML(src []byte) ([]byte, error) {
	return HTMLWithOptions(src, Options{})
}

// HTMLWithOptions is HTML with a hook for AST-level transforms (currently
// just link rewriting). It parses once, mutates the AST in place if the
// resolver is set, then renders.
func HTMLWithOptions(src []byte, opts Options) ([]byte, error) {
	md := mdcore.New()
	doc := md.Parser().Parse(text.NewReader(src))
	if opts.LinkResolver != nil {
		rewriteLinks(doc, opts.LinkResolver)
	}
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rewriteLinks(doc ast.Node, resolve func(string) (string, bool)) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		link, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		if newHref, ok := resolve(string(link.Destination)); ok {
			link.Destination = []byte(newHref)
		}
		return ast.WalkContinue, nil
	})
}
