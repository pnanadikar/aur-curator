package widgets

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/theme"
)

// LaneStatus represents the visible status of a single lane.
type LaneStatus struct {
	Name     string
	Status   string
	LastLine string
	Pkg      domain.PackageName
	Kind     string // operation kind display label; empty when idle (falls back to Name)
}

// laneVerbs maps an operation kind label to its present-participle verb form
// for use in the running state. Keys are lowercase operation kind strings.
var laneVerbs = map[string]string{
	"fetch":   "fetching",
	"analyze": "analyzing",
	"build":   "building",
	"publish": "publishing",
	"merge":   "merging",
	"install": "installing",
}

// formatLaneState produces the uniform display text for a lane state:
// "[<pkg>] <state text>" where state text varies by status.
// Package name appears exactly once, in brackets, at the start of the line.
// LastLine is used only as the failure reason, not as the full state text.
// When an operation has run on the lane, the display label is derived from
// that operation's kind (e.g. "merge") rather than the lane's generic name
// (e.g. "Fetch"), resolving the ambiguity when multiple operation kinds
// share a lane (OpMerge and OpFetch both map to LaneFetch).
func formatLaneState(ls LaneStatus) string {
	if ls.Pkg == "" {
		return ""
	}
	label := ls.Name
	if ls.Kind != "" {
		label = ls.Kind
	}
	var stateText string
	switch ls.Status {
	case "running":
		stateText = laneVerbs[label] + "\xe2\x80\xa6"
	case "completed":
		stateText = label + " succeeded"
	case "failed":
		if ls.LastLine != "" {
			stateText = ls.LastLine
		} else {
			stateText = label + " failed"
		}
	case "cancelled":
		stateText = label + " cancelled"
	default:
		// idle — no text (caller omits the line)
		return ""
	}
	return fmt.Sprintf("[%s] %s", ls.Pkg, stateText)
}

// RenderOperationsPanel renders a 4-lane operations panel (Fetch/Analyze/
// Build/Publish). Each row shows one lane with its current status and the
// most recent log line. Only the most recent Log entry per lane is shown —
// scrollable history is a follow-up (CMD-056 follow-up).
//
// All four lanes are always rendered, even when Idle. If footer is
// non-empty, it is rendered as a dimmed hint line below the lanes
// (e.g. "^T Cancel" when an operation is active).
func RenderOperationsPanel(lanes [4]LaneStatus, width int, footer string) string {
	var lines []string
	lines = append(lines, theme.HeaderStyle().Render("Operations"))
	lines = append(lines, theme.DimmedStyle().Render(strings.Repeat("─", width-2)))

	for _, ls := range lanes {
		var statusColor lipgloss.Style
		switch ls.Status {
		case "running":
			statusColor = theme.FocusedButtonStyle()
		case "completed":
			statusColor = theme.SubtleStyle()
		case "failed":
			statusColor = theme.ErrorStyle()
		default:
			statusColor = theme.DimmedStyle()
		}

		row := fmt.Sprintf("  %-12s", ls.Name)
		if stateText := formatLaneState(ls); stateText != "" {
			row += " — " + statusColor.Render(stateText)
		}
		lines = append(lines, row)
	}

	if footer != "" {
		lines = append(lines, theme.DimmedStyle().Render(" "+footer))
	}

	return theme.SectionBorderStyle().
		Width(width).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}
