// Package sanitize provides text-cleaning helpers for embedding raw command
// output into user-facing error messages and logs.
package sanitize

import (
	"strings"
	"unicode"
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
