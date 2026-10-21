package widgets

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// Action is one entry in a screen's action bar (tui-package-details.md
// § Action Bar Convention). Screens build the list and decide
// availability; this package only renders it.
type Action struct {
	Key     string // as shown, e.g. "B", "ENTER", "^PgDn/^PgUp"
	Label   string // short label, e.g. "Build"
	Enabled bool
	Reason  string // why it is unavailable; shown only in the full help
}

// helpAction is the entry every compact bar ends with.
var helpAction = Action{Key: "?", Label: "more", Enabled: true}

// closeHelpAction is the entry every full help ends with.
var closeHelpAction = Action{Key: "?", Label: "Close help", Enabled: true}

const actionGap = "  "

// RenderActionBar renders the compact one-line bar: every action as key +
// label, unavailable ones dimmed with no reason, ending with "? more".
func RenderActionBar(actions []Action) string {
	parts := make([]string, 0, len(actions)+1)
	for _, a := range append(append([]Action(nil), actions...), helpAction) {
		parts = append(parts, actionStyle(a).Render(a.Key+" "+a.Label))
	}
	return strings.Join(parts, actionGap)
}

// RenderActionHelp renders the full help: every action with its key and
// label and, when unavailable, its reason, in as many columns as fit in
// width (filled top to bottom). It ends with "? Close help".
func RenderActionHelp(actions []Action, width int) string {
	all := append(append([]Action(nil), actions...), closeHelpAction)

	keyW := 0
	for _, a := range all {
		keyW = max(keyW, lipgloss.Width(a.Key))
	}
	cells := make([]string, len(all))
	cellW := 0
	for i, a := range all {
		text := a.Key + strings.Repeat(" ", keyW-lipgloss.Width(a.Key)) + "  " + a.Label
		if !a.Enabled && a.Reason != "" {
			text += " — " + a.Reason
		}
		cells[i] = text
		cellW = max(cellW, lipgloss.Width(text))
	}

	colW := cellW + len(actionGap)
	cols := max(1, min(len(all), (width+len(actionGap))/colW))
	rows := (len(all) + cols - 1) / cols

	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var line strings.Builder
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= len(all) {
				break
			}
			if c > 0 {
				line.WriteString(actionGap)
			}
			text := cells[i]
			if c < cols-1 && (c+1)*rows+r < len(all) {
				text += strings.Repeat(" ", cellW-lipgloss.Width(text))
			}
			line.WriteString(actionStyle(all[i]).Render(text))
		}
		// A cell wider than the whole width (single column on a very
		// narrow screen) is cut rather than allowed to overflow.
		lines[r] = lipgloss.NewStyle().MaxWidth(width).Render(line.String())
	}
	return strings.Join(lines, "\n")
}

func actionStyle(a Action) lipgloss.Style {
	if a.Enabled {
		return theme.ActionBarEnabledStyle()
	}
	return theme.ActionBarDimmedStyle()
}
