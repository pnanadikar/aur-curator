package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/lifecycle"
)

// firstRow renders a one-entry list and returns its single row.
func firstRow(e LifecycleEntry) string {
	return strings.SplitN(NewLifecycleList([]LifecycleEntry{e}).Render(), "\n", 2)[0]
}

// TestLifecycleListLocalCommitsColumn covers the Local column
// (workflow.md § Local Commits Not on Upstream): blank when none, the
// count when some, "?" when the check failed — never blank for a failed
// check — and the column keeps every row the same width.
func TestLifecycleListLocalCommitsColumn(t *testing.T) {
	base := LifecycleEntry{Name: "pkg", Lifecycle: lifecycle.Merged, NextAction: "Build"}

	none := firstRow(base)
	withN := base
	withN.LocalCommits = 3
	some := firstRow(withN)
	failed := base
	failed.LocalCommitsUnknown = true
	unknown := firstRow(failed)

	if strings.ContainsAny(none, "3?") {
		t.Fatalf("no local commits must render blank, got %q", none)
	}
	if !strings.Contains(some, "3") {
		t.Fatalf("count missing: %q", some)
	}
	if !strings.Contains(unknown, "?") {
		t.Fatalf("failed check must render \"?\": %q", unknown)
	}
	w := lipgloss.Width(none)
	if lipgloss.Width(some) != w || lipgloss.Width(unknown) != w {
		t.Fatalf("row widths differ: none=%d some=%d unknown=%d", w, lipgloss.Width(some), lipgloss.Width(unknown))
	}
}

// TestLifecycleListUnknownWinsOverCount: a failed check's count is
// meaningless, so "?" is shown even if a count is set.
func TestLifecycleListUnknownWinsOverCount(t *testing.T) {
	e := LifecycleEntry{Name: "pkg", Lifecycle: lifecycle.Merged, LocalCommits: 7, LocalCommitsUnknown: true}
	row := firstRow(e)
	if strings.Contains(row, "7") || !strings.Contains(row, "?") {
		t.Fatalf("want \"?\" and no count, got %q", row)
	}
}
