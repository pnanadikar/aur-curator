package widgets

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pramod/aur-curator/internal/theme"
)

// NavItem represents a single navigation menu entry.
type NavItem struct {
	Key   string
	Label string
	Live  bool
}

// DefaultNavItems returns the standard five navigation menu items.
func DefaultNavItems() []NavItem {
	return []NavItem{
		{Key: "1", Label: "Packages", Live: true},
		{Key: "2", Label: "Repository", Live: true},
		{Key: "3", Label: "Settings", Live: false},
		{Key: "4", Label: "Help", Live: true},
		{Key: "q", Label: "Quit", Live: true},
	}
}

// ScreenBodyMargin is how much narrower than the terminal WrapScreenBody
// makes a screen's content: content up to the terminal width minus this
// is drawn uncut.
const ScreenBodyMargin = 6

// WrapScreenBody draws content in the screen's section box. Lines wider
// than the box are truncated, never wrapped: one content line is always
// one row, so heights measured with lipgloss.Height (FlexHeight) match
// what is drawn (#329). Wrapping here used to add rows nobody measured,
// pushing App's top rows — the toast — off-screen.
func WrapScreenBody(width int, content string) string {
	w := max(width-ScreenBodyMargin, theme.MinScreenWidth)
	content = lipgloss.NewStyle().MaxWidth(w).Render(content)
	return theme.SectionBorderStyle().Width(w).Render(content)
}

// RenderNavMenu renders the standard 5-item navigation menu bar.
// The items are: [1] Packages, [2] Repository, [3] Settings, [4] Help, [q] Quit.
// The caller determines which chips are live/dimmed based on the current screen.
func RenderNavMenu(items []NavItem) string {
	var parts []string
	for _, item := range items {
		chip := fmt.Sprintf("[%s] %s", item.Key, item.Label)
		if item.Live {
			parts = append(parts, theme.NavChipEnabledStyle().Render(chip))
		} else {
			parts = append(parts, theme.NavChipDimmedStyle().Render(chip))
		}
	}
	return strings.Join(parts, " ")
}
