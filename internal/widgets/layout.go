package widgets

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// MinFlexHeight is the floor for a screen's flexible section.
const MinFlexHeight = 6

// FlexHeight returns the rows left for a screen's one flexible section:
// total minus the section-box frame minus the measured height of every
// fixed part. Blank lines must be passed as "" entries.
func FlexHeight(total int, fixed ...string) int {
	occupied := theme.SectionBorderStyle().GetVerticalFrameSize()
	for _, s := range fixed {
		occupied += lipgloss.Height(s)
	}
	return max(total-occupied, MinFlexHeight)
}
