package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/lifecycle"
)

// TestStateText covers what each state shows after it
// (tui-dashboard-wireframe.md § State, #428).
func TestStateText(t *testing.T) {
	all := StateVersions{Base: "1.10.0-1", Reviewed: "1.11.0-1", Head: "1.9.0-1"}
	first := StateVersions{Reviewed: "1.11.0-1", FirstReview: true}
	cases := []struct {
		name  string
		state lifecycle.State
		v     StateVersions
		want  string
	}{
		{"Not Reviewed: no versions", lifecycle.NotReviewed, all, "Not Reviewed"},
		{"Review Pending: base → reviewed", lifecycle.ReviewPending, all, "Review Pending 1.10.0-1 → 1.11.0-1"},
		{"Approved: base → reviewed", lifecycle.Approved, all, "Approved 1.10.0-1 → 1.11.0-1"},
		{"first review: → reviewed", lifecycle.ReviewPending, first, "Review Pending → 1.11.0-1"},
		{"Rejected: reviewed", lifecycle.Rejected, all, "Rejected 1.11.0-1"},
		{"Merged: master HEAD", lifecycle.Merged, all, "Merged 1.9.0-1"},
		{"Built: master HEAD", lifecycle.Built, all, "Built 1.9.0-1"},
		{"Confirmed: master HEAD", lifecycle.Confirmed, all, "Confirmed 1.9.0-1"},
		{"Published: master HEAD", lifecycle.Published, all, "Published 1.9.0-1"},

		// Never half a pair, never a guess.
		{"reviewed not read", lifecycle.Approved, StateVersions{Base: "1.10.0-1"}, "Approved"},
		{"base not read, not a first review", lifecycle.Approved, StateVersions{Reviewed: "1.11.0-1"}, "Approved"},
		{"HEAD not read", lifecycle.Merged, StateVersions{Reviewed: "1.11.0-1"}, "Merged"},
		{"Rejected, reviewed not read", lifecycle.Rejected, StateVersions{}, "Rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stateText(tc.state, tc.v); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStateTextWidth: the State cell is at most StateColMaxWidth cells. A
// pair that does not fit drops its base; what still does not fit is cut
// with "…", the state itself never.
func TestStateTextWidth(t *testing.T) {
	cases := []struct {
		name  string
		state lifecycle.State
		v     StateVersions
		want  string
	}{
		{"fits exactly", lifecycle.ReviewPending,
			StateVersions{Base: "12.4.1-1", Reviewed: "12.4.2-1"},
			"Review Pending 12.4.1-1 → 12.4.2-1"},
		{"pair too long: base dropped", lifecycle.Approved,
			StateVersions{Base: "r340.e18a095-1", Reviewed: "r345.abc1234-1"},
			"Approved → r345.abc1234-1"},
		{"still too long: cut", lifecycle.ReviewPending,
			StateVersions{Base: "x", Reviewed: "0.13.0.r202.gc3c06723f0-1"},
			"Review Pending → 0.13.0.r202.gc3c…"},
		{"single version too long: cut", lifecycle.Published,
			StateVersions{Head: "1:2.0.r4.gdef5678abcdef0123-2"},
			"Published 1:2.0.r4.gdef5678abcdef…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stateText(tc.state, tc.v)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if w := lipgloss.Width(got); w > StateColMaxWidth {
				t.Fatalf("%q is %d cells, more than %d", got, w, StateColMaxWidth)
			}
		})
	}
}

// TestRenderStateVersions: the row shows the versions after the state, and
// the columns after State stay lined up with the header when the page has
// versions of different lengths.
func TestRenderStateVersions(t *testing.T) {
	l := NewLifecycleList([]LifecycleEntry{
		{Name: "a", Lifecycle: lifecycle.Approved, NextAction: "Merge",
			Versions: StateVersions{Base: "1.10.0-1", Reviewed: "1.11.0-1"}},
		{Name: "b", Lifecycle: lifecycle.Merged, NextAction: "Build",
			Versions: StateVersions{Head: "2.0-1"}},
	})
	lines := strings.Split(strings.TrimRight(l.Render(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 rows, got:\n%s", l.Render())
	}
	if !strings.Contains(lines[0], "Approved 1.10.0-1 → 1.11.0-1") || !strings.Contains(lines[1], "Merged") {
		t.Fatalf("versions missing:\n%s\n%s", lines[0], lines[1])
	}
	if a, b := cellStart(t, lines[0], "1.11.0-1"), cellStart(t, lines[1], "2.0-1"); a != b {
		t.Fatalf("versions start at %d and %d:\n%s\n%s", a, b, lines[0], lines[1])
	}
	h := cellStart(t, l.HeaderRow(), "Next Action")
	if a, b := cellStart(t, lines[0], "Merge"), cellStart(t, lines[1], "Build"); a != h || b != h {
		t.Fatalf("Next Action label at %d, cells at %d and %d", h, a, b)
	}
}

// TestStateLayoutAlignsVersions: on one page, the state names are padded
// to the longest, arrows sit in one column and every row's own version
// starts in the same column (manual test of #428).
func TestStateLayoutAlignsVersions(t *testing.T) {
	entries := []LifecycleEntry{
		{Lifecycle: lifecycle.ReviewPending, Versions: StateVersions{Reviewed: "0.1.0-1", FirstReview: true}},
		{Lifecycle: lifecycle.Merged, Versions: StateVersions{Head: "1.11.0-1"}},
		{Lifecycle: lifecycle.Approved, Versions: StateVersions{Reviewed: "2.27-1", FirstReview: true}},
		{Lifecycle: lifecycle.Approved, Versions: StateVersions{Base: "1.9-1", Reviewed: "1.10-1"}},
		{Lifecycle: lifecycle.Rejected, Versions: StateVersions{Reviewed: "3.0-2"}},
		{Lifecycle: lifecycle.NotReviewed},
	}
	cells, width := stateLayout(entries)
	want := []string{
		"Review Pending       → 0.1.0-1",
		"Merged                 1.11.0-1",
		"Approved             → 2.27-1",
		"Approved       1.9-1 → 1.10-1",
		"Rejected               3.0-2",
		"Not Reviewed",
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Errorf("row %d: got %q, want %q", i, cells[i], want[i])
		}
	}
	if width != lipgloss.Width(want[1]) {
		t.Errorf("width = %d, want %d", width, lipgloss.Width(want[1]))
	}
}

// TestStateLayoutDropsBasesTogether: when a page does not fit, every pair
// drops its base, so the arrows stay in one column.
func TestStateLayoutDropsBasesTogether(t *testing.T) {
	entries := []LifecycleEntry{
		{Lifecycle: lifecycle.ReviewPending, Versions: StateVersions{Base: "r340.e18a095-1", Reviewed: "r345.abc1234-1"}},
		{Lifecycle: lifecycle.Approved, Versions: StateVersions{Base: "1.0-1", Reviewed: "1.1-1"}},
		{Lifecycle: lifecycle.Merged, Versions: StateVersions{Head: "2.0-1"}},
	}
	cells, width := stateLayout(entries)
	want := []string{
		"Review Pending → r345.abc1234-1",
		"Approved       → 1.1-1",
		"Merged           2.0-1",
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Errorf("row %d: got %q, want %q", i, cells[i], want[i])
		}
	}
	if width > StateColMaxWidth {
		t.Errorf("width %d over %d", width, StateColMaxWidth)
	}
}

// TestRenderStateVersionsAligned: in rendered rows, the versions after the
// state start in the same column.
func TestRenderStateVersionsAligned(t *testing.T) {
	l := NewLifecycleList([]LifecycleEntry{
		{Name: "iotop-rs", Lifecycle: lifecycle.ReviewPending, Versions: StateVersions{Reviewed: "0.1.0-1", FirstReview: true}},
		{Name: "pfetch", Lifecycle: lifecycle.Merged, Versions: StateVersions{Head: "1.11.0-1"}},
	})
	lines := strings.Split(strings.TrimRight(l.Render(), "\n"), "\n")
	if a, b := cellStart(t, lines[0], "0.1.0-1"), cellStart(t, lines[1], "1.11.0-1"); a != b {
		t.Fatalf("versions start at %d and %d:\n%s\n%s", a, b, lines[0], lines[1])
	}
}
