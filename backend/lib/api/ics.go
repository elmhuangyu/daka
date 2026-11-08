package api

import (
	"strings"
	"unicode/utf8"
)

// icsMaxLine is the RFC 5545 content-line limit in octets (excluding CRLF).
const icsMaxLine = 75

// icsText escapes a TEXT value (RFC 5545 3.3.11): backslash, semicolon and
// comma are backslash-escaped, line breaks become \n and other control
// characters are dropped (they could otherwise inject extra content lines).
func icsText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == utf8.RuneError && n == 1:
			b.WriteRune('�')
		case r == '\\':
			b.WriteString(`\\`)
		case r == ';':
			b.WriteString(`\;`)
		case r == ',':
			b.WriteString(`\,`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			// CRLF becomes one \n (via the '\n' case); a lone CR is a line break.
			if i >= len(s) || s[i] != '\n' {
				b.WriteString(`\n`)
			}
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// icsWriter accumulates content lines, folding at 75 octets on rune
// boundaries and terminating each line with CRLF.
type icsWriter struct{ b strings.Builder }

// line writes one content line (already escaped).
func (w *icsWriter) line(s string) {
	limit := icsMaxLine
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = limit
		}
		w.b.WriteString(s[:cut])
		w.b.WriteString("\r\n ")
		s = s[cut:]
		limit = icsMaxLine - 1 // continuation lines start with a space
	}
	w.b.WriteString(s)
	w.b.WriteString("\r\n")
}

func (w *icsWriter) String() string { return w.b.String() }
