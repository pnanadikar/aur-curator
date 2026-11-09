package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/sanitize"
)

// RenderEscaped renders one line of text the AUR maintainer wrote: plain
// runs in base, and each control, bidi or zero-width character as its
// visible escape in the error style, so nothing reaches the terminal raw
// (tui-review.md § Escaped Characters, #491). Each segment is styled on
// its own: styling the whole line and then part of it again would end the
// line's style at the first escape.
func RenderEscaped(s string, base lipgloss.Style) string {
	segs := sanitize.ForReview(s)
	if len(segs) == 0 {
		return base.Render(s)
	}
	var b strings.Builder
	for _, seg := range segs {
		if seg.Escaped {
			b.WriteString(ErrorStyle().Render(seg.Text))
		} else {
			b.WriteString(base.Render(seg.Text))
		}
	}
	return b.String()
}
