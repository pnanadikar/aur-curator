package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// #333 — compact action bar with full help on ? (tui-package-details.md
// § Action Bar Convention), Dashboard and Package Details.

func actionByKey(t *testing.T, actions []widgets.Action, k string) widgets.Action {
	t.Helper()
	for _, a := range actions {
		if a.Key == k {
			return a
		}
	}
	t.Fatalf("no action with key %q", k)
	return widgets.Action{}
}

// maxBarWidth is the widest a compact bar may be: the minimum screen
// width minus the screen box frame.
const maxBarWidth = theme.MinScreenWidth - 2

func helpKeyMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")} }

func dashboardWith(lc lifecycle.State) DashboardModel {
	m := NewDashboardModel(deps.Dependencies{})
	m.lifecycle = map[domain.PackageName]lifecycle.State{"yay": lc}
	m.pkgList = widgets.NewLifecycleList([]widgets.LifecycleEntry{{Name: "yay", Lifecycle: lc}})
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	return m
}

// TestCompactBarsFitMinimumWidth: in every state, each screen's compact
// bar is one line no wider than the minimum screen allows.
func TestCompactBarsFitMinimumWidth(t *testing.T) {
	for _, lc := range []lifecycle.State{lifecycle.ReviewPending, lifecycle.Approved, lifecycle.Merged, lifecycle.Built, lifecycle.Confirmed, lifecycle.Published} {
		d := dashboardWith(lc)
		if w := lipgloss.Width(widgets.RenderActionBar(d.actions())); w > maxBarWidth {
			t.Errorf("Dashboard (%s): bar is %d wide, max %d", lc, w, maxBarWidth)
		}
	}
	empty := NewDashboardModel(deps.Dependencies{})
	if w := lipgloss.Width(widgets.RenderActionBar(empty.actions())); w > maxBarWidth {
		t.Errorf("Dashboard (no rows): bar is %d wide, max %d", w, maxBarWidth)
	}
	built := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}
	for _, m := range []PackageDetailModel{detailAt(nil, "A"), detailAt(built, "A"), NewPackageDetailModel(deps.Dependencies{})} {
		if w := lipgloss.Width(widgets.RenderActionBar(m.actions())); w > maxBarWidth {
			t.Errorf("Package Details: bar is %d wide, max %d", w, maxBarWidth)
		}
	}
}

// TestDashboardActionReasons: unavailable actions carry the shared
// prerequisite text; with no row selected, row actions say so.
func TestDashboardActionReasons(t *testing.T) {
	m := dashboardWith(lifecycle.ReviewPending)
	cases := map[string]string{"M": prereqApproved, "B": prereqMergedOrLater, "C": prereqBuilt, "P": prereqConfirmed}
	for k, want := range cases {
		a := actionByKey(t, m.actions(), k)
		if a.Enabled || a.Reason != want {
			t.Errorf("%s: enabled %v reason %q, want disabled %q", k, a.Enabled, a.Reason, want)
		}
	}
	// Merge follows the state alone, not commits behind upstream (#406).
	approved := dashboardWith(lifecycle.Approved) // no commits-behind data
	if a := actionByKey(t, approved.actions(), "M"); !a.Enabled {
		t.Errorf("Approved: Merge %+v, want enabled", a)
	}
	merged := dashboardWith(lifecycle.Merged)
	if a := actionByKey(t, merged.actions(), "M"); a.Enabled || a.Reason != reasonNothingToMerge {
		t.Errorf("Merged: Merge %+v, want disabled %q", a, reasonNothingToMerge)
	}
	empty := NewDashboardModel(deps.Dependencies{})
	for _, k := range []string{"ENTER", "M", "F", "B", "C", "P"} {
		if a := actionByKey(t, empty.actions(), k); a.Enabled || a.Reason != reasonNoPackage {
			t.Errorf("no rows, %s: %+v, want disabled %q", k, a, reasonNoPackage)
		}
	}
}

// TestDashboardHelpToggle: ? opens the full help, which takes rows from
// the package list; ? or Esc closes it.
func TestDashboardHelpToggle(t *testing.T) {
	m := dashboardWith(lifecycle.Merged)
	closedPage := m.listPageSize(m.lastHeight)

	m, _ = m.Update(helpKeyMsg())
	if !m.helpOpen {
		t.Fatal("? did not open the help")
	}
	if lipgloss.Height(m.renderActionBar()) <= 2 {
		t.Fatalf("help renders no extra rows:\n%s", m.renderActionBar())
	}
	if m.pkgList.PageSize() != m.listPageSize(m.lastHeight) || m.pkgList.PageSize() >= closedPage {
		t.Errorf("list page size %d with help open, want the re-measured size < %d", m.pkgList.PageSize(), closedPage)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpOpen || m.pkgList.PageSize() != closedPage {
		t.Errorf("Esc: helpOpen %v, page %d (want closed, %d)", m.helpOpen, m.pkgList.PageSize(), closedPage)
	}
	m, _ = m.Update(helpKeyMsg())
	m, _ = m.Update(helpKeyMsg())
	if m.helpOpen {
		t.Error("second ? did not close the help")
	}
}

// TestDetailHelpToggle: ? opens the help and shrinks the content pane;
// Esc closes the help instead of navigating back.
func TestDetailHelpToggle(t *testing.T) {
	m := detailAt(nil, "A")
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	closed := m.contentHeight()

	closedView := lipgloss.Height(m.View())

	m, _ = m.Update(helpKeyMsg())
	if !m.helpOpen {
		t.Fatal("? did not open the help")
	}
	// The pane is re-sized, so the screen keeps its height: the help
	// takes rows from the content, it does not push the frame taller.
	if got := lipgloss.Height(m.View()); got != closedView {
		t.Errorf("screen is %d rows with help open, %d closed", got, closedView)
	}
	if m.contentHeight() >= closed {
		t.Errorf("content height %d with help open, want < %d", m.contentHeight(), closed)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpOpen {
		t.Fatal("Esc did not close the help")
	}
	if cmd != nil {
		if _, back := cmd().(msgs.BackMsg); back {
			t.Fatal("Esc navigated back while the help was open")
		}
	}
	if m.contentHeight() != closed {
		t.Errorf("content height %d after closing, want %d", m.contentHeight(), closed)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc with the help closed must navigate back")
	}
	if _, back := cmd().(msgs.BackMsg); !back {
		t.Fatal("Esc with the help closed must send BackMsg")
	}
}

// TestHelpNotOpenedInsideModal: the build gate's modal owns ? (#320).
func TestHelpNotOpenedInsideModal(t *testing.T) {
	m := dashboardWith(lifecycle.Merged)
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{localCommits: 1}))
	m.gate = g
	m, _ = m.Update(helpKeyMsg())
	if m.helpOpen {
		t.Fatal("? opened the help while a modal was open")
	}
}
