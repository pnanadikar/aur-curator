package screens

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/widgets"
)

// #450 — on a short terminal the Dashboard's page shrinks to what fits, so
// the selected row is never cut off; when not even one row fits, the App
// shows a "terminal too small" line (tui-dashboard-wireframe.md § Page
// Size, § 6).

// shortHeight returns the screen height at which the Dashboard has rows
// package rows left: the chrome is measured at a tall height first.
func shortHeight(m DashboardModel, rows int) int {
	const tall = 100
	return tall - m.listRowsLeft(tall) + rows
}

func TestDashboardPageFitsShortTerminal(t *testing.T) {
	t.Parallel()

	for _, rows := range []int{5, 3, 1} {
		m := pageSizeTestModel(10, true)
		h := shortHeight(m, rows)
		m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: h})
		m.buildEntries(context.Background(), "test") // real rows, as after a load

		if got := m.pkgList.PageSize(); got != rows {
			t.Errorf("rows %d: PageSize() = %d, want %d", rows, got, rows)
		}
		if got := lipgloss.Height(widgets.WrapScreenBody(200, m.View())); got > h {
			t.Errorf("rows %d: the boxed Dashboard is %d rows, more than the %d it has", rows, got, h)
		}
		if m.TooSmall() {
			t.Errorf("rows %d: TooSmall() with a row to spare", rows)
		}
	}
}

func TestDashboardTooSmall(t *testing.T) {
	t.Parallel()

	m := pageSizeTestModel(10, true)
	h := shortHeight(m, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: h})

	if !m.TooSmall() {
		t.Fatalf("TooSmall() = false with no row left (height %d)", h)
	}
	if got := m.pkgList.PageSize(); got != 1 {
		t.Errorf("PageSize() = %d, want the floor of 1", got)
	}

	// The terminal is 40 rows here; a page of MinFlexHeight rows needs
	// MinFlexHeight more.
	lines, cols := m.RecommendedSize(40)
	if want := 40 + widgets.MinFlexHeight; lines != want {
		t.Errorf("recommended lines = %d, want %d", lines, want)
	}
	if min := theme.MinScreenWidth + widgets.ScreenBodyMargin; cols < min {
		t.Errorf("recommended columns = %d, want at least %d", cols, min)
	}
}
