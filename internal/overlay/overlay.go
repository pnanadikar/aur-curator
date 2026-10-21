// Package overlay provides utilities for rendering centered overlay
// content on top of a base string within fixed terminal dimensions.
// Used by multiple screens to composite modals onto their body content.
package overlay

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Render centers an overlay string on top of a base string using the
// given terminal dimensions. It pads each base line to fill the full
// width so the overlay does not shift when centered. The overlay is
// vertically centered within the first `height` lines.
func Render(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(overlayLines) == 0 {
		return base
	}

	lineCount := max(len(baseLines), height)
	lines := make([]string, lineCount)
	for i := range lines {
		if i < len(baseLines) {
			lines[i] = baseLines[i]
		} else {
			lines[i] = ""
		}
	}

	modalHeight := len(overlayLines)
	startRow := max((height-modalHeight)/2, 0)
	for i, line := range overlayLines {
		row := startRow + i
		if row >= len(lines) {
			break
		}
		lineWidth := lipgloss.Width(line)
		leftPad := max((width-lineWidth)/2, 0)
		rightPad := max(width-leftPad-lineWidth, 0)
		lines[row] = strings.Repeat(" ", leftPad) + line + strings.Repeat(" ", rightPad)
	}

	return strings.Join(lines, "\n")
}
