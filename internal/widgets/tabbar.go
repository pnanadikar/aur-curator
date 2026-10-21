package widgets

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// Tab describes one tab in the reusable review tab bar.
type Tab struct {
	Label   string
	Enabled bool
}

// TabBar renders tabs and manages active-tab navigation.
type TabBar struct {
	tabs   []Tab
	active int
	width  int
}

// NewTabBar constructs a TabBar and selects the first enabled tab.
func NewTabBar(tabs []Tab) TabBar {
	bar := TabBar{tabs: append([]Tab(nil), tabs...)}
	for i, tab := range bar.tabs {
		if tab.Enabled {
			bar.active = i
			break
		}
	}
	return bar
}

// SetWidth updates the render width used to align the findings label.
func (t *TabBar) SetWidth(width int) {
	t.width = width
}

// SetActive sets the active tab by index, skipping to the next enabled tab
// if the requested index is disabled.
func (t *TabBar) SetActive(index int) {
	if len(t.tabs) == 0 {
		return
	}
	if index < 0 || index >= len(t.tabs) {
		return
	}
	if t.tabs[index].Enabled {
		t.active = index
		return
	}
	// Find next enabled tab from the requested index
	for step := 1; step <= len(t.tabs); step++ {
		next := (index + step) % len(t.tabs)
		if t.tabs[next].Enabled {
			t.active = next
			return
		}
	}
}

// SetEnabled enables or dims the tab with the given label (no-op if there
// is none). If that dims the active tab, the first enabled tab becomes
// active, so a dimmed tab is never shown as active.
func (t *TabBar) SetEnabled(label string, enabled bool) {
	for i := range t.tabs {
		if t.tabs[i].Label != label {
			continue
		}
		t.tabs[i].Enabled = enabled
		if !enabled && i == t.active {
			t.SetActive(0)
		}
		return
	}
}

// Next advances to the next enabled tab, skipping dimmed tabs.
func (t *TabBar) Next() {
	if len(t.tabs) == 0 {
		return
	}
	for step := 1; step <= len(t.tabs); step++ {
		next := (t.active + step) % len(t.tabs)
		if t.tabs[next].Enabled {
			t.active = next
			return
		}
	}
}

// Prev moves to the previous enabled tab, skipping dimmed tabs.
func (t *TabBar) Prev() {
	if len(t.tabs) == 0 {
		return
	}
	for step := 1; step <= len(t.tabs); step++ {
		prev := (t.active - step + len(t.tabs)) % len(t.tabs)
		if t.tabs[prev].Enabled {
			t.active = prev
			return
		}
	}
}

// Active returns the current tab.
func (t TabBar) Active() Tab {
	if len(t.tabs) == 0 {
		return Tab{}
	}
	return t.tabs[t.active]
}

// View renders the tab bar and right-aligned findings status label.
func (t TabBar) View(findingsLabel string) string {
	parts := make([]string, 0, len(t.tabs))
	for i, tab := range t.tabs {
		style := theme.InactiveTabStyle()
		label := tab.Label
		if !tab.Enabled {
			style = theme.DimmedStyle()
		}
		if i == t.active && tab.Enabled {
			label = "[ " + label + " ]"
			style = theme.ActiveTabStyle()
		}
		parts = append(parts, style.Render(label))
	}

	left := strings.Join(parts, " ")
	right := theme.SubtleStyle().Render(findingsLabel)
	if t.width <= 0 {
		return left + " " + right
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.PlaceHorizontal(t.width-lipgloss.Width(left), lipgloss.Right, right))
}
