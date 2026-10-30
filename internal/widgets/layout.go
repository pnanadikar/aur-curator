package widgets

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// MinFlexHeight is the floor for a screen's flexible section.
const MinFlexHeight = 6

// FlexHeight returns the rows left for a screen's one flexible section,
// never fewer than MinFlexHeight (see FlexRows).
func FlexHeight(total int, fixed ...string) int {
	return max(FlexRows(total, fixed...), MinFlexHeight)
}

// FlexRows returns the rows left for a screen's one flexible section:
// total minus the section-box frame minus the measured height of every
// fixed part, with no floor, so it can be zero or negative when the
// screen is too short. Blank lines must be passed as "" entries. The
// Dashboard uses it to size its page down to one row (#450).
func FlexRows(total int, fixed ...string) int {
	occupied := theme.SectionBorderStyle().GetVerticalFrameSize()
	for _, s := range fixed {
		occupied += lipgloss.Height(s)
	}
	return total - occupied
}
