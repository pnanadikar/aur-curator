// Package sanitize provides text-cleaning helpers for embedding raw command
// output into user-facing error messages and logs.
package sanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ForLogDisplay cleans build-log text for safe display in a viewport.
//
// It preserves line breaks, strips carriage returns (taking the last segment
// when a line contains embedded \r — the terminal's progress-bar redraw
// pattern), removes control characters (except tabs), and leaves all other
// runes, including non-ASCII, untouched. Invalid UTF-8 is replaced with U+FFFD.
func ForLogDisplay(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		line = stripCR(line)
		b.WriteString(stripControl(line))
	}
	return b.String()
}

// stripCR removes all trailing \r and, if \r remains inside the line,
// returns only the text after the last \r.
func stripCR(s string) string {
	s = strings.TrimRight(s, "\r")
	if idx := strings.LastIndex(s, "\r"); idx != -1 {
		return s[idx+1:]
	}
	return s
}

// stripControl removes every rune for which unicode.IsControl is true,
// except '\t' (tab), which is preserved.
func stripControl(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\t' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ForMessage strips control characters, collapses newlines to spaces, and
// truncates to the last 200 runes for display in UI components and logs.
// Truncating from the tail preserves the most diagnostically valuable content,
// since build and command errors conventionally appear at the end of stderr.
func ForMessage(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if unicode.IsControl(r) {
			if r == '\n' || r == '\r' {
				if b.Len() > 0 && b.String()[b.Len()-1] != ' ' {
					b.WriteByte(' ')
				}
			}
			continue
		}
		b.WriteRune(r)
	}

	cleaned := strings.TrimSpace(b.String())

	runes := []rune(cleaned)
	if len(runes) > 200 {
		var sb strings.Builder
		sb.WriteString("...")
		for i := len(runes) - 197; i < len(runes); i++ {
			sb.WriteRune(runes[i])
		}
		return sb.String()
	}
	return cleaned
}

// Segment is a run of text as ForReview splits it. An Escaped segment is
// the visible escape of one character that must not reach the terminal
// raw; the caller shows it in the error style (#491).
type Segment struct {
	Text    string
	Escaped bool
}

// ForReview splits text the AUR maintainer wrote into plain and escaped
// segments for the review screens (tui-review.md § Escaped Characters,
// #491). Unlike ForLogDisplay it removes nothing: a hidden character is
// itself worth seeing.
//
// Escaped, one segment per character:
//   - carriage return as \r;
//   - other C0 control characters and DEL as \xNN, and so is each byte
//     that is not valid UTF-8;
//   - C1 control characters, bidi controls and marks, and zero-width
//     characters as \uNNNN.
//
// Tab and newline stay plain (callers split lines first); so does all
// other text, non-ASCII included. Adjacent plain runes form one segment.
func ForReview(s string) []Segment {
	var segs []Segment
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		esc, ok := escapeFor(r, size, s[i])
		if !ok {
			i += size
			continue
		}
		if start < i {
			segs = append(segs, Segment{Text: s[start:i]})
		}
		segs = append(segs, Segment{Text: esc, Escaped: true})
		i += size
		start = i
	}
	if start < len(s) {
		segs = append(segs, Segment{Text: s[start:]})
	}
	return segs
}

// Escape is ForReview's segments joined: the escaped text without any
// styling, for a file (#488's pager) or a width calculation.
func Escape(s string) string {
	var b strings.Builder
	for _, seg := range ForReview(s) {
		b.WriteString(seg.Text)
	}
	return b.String()
}

// HasHidden reports whether s holds any character ForReview escapes.
func HasHidden(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if _, ok := escapeFor(r, size, s[i]); ok {
			return true
		}
		i += size
	}
	return false
}

// escapeFor returns the visible escape for the rune r decoded from size
// bytes starting with b, and whether it needs one.
func escapeFor(r rune, size int, b byte) (string, bool) {
	switch {
	case r == utf8.RuneError && size == 1:
		return fmt.Sprintf(`\x%02x`, b), true
	case r == '\t' || r == '\n':
		return "", false
	case r == '\r':
		return `\r`, true
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\x%02x`, r), true
	case r >= 0x80 && r <= 0x9f, isHidden(r):
		return fmt.Sprintf(`\u%04x`, r), true
	}
	return "", false
}

// isHidden reports the bidi controls and marks and the zero-width
// characters: they reorder or hide text without being control characters
// in the unicode.IsControl sense (Trojan Source, CVE-2021-42574).
func isHidden(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e, // LRE, RLE, PDF, LRO, RLO
		r >= 0x2066 && r <= 0x2069,            // LRI, RLI, FSI, PDI
		r == 0x200e, r == 0x200f, r == 0x061c, // LRM, RLM, ALM
		r >= 0x200b && r <= 0x200d, // ZWSP, ZWNJ, ZWJ
		r == 0x2060, r == 0xfeff:   // WJ, BOM / ZWNBSP
		return true
	}
	return false
}
