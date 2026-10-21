package widgets

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// DashboardToolbar provides the search bar and filter/sort indicators
// that sit between the SummaryCards and the package list on the
// Dashboard screen.
type DashboardToolbar struct {
	searchInput textinput.Model
	width       int
	searchText  string
	filter      string
	sort        string
	activeSort  string
	sortEnabled bool
	sortKey     key.Binding
}

// NewDashboardToolbar constructs a DashboardToolbar with a blank
// search input and default filter/sort values.
func NewDashboardToolbar() DashboardToolbar {
	//input := textinput.New()
	//input.Placeholder = "Search packages..."
	//input.PlaceholderStyle = theme.DimmedStyle()
	//input.PromptStyle = theme.FocusedButtonStyle()

	return DashboardToolbar{
		//	searchInput: input,
		filter:      "All",
		sort:        "name",
		activeSort:  "Alphabetical",
		sortEnabled: false,
		sortKey: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("^O", "Sort"),
		),
	}
}

// SetWidth resizes the search input to fit the available width.
func (t *DashboardToolbar) SetWidth(width int) {
	t.searchInput.Width = max(width-8, 10)
}

// SetValue sets the current search text and updates the input model.
func (t *DashboardToolbar) SetValue(v string) {
	t.searchText = v
	t.searchInput.SetValue(v)
}

// Value returns the current search text.
func (t DashboardToolbar) Value() string {
	return t.searchText
}

// Filter returns the currently active lifecycle filter.
func (t DashboardToolbar) Filter() string {
	return t.filter
}

// SetFilter sets the lifecycle filter value.
func (t *DashboardToolbar) SetFilter(f string) {
	t.filter = f
}

// Sort returns the current sort mode.
func (t DashboardToolbar) Sort() string {
	return t.sort
}

// SetSort sets the sort mode ("Alphabetical" or "Lifecycle").
func (t *DashboardToolbar) SetSort(s string) {
	t.sort = s
	t.activeSort = s
}

// SortEnabled reports whether sort toggling is active.
func (t DashboardToolbar) SortEnabled() bool {
	return t.sortEnabled
}

// SetSortEnabled controls whether sort toggling is active.
func (t *DashboardToolbar) SetSortEnabled(v bool) {
	t.sortEnabled = v
}

// SortKey returns the key binding for toggling sort mode.
func (t DashboardToolbar) SortKey() key.Binding {
	return t.sortKey
}

// ToggleSort switches between "Alphabetical" and "Lifecycle" sort modes.
func (t *DashboardToolbar) ToggleSort() {
	if !t.sortEnabled {
		return
	}
	if t.activeSort == "Alphabetical" {
		t.activeSort = "Lifecycle"
		t.sort = "lifecycle"
	} else {
		t.activeSort = "Alphabetical"
		t.sort = "name"
	}
}

// Update processes keyboard input for the search field.
func (t *DashboardToolbar) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab":
			return nil
		}
	}
	t.searchInput, cmd = t.searchInput.Update(msg)
	t.searchText = t.searchInput.Value()
	return cmd
}

// View renders the toolbar with a search input and filter/sort indicators.
func (t DashboardToolbar) View() string {
	filterStyle := theme.SubtleStyle()
	filterLabel := "Filter: " + filterStyle.Render(t.filter)

	sortLabel := t.renderSortLabel()

	left := lipgloss.JoinHorizontal(lipgloss.Top, filterLabel, "  ", sortLabel)
	search := t.searchInput.View()

	if t.width <= 0 {
		return left + " " + search
	}

	right := lipgloss.PlaceHorizontal(t.width, lipgloss.Right, search)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (t DashboardToolbar) renderSortLabel() string {
	activeStyle := theme.EnabledActionStyle()
	dimmedStyle := theme.DimmedStyle()
	activeParen := activeStyle.Render("(·)")
	inactiveParen := dimmedStyle.Render("( )")

	if !t.sortEnabled {
		return "Sort: " + dimmedStyle.Render("(·)") + " " + dimmedStyle.Render("Alphabetical") + "   " + inactiveParen + " " + dimmedStyle.Render("Lifecycle")
	}

	if t.activeSort == "Alphabetical" {
		return "Sort: " + activeParen + " " + activeStyle.Render("Alphabetical") + "   " + inactiveParen + " " + dimmedStyle.Render("Lifecycle")
	}
	return "Sort: " + dimmedStyle.Render("Alphabetical") + "   " + inactiveParen + " " + activeParen + " " + activeStyle.Render("Lifecycle")
}
