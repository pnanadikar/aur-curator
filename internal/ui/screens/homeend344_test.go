package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/theme"
)

// #344 — Home/End jump to the top/bottom of every scrollable pane.

var (
	homeKeyMsg = tea.KeyMsg{Type: tea.KeyHome}
	endKeyMsg  = tea.KeyMsg{Type: tea.KeyEnd}
)

func manyLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

func TestDetailHomeEnd(t *testing.T) {
	m := detailAt(nil, "A")
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	m.contentPane.SetContent(manyLines(200))

	m, _ = m.Update(endKeyMsg)
	if m.contentPane.ScrollPosition() == 0 {
		t.Fatal("End did not scroll to the bottom")
	}
	m, _ = m.Update(homeKeyMsg)
	if got := m.contentPane.ScrollPosition(); got != 0 {
		t.Fatalf("Home: scroll position %d, want 0", got)
	}
}

// TestDetailHomeEndBlockedByModal: with the build gate's modal open, End
// belongs to the modal and must not scroll (#320).
func TestDetailHomeEndBlockedByModal(t *testing.T) {
	m := detailAt(nil, "A")
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	m.contentPane.SetContent(manyLines(200))
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{localCommits: 1}))
	m.gate = g

	m, _ = m.Update(endKeyMsg)
	if m.contentPane.ScrollPosition() != 0 {
		t.Fatal("End scrolled the pane while a modal was open")
	}
}

func TestRepoStatusHomeEnd(t *testing.T) {
	m := NewRepositoryStatusModel(deps.Dependencies{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	pkgs := make([]RepoPackageEntry, 200)
	for i := range pkgs {
		pkgs[i] = RepoPackageEntry{Name: fmt.Sprintf("pkg%03d", i), Version: "1.0-1"}
	}
	m, _ = m.Update(RepoStatusLoadedMsg{Initialized: true, Packages: pkgs})

	m, _ = m.Update(endKeyMsg)
	if m.pkgList.YOffset == 0 {
		t.Fatal("End did not scroll to the bottom")
	}
	m, _ = m.Update(homeKeyMsg)
	if m.pkgList.YOffset != 0 {
		t.Fatalf("Home: offset %d, want 0", m.pkgList.YOffset)
	}
}

func TestHelpScreenHomeEnd(t *testing.T) {
	m := NewHelpModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	m.pane.SetContent(manyLines(200))

	m, _ = m.Update(endKeyMsg)
	if m.pane.YOffset == 0 {
		t.Fatal("End did not scroll to the bottom")
	}
	m, _ = m.Update(homeKeyMsg)
	if m.pane.YOffset != 0 {
		t.Fatalf("Home: offset %d, want 0", m.pane.YOffset)
	}
}

// Dashboard is out of scope (#344): Home/End must not trigger anything there.
func TestDashboardIgnoresHomeEnd(t *testing.T) {
	m := dashboardWith(lifecycle.Merged)
	if _, cmd := m.Update(endKeyMsg); cmd != nil {
		t.Fatalf("End on the Dashboard produced a command (%T)", cmd())
	}
}
