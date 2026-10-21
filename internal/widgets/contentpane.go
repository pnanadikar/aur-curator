package widgets

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ContentPane renders a scrollable block of pre-formatted text.
// It is the tab-agnostic counterpart to DiffPane — callers pass in
// a fully rendered string; ContentPane has no opinion on Git diffs
// or any other content domain.
type ContentPane struct {
	viewport viewport.Model
}

// NewContentPane constructs a ContentPane with a ready-to-use viewport.
func NewContentPane() ContentPane {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return ContentPane{viewport: vp}
}

// SetSize updates the viewport dimensions.
func (c *ContentPane) SetSize(width, height int) {
	c.viewport.Width = max(width, 0)
	c.viewport.Height = max(height, 0)
}

// SetContent replaces the viewport's rendered text content.
// content is expected to already be styled (e.g. via theme styles).
func (c *ContentPane) SetContent(content string) {
	c.viewport.SetContent(content)
}

// Update processes scroll keys for the viewport.
func (c *ContentPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.viewport, cmd = c.viewport.Update(msg)
	return cmd
}

// GotoTop scrolls to the first line (Home, #344).
func (c *ContentPane) GotoTop() { c.viewport.GotoTop() }

// GotoBottom scrolls to the last line (End, #344).
func (c *ContentPane) GotoBottom() { c.viewport.GotoBottom() }

// View returns the rendered viewport content.
func (c ContentPane) View() string {
	return c.viewport.View()
}

// ViewportHeight returns the current viewport height.
func (c ContentPane) ViewportHeight() int {
	return c.viewport.Height
}

// ScrollPosition returns the current scroll line.
func (c ContentPane) ScrollPosition() int {
	return c.viewport.YOffset
}
