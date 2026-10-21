package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #333 step 3 — Repository Status gets the shared action bar
// (tui-package-details.md § Action Bar Convention).

func TestRepoStatusActionBarAndHelp(t *testing.T) {
	m := NewRepositoryStatusModel(deps.Dependencies{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	m, _ = m.Update(RepoStatusLoadedMsg{Initialized: true})
	closedPane := m.pkgList.Height
	view := m.View()
	if !strings.Contains(view, "R Refresh") || !strings.Contains(view, "? more") || strings.Contains(view, "Esc Back") {
		t.Fatalf("bar missing from the screen:\n%s", view)
	}
	viewH := lipgloss.Height(view)
	if viewH > 40 {
		t.Fatalf("screen is %d rows, taller than the 40 it was given: the bar is not measured", viewH)
	}

	m, _ = m.Update(helpKeyMsg())
	if !m.helpOpen {
		t.Fatal("? did not open the help")
	}
	// Few actions: the full help fits on one row, so the list keeps its
	// height. What matters is that the screen does not grow.
	if v := m.View(); !strings.Contains(v, "Close help") || strings.Contains(v, "? more") {
		t.Errorf("full help not shown:\n%s", v)
	}
	if h := lipgloss.Height(m.View()); h != viewH {
		t.Errorf("screen is %d rows with help open, %d closed", h, viewH)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpOpen || m.pkgList.Height != closedPane {
		t.Fatalf("Esc: helpOpen %v, height %d (want closed, %d)", m.helpOpen, m.pkgList.Height, closedPane)
	}
	if cmd != nil {
		if _, back := cmd().(msgs.BackMsg); back {
			t.Fatal("Esc navigated back while the help was open")
		}
	}
}
