package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"
)

// HTMLMarkup renders markdown source as a hybrid view: the markup characters
// stay visible (wrapped in low-contrast spans) while the surrounding text gets
// the styled rendering you'd expect — so a `## Goals` line shows a muted `##`
// next to a styled "Goals". Think Obsidian's live-preview but server-rendered.
//
// This v0 covers ATX headings, code fences (verbatim), blockquotes, list
// markers, and inline bold / italic / code / links. Setext headings, tables,
// and reference-style links fall through as plain paragraphs — fine for the
// "show me the source visually" goal but not a full goldmark replacement.
func HTMLMarkup(src []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<div class="md-markup">`)

	inFence := false
	for _, line := range strings.Split(string(src), "\n") {
		switch {
		case inFence && fenceCloseRe.MatchString(line):
			writeMarkerLine(&buf, "md-fence", line)
			inFence = false
		case inFence:
			writeMarkerLine(&buf, "md-code", line)
		case fenceOpenRe.MatchString(line):
			writeMarkerLine(&buf, "md-fence", line)
			inFence = true
		default:
			renderProse(&buf, line)
		}
	}
	buf.WriteString(`</div>`)
	return buf.Bytes()
}

var (
	fenceOpenRe  = regexp.MustCompile("^```")
	fenceCloseRe = regexp.MustCompile("^```\\s*$")
	headingRe    = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bqRe         = regexp.MustCompile(`^(>+\s?)(.*)$`)
	listRe       = regexp.MustCompile(`^(\s*(?:[-*+]|\d+\.)\s+)(.*)$`)
)

func renderProse(buf *bytes.Buffer, line string) {
	if line == "" {
		buf.WriteString(`<div class="md-line md-blank"> </div>`)
		return
	}
	if m := headingRe.FindStringSubmatch(line); m != nil {
		level := len(m[1])
		buf.WriteString(`<div class="md-line md-h`)
		buf.WriteByte(byte('0' + level))
		buf.WriteString(`"><span class="md-marker">`)
		buf.WriteString(html.EscapeString(m[1]))
		buf.WriteString(` </span><span class="md-text">`)
		buf.WriteString(renderInline(m[2]))
		buf.WriteString(`</span></div>`)
		return
	}
	if m := bqRe.FindStringSubmatch(line); m != nil {
		buf.WriteString(`<div class="md-line md-bq"><span class="md-marker">`)
		buf.WriteString(html.EscapeString(m[1]))
		buf.WriteString(`</span><span class="md-text">`)
		buf.WriteString(renderInline(m[2]))
		buf.WriteString(`</span></div>`)
		return
	}
	if m := listRe.FindStringSubmatch(line); m != nil {
		buf.WriteString(`<div class="md-line md-li"><span class="md-marker">`)
		buf.WriteString(html.EscapeString(m[1]))
		buf.WriteString(`</span><span class="md-text">`)
		buf.WriteString(renderInline(m[2]))
		buf.WriteString(`</span></div>`)
		return
	}
	buf.WriteString(`<div class="md-line md-p"><span class="md-text">`)
	buf.WriteString(renderInline(line))
	buf.WriteString(`</span></div>`)
}

func writeMarkerLine(buf *bytes.Buffer, cls, line string) {
	buf.WriteString(`<div class="md-line `)
	buf.WriteString(cls)
	buf.WriteString(`"><span class="md-marker">`)
	if line == "" {
		buf.WriteString(" ")
	} else {
		buf.WriteString(html.EscapeString(line))
	}
	buf.WriteString(`</span></div>`)
}

// renderInline tokenises a single line for bold/italic/code/links. We
// intentionally avoid regex-replace-on-already-escaped-HTML (which corrupts
// nested markers) — instead we walk the raw bytes once and emit HTML directly.
func renderInline(s string) string {
	var b strings.Builder
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			b.WriteString(html.EscapeString(plain.String()))
			plain.Reset()
		}
	}

	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '`':
			j := strings.IndexByte(s[i+1:], '`')
			if j < 0 {
				plain.WriteByte(c)
				i++
				continue
			}
			j += i + 1
			flush()
			b.WriteString(`<span class="md-marker">` + "`" + `</span><code>`)
			b.WriteString(html.EscapeString(s[i+1 : j]))
			b.WriteString(`</code><span class="md-marker">` + "`" + `</span>`)
			i = j + 1
		case c == '*' && i+1 < len(s) && s[i+1] == '*':
			j := strings.Index(s[i+2:], "**")
			if j < 0 {
				plain.WriteByte(c)
				i++
				continue
			}
			j += i + 2
			flush()
			b.WriteString(`<span class="md-marker">**</span><strong>`)
			b.WriteString(renderInline(s[i+2 : j]))
			b.WriteString(`</strong><span class="md-marker">**</span>`)
			i = j + 2
		case c == '*':
			j := strings.IndexByte(s[i+1:], '*')
			if j < 0 {
				plain.WriteByte(c)
				i++
				continue
			}
			// Avoid eating a `**` opener: if the closer is immediately followed
			// by another `*`, treat this as not-italic and let bold handle it.
			j += i + 1
			if j+1 < len(s) && s[j+1] == '*' {
				plain.WriteByte(c)
				i++
				continue
			}
			flush()
			b.WriteString(`<span class="md-marker">*</span><em>`)
			b.WriteString(renderInline(s[i+1 : j]))
			b.WriteString(`</em><span class="md-marker">*</span>`)
			i = j + 1
		case c == '[':
			close := strings.IndexByte(s[i:], ']')
			if close < 0 || i+close+1 >= len(s) || s[i+close+1] != '(' {
				plain.WriteByte(c)
				i++
				continue
			}
			urlStart := i + close + 2
			urlOff := strings.IndexByte(s[urlStart:], ')')
			if urlOff < 0 {
				plain.WriteByte(c)
				i++
				continue
			}
			urlEnd := urlStart + urlOff
			text := s[i+1 : i+close]
			urlStr := s[urlStart:urlEnd]
			flush()
			b.WriteString(`<span class="md-marker">[</span><a href="`)
			b.WriteString(html.EscapeString(urlStr))
			b.WriteString(`">`)
			b.WriteString(renderInline(text))
			b.WriteString(`</a><span class="md-marker">](</span><span class="md-link-url">`)
			b.WriteString(html.EscapeString(urlStr))
			b.WriteString(`</span><span class="md-marker">)</span>`)
			i = urlEnd + 1
		default:
			plain.WriteByte(c)
			i++
		}
	}
	flush()
	return b.String()
}
