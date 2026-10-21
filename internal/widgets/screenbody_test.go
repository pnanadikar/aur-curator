package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/pramod/aur-curator/internal/theme"
)

// #329 — the screen box truncates long lines instead of wrapping them.
func TestWrapScreenBodyNeverWraps(t *testing.T) {

	// Box content width: well above theme.MinScreenWidth, so the test does
	// not depend on its value.
	width := theme.MinScreenWidth + 106
	box := width - 6
	content := strings.Join([]string{
		strings.Repeat("x", box+50),                          // plain, too long
		"\x1b[31m" + strings.Repeat("y", box+50) + "\x1b[0m", // styled, too long
		"short",
	}, "\n")

	out := WrapScreenBody(width, content)

	if got, want := lipgloss.Height(out), lipgloss.Height(content)+2; got != want {
		t.Fatalf("box is %d rows, want %d (content lines + 2 border rows): long lines wrapped", got, want)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != box+2 {
			t.Fatalf("row %d is %d cells wide, want %d", i, w, box+2)
		}
	}
	if !strings.Contains(out, "short") {
		t.Fatal("short line lost")
	}
}
