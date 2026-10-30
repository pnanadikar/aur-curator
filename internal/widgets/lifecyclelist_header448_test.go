package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/lifecycle"
)

// cellStart returns the display column at which sub starts in line, or
// fails the test.
func cellStart(t *testing.T, line, sub string) int {
	t.Helper()
	i := strings.Index(line, sub)
	if i < 0 {
		t.Fatalf("%q not found in %q", sub, line)
	}
	return lipgloss.Width(line[:i])
}

// TestHeaderRowLabelsOverColumns: each header label starts where its
// column starts in the rows, with a narrow and a wide State column
// (tui-dashboard-wireframe.md § Header Row, #448).
func TestHeaderRowLabelsOverColumns(t *testing.T) {
	cases := []struct {
		name  string
		state lifecycle.State
	}{
		{"narrow State", lifecycle.Merged},
		{"wide State", lifecycle.ReviewPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLifecycleList([]LifecycleEntry{{
				Name:               "pkgname",
				Lifecycle:          tc.state,
				NextAction:         "Act",
				Available:          "AV",
				ConfirmedPublished: "PUB",
				Risk:               "RSK",
				LocalCommits:       7,
				Updates:            5,
			}})
			header := l.HeaderRow()
			row := strings.SplitN(l.Render(), "\n", 2)[0]

			pairs := []struct{ label, cell string }{
				{"Package", "pkgname"},
				{"State", string(tc.state)},
				{"Next Action", "Act"},
				{"Available", "AV"},
				{"Published", "PUB"},
				{"Risk", "RSK"},
				{"Local", "7"},
				{"Behind", "↑5"},
			}
			for _, p := range pairs {
				if h, r := cellStart(t, header, p.label), cellStart(t, row, p.cell); h != r {
					t.Errorf("%s label at %d, its cell %q at %d\nheader: %q\nrow:    %q", p.label, h, p.cell, r, header, row)
				}
			}
		})
	}
}

// TestHeaderRowFollowsPage: the State width is the page's, so moving to a
// page with a wider state moves the labels after State with the rows.
func TestHeaderRowFollowsPage(t *testing.T) {
	l := NewLifecycleList([]LifecycleEntry{
		{Name: "a", Lifecycle: lifecycle.Merged, NextAction: "Act"},
		{Name: "b", Lifecycle: lifecycle.ReviewPending, NextAction: "Act"},
	})
	l.SetPageSize(1)
	first := cellStart(t, l.HeaderRow(), "Next Action")
	l.NextPage()
	second := cellStart(t, l.HeaderRow(), "Next Action")
	if want := first + len(lifecycle.ReviewPending) - len(lifecycle.Merged); second != want {
		t.Fatalf("Next Action label at %d on page 2, want %d (page 1: %d)", second, want, first)
	}
	row := strings.SplitN(l.Render(), "\n", 2)[0]
	if r := cellStart(t, row, "Act"); r != second {
		t.Fatalf("page 2: label at %d, cell at %d", second, r)
	}
}

// TestHeaderRowWithNoRows: the header is still returned when no rows
// match the filter, so the layout does not move; State falls back to the
// label's width.
func TestHeaderRowWithNoRows(t *testing.T) {
	l := NewLifecycleList([]LifecycleEntry{{Name: "a", Lifecycle: lifecycle.Merged}})
	l.SetSearch("nothing-matches")
	h := l.HeaderRow()
	for _, label := range []string{"Package", "State", "Next Action", "Local", "Behind"} {
		if !strings.Contains(h, label) {
			t.Fatalf("header %q lacks %q", h, label)
		}
	}
	if got := lipgloss.Height(h); got != 1 {
		t.Fatalf("header is %d lines, want 1", got)
	}
}

// TestLocalCellText: with the header naming the column, the Local cell is
// the bare count or "?" (#448), not "N local".
func TestLocalCellText(t *testing.T) {
	base := LifecycleEntry{Name: "pkg", Lifecycle: lifecycle.Merged}
	some := base
	some.LocalCommits = 3
	failed := base
	failed.LocalCommitsUnknown = true
	for _, e := range []LifecycleEntry{some, failed} {
		if row := firstRow(e); strings.Contains(row, "local") {
			t.Errorf("Local cell still self-labels: %q", row)
		}
	}
}
