package screens

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/help"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// HelpModel represents the static Help screen showing
// app description and CLI options.
type HelpModel struct {
	width  int
	height int
	pane   viewport.Model
}

// NewHelpModel constructs a new HelpModel.
func NewHelpModel() HelpModel {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	rendered, err := help.Render(0, true)
	if err != nil {
		rendered = err.Error()
	}
	vp.SetContent(rendered)
	return HelpModel{pane: vp}
}

// Init returns the initialization command for the help screen.
func (m HelpModel) Init() tea.Cmd {
	return nil
}

// renderHelpNavMenu renders the nav bar inside the Help screen bordered box.
func renderHelpNavMenu() string {
	return widgets.RenderNavMenu(widgets.DefaultNavItems())
}

// fixedPartsAbovePane returns the fixed parts rendered above the
// viewport, in render order: nav bar, blank line.
func (m HelpModel) fixedPartsAbovePane() []string {
	return []string{renderHelpNavMenu(), ""}
}

// syncLayout sizes the viewport to whatever room remains for the
// screen's one flexible section.
func (m *HelpModel) syncLayout() {
	m.pane.Width = max(m.width-8, 0)
	m.pane.Height = widgets.FlexHeight(m.height, m.fixedPartsAbovePane()...)
}

// Update processes messages for the help screen.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLayout()
		return m, nil
	case tea.KeyMsg:
		s := msg.String()
		switch s {
		case "esc":
			return m, func() tea.Msg { return msgs.BackMsg{Changed: false} }
		case "1":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenDashboard, FromMenu: true} }
		case "2":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenRepositoryStatus, FromMenu: true} }
		case "3":
			return m, nil
		case "4":
			return m, nil
		case "q":
			return m, func() tea.Msg { return msgs.QuitRequestedMsg{} }
		case "home":
			m.pane.GotoTop()
			return m, nil
		case "end":
			m.pane.GotoBottom()
			return m, nil
		}
	}
	// Everything else (PgUp/PgDn, arrows, mouse wheel) goes to the
	// viewport so scrolling actually works.
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(msg)
	return m, cmd
}

// View renders the help screen inside the same bordered box every other
// screen uses (theme.SectionBorderStyle), matching Dashboard/Review.
func (m HelpModel) View() string {
	sections := append(m.fixedPartsAbovePane(), m.pane.View())
	return widgets.WrapScreenBody(m.width, strings.Join(sections, "\n"))
}
