package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderNextActionColumnAligned: a "—" next action (multi-byte, one
// cell wide) must not shift the columns after it (#351; same bug class
// as #325).
func TestRenderNextActionColumnAligned(t *testing.T) {
	l := NewLifecycleList([]LifecycleEntry{
		{Name: "aaa", Lifecycle: "Built", NextAction: "Confirm", Available: "x", Risk: "LOW"},
		{Name: "bbb", Lifecycle: "Built", NextAction: "—", Available: "x", Risk: "LOW"},
	})
	lines := strings.Split(strings.TrimRight(l.Render(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 rows, got:\n%s", l.Render())
	}
	// Display column at which the Available value ("x") starts.
	col := func(line string) int {
		i := strings.Index(line, " x ")
		if i < 0 {
			t.Fatalf("Available value not found in %q", line)
		}
		return lipgloss.Width(line[:i])
	}
	if a, b := col(lines[0]), col(lines[1]); a != b {
		t.Fatalf("Available column starts at %d vs %d:\n%s\n%s", a, b, lines[0], lines[1])
	}
}
