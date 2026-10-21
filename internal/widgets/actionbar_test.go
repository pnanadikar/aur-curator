package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// #333 — tui-package-details.md § Action Bar Convention.

var testActions = []Action{
	{Key: "ENTER", Label: "Open", Enabled: true},
	{Key: "M", Label: "Merge", Enabled: false, Reason: "requires Approved"},
	{Key: "B", Label: "Build", Enabled: true, Reason: "requires Merged or later"},
	{Key: "^PgDn/^PgUp", Label: "Cards", Enabled: true},
	{Key: "P", Label: "Publish", Enabled: false, Reason: "requires Confirmed"},
}

func TestRenderActionBarCompact(t *testing.T) {
	got := RenderActionBar(testActions)
	if strings.Contains(got, "\n") {
		t.Fatalf("compact bar spans more than one line:\n%s", got)
	}
	for _, a := range testActions {
		if !strings.Contains(got, a.Key+" "+a.Label) {
			t.Errorf("bar lacks %q: %q", a.Key+" "+a.Label, got)
		}
		if a.Reason != "" && strings.Contains(got, a.Reason) {
			t.Errorf("compact bar shows a reason %q: %q", a.Reason, got)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "? more") {
		t.Errorf("bar does not end with \"? more\": %q", got)
	}
	// Fixed order: actions appear in the order given.
	if strings.Index(got, "ENTER Open") > strings.Index(got, "P Publish") {
		t.Errorf("order not preserved: %q", got)
	}
}

func TestRenderActionHelp(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		got := RenderActionHelp(testActions, width)
		for _, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, line)
			}
		}
		if width < 80 {
			continue // narrower than any real screen: only the width bound applies
		}
		if !strings.Contains(got, "Merge — requires Approved") || !strings.Contains(got, "Publish — requires Confirmed") {
			t.Errorf("width %d: unavailable actions lack their reason:\n%s", width, got)
		}
		if strings.Contains(got, "Build — ") {
			t.Errorf("width %d: available action shows a reason:\n%s", width, got)
		}
		if !strings.Contains(got, "?") || !strings.Contains(got, "Close help") {
			t.Errorf("width %d: no close entry:\n%s", width, got)
		}
		for _, a := range testActions {
			if !strings.Contains(got, a.Label) {
				t.Errorf("width %d: help lacks %q", width, a.Label)
			}
		}
	}
	if narrow, wide := lipgloss.Height(RenderActionHelp(testActions, 40)), lipgloss.Height(RenderActionHelp(testActions, 120)); wide >= narrow {
		t.Errorf("help does not use columns: %d rows at 120 vs %d at 40", wide, narrow)
	}
}
