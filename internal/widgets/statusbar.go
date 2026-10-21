package widgets

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// StatusBarState holds the data needed to render the status bar.
type StatusBarState struct {
	ActiveOp   string // empty when idle
	Pending    int    // total pending across all lanes
	LastResult string // most recent completed/failed result
}

// RenderStatusBar renders one line showing active operation, pending count,
// and last result. When idle, shows "Idle" with total pending count.
func RenderStatusBar(width int, state StatusBarState) string {
	var parts []string

	if state.ActiveOp != "" {
		parts = append(parts, theme.FocusedButtonStyle().Render("▶ "+state.ActiveOp))
	} else {
		parts = append(parts, theme.DimmedStyle().Render("Idle"))
	}

	if state.Pending > 0 {
		parts = append(parts, theme.SubtleStyle().Render(fmt.Sprintf("%d queued", state.Pending)))
	}

	if state.LastResult != "" {
		parts = append(parts, theme.SubtleStyle().Render("• "+state.LastResult))
	}

	return lipgloss.NewStyle().
		Width(max(width-6, 10)).
		Align(lipgloss.Left).
		Render(strings.Join(parts, "  |  "))
}
