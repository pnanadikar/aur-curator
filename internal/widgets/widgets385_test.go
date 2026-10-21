package widgets

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// Tests for #385: tabs enabled after construction, and the diff pane's
// settable empty text.

func newReviewTabs385() TabBar {
	return NewTabBar([]Tab{
		{Label: "Diff", Enabled: true},
		{Label: ".SRCINFO", Enabled: false},
		{Label: ".install", Enabled: false},
		{Label: "Patches", Enabled: false},
		{Label: "Findings", Enabled: true},
	})
}

func TestTabBarSetEnabledMakesTabReachable385(t *testing.T) {
	t.Parallel()
	bar := newReviewTabs385()
	bar.Next()
	if got := bar.Active().Label; got != "Findings" {
		t.Fatalf("before SetEnabled: Next -> %q, want Findings", got)
	}

	bar = newReviewTabs385()
	bar.SetEnabled(".SRCINFO", true)
	bar.Next()
	if got := bar.Active().Label; got != ".SRCINFO" {
		t.Fatalf("after SetEnabled: Next -> %q, want .SRCINFO", got)
	}
}

func TestTabBarSetEnabledDimmingActiveTabFallsBack385(t *testing.T) {
	t.Parallel()
	bar := newReviewTabs385()
	bar.SetEnabled(".install", true)
	bar.Next() // .install
	if got := bar.Active().Label; got != ".install" {
		t.Fatalf("active = %q, want .install", got)
	}
	bar.SetEnabled(".install", false)
	if got := bar.Active(); got.Label != "Diff" || !got.Enabled {
		t.Fatalf("after dimming the active tab: active = %+v, want enabled Diff", got)
	}
}

func TestTabBarSetEnabledUnknownLabelIsNoOp385(t *testing.T) {
	t.Parallel()
	bar := newReviewTabs385()
	bar.SetEnabled("Nope", true)
	bar.Next()
	if got := bar.Active().Label; got != "Findings" {
		t.Fatalf("Next -> %q, want Findings", got)
	}
}

func TestDiffPaneEmptyText385(t *testing.T) {
	t.Parallel()
	d := NewDiffPane()
	d.SetSize(80, 5)
	d.SetFiles(nil)
	if !strings.Contains(d.View(), defaultEmptyText) {
		t.Fatalf("default empty text missing:\n%s", d.View())
	}

	d.SetEmptyText("nothing on this tab")
	d.SetFiles(nil)
	if v := d.View(); !strings.Contains(v, "nothing on this tab") || strings.Contains(v, defaultEmptyText) {
		t.Fatalf("custom empty text not shown:\n%s", v)
	}
}

// TestDiffPaneSeparatesFiles385: a rule is drawn between files, never
// before the first one.
func TestDiffPaneSeparatesFiles385(t *testing.T) {
	t.Parallel()
	rule := strings.Repeat("─", 20)
	d := NewDiffPane()
	d.SetSize(20, 20)

	d.SetFiles([]git.FileDiff{{Path: "a", Patch: "diff --git a/a b/a\n+one"}})
	if strings.Contains(d.View(), rule) {
		t.Fatalf("single file must have no separator:\n%s", d.View())
	}

	d.SetFiles([]git.FileDiff{
		{Path: "a", Patch: "diff --git a/a b/a\n+one"},
		{Path: "b", Patch: "diff --git a/b b/b\n+two"},
	})
	lines := strings.Split(d.View(), "\n")
	ruleAt, secondAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, rule) && ruleAt < 0 {
			ruleAt = i
		}
		if strings.Contains(l, "diff --git a/b") {
			secondAt = i
		}
	}
	if ruleAt < 0 || secondAt != ruleAt+1 {
		t.Fatalf("want a rule directly above the second file (rule at %d, second file at %d):\n%s", ruleAt, secondAt, d.View())
	}
}
