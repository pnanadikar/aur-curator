package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/theme"
)

// #344 — Home/End on the Review screen's Diff and Findings panes.

func TestReviewHomeEnd(t *testing.T) {
	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	upd, _ := model.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	model = upd.(ReviewScreen)
	model.diffPane.SetFiles([]git.FileDiff{{Patch: manyLines(200)}})

	upd, _ = model.Update(endKeyMsg)
	model = upd.(ReviewScreen)
	if model.diffPane.ScrollPosition() == 0 {
		t.Fatal("End did not scroll the Diff pane to the bottom")
	}
	upd, _ = model.Update(homeKeyMsg)
	model = upd.(ReviewScreen)
	if got := model.diffPane.ScrollPosition(); got != 0 {
		t.Fatalf("Home: Diff scroll position %d, want 0", got)
	}

	// Findings tab: the jump goes to the Findings pane, not the Diff pane.
	model.tabs.SetActive(4)
	model.findingsPane.SetContent(manyLines(200))
	upd, _ = model.Update(endKeyMsg)
	model = upd.(ReviewScreen)
	if model.findingsPane.ScrollPosition() == 0 {
		t.Fatal("End did not scroll the Findings pane to the bottom")
	}
	if model.diffPane.ScrollPosition() != 0 {
		t.Fatal("End on the Findings tab scrolled the Diff pane")
	}
}

// TestReviewHomeEndBlockedByModal: with the Approve modal open, End goes
// to the modal's notes input (cursor to end), not the pane.
func TestReviewHomeEndBlockedByModal(t *testing.T) {
	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	upd, _ := model.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	model = upd.(ReviewScreen)
	model.diffPane.SetFiles([]git.FileDiff{{Patch: manyLines(200)}})

	upd, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = upd.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("setup: Enter did not open the Approve modal")
	}
	upd, _ = model.Update(endKeyMsg)
	model = upd.(ReviewScreen)
	if model.diffPane.ScrollPosition() != 0 {
		t.Fatal("End scrolled the Diff pane while the Approve modal was open")
	}
}
