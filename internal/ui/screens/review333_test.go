package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// #333 step 3 — Review gets the shared action bar (tui-review.md § 5,
// tui-package-details.md § Action Bar Convention).

func TestReviewCompactBarFitsMinimumWidth(t *testing.T) {
	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	if w := lipgloss.Width(widgets.RenderActionBar(model.actions())); w > maxBarWidth {
		t.Errorf("Review bar is %d wide, max %d", w, maxBarWidth)
	}
}

// TestReviewHelpToggle: ? opens the full help; Esc closes it instead of
// navigating back; with the Approve modal open, ? belongs to the modal.
func TestReviewHelpToggle(t *testing.T) {
	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	upd, _ := model.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	model = upd.(ReviewScreen)
	closedBar := lipgloss.Height(model.renderActionBar())

	upd, _ = model.Update(helpKeyMsg())
	model = upd.(ReviewScreen)
	if !model.helpOpen {
		t.Fatal("? did not open the help")
	}
	if lipgloss.Height(model.renderActionBar()) <= closedBar {
		t.Errorf("full help (%d rows) not taller than the compact bar (%d)", lipgloss.Height(model.renderActionBar()), closedBar)
	}

	upd, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = upd.(ReviewScreen)
	if model.helpOpen {
		t.Fatal("Esc did not close the help")
	}
	if cmd != nil {
		if _, back := cmd().(msgs.BackMsg); back {
			t.Fatal("Esc navigated back while the help was open")
		}
	}

	// Approve modal open: ? goes to the modal's notes input.
	upd, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = upd.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("setup: Enter did not open the Approve modal")
	}
	upd, _ = model.Update(helpKeyMsg())
	model = upd.(ReviewScreen)
	if model.helpOpen {
		t.Fatal("? opened the help while the Approve modal was open")
	}
}
