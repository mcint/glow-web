// Package render owns the output adapters for the markdown AST: HTML for the
// web view, ANSI for the TUI, and (eventually) JSON for tooling.
package render

import (
	"bytes"

	"github.com/mcint/glow-web/internal/mdcore"
)

// HTML renders src to HTML using the project's standard goldmark configuration.
// The output is the raw <body>-fragment HTML; wrap it in a page template if you
// need a full document.
func HTML(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := mdcore.New().Convert(src, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
