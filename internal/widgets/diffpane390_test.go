package widgets

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// Tests for #390: no empty-state text claims master and upstream/master
// are identical; DiffPane does not know which refs were compared.

func TestDiffPaneDefaultEmptyTextNamesNoRefs390(t *testing.T) {
	t.Parallel()
	d := NewDiffPane()
	d.SetSize(80, 5)
	d.SetFiles(nil)
	v := d.View()
	if !strings.Contains(v, "No changes.") {
		t.Fatalf("default empty text = %q, want it to contain %q", v, "No changes.")
	}
	for _, bad := range []string{"identical", "master", "upstream"} {
		if strings.Contains(v, bad) {
			t.Fatalf("default empty text mentions %q:\n%s", bad, v)
		}
	}
}

func TestDiffPaneEmptyPatchNamesFile390(t *testing.T) {
	t.Parallel()
	d := NewDiffPane()
	d.SetSize(80, 5)
	d.SetFiles([]git.FileDiff{{Path: "icon.png", Patch: ""}})
	v := d.View()
	if !strings.Contains(v, "No text changes in icon.png.") {
		t.Fatalf("empty-patch file: got\n%s\nwant it to contain %q", v, "No text changes in icon.png.")
	}
	if strings.Contains(v, "identical") {
		t.Fatalf("empty-patch file still claims identical:\n%s", v)
	}
}

// An empty-patch file among others keeps its place and the separator rule.
func TestDiffPaneEmptyPatchAmongFiles390(t *testing.T) {
	t.Parallel()
	rule := strings.Repeat("─", 30)
	d := NewDiffPane()
	d.SetSize(30, 20)
	d.SetFiles([]git.FileDiff{
		{Path: "PKGBUILD", Patch: "diff --git a/PKGBUILD b/PKGBUILD\n+pkgver=2"},
		{Path: "icon.png", Patch: "  \n"},
	})
	lines := strings.Split(d.View(), "\n")
	ruleAt, emptyAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, rule) && ruleAt < 0 {
			ruleAt = i
		}
		if strings.Contains(l, "No text changes in icon.png.") {
			emptyAt = i
		}
	}
	if ruleAt < 0 || emptyAt != ruleAt+1 {
		t.Fatalf("want the rule directly above the empty-patch line (rule at %d, line at %d):\n%s", ruleAt, emptyAt, d.View())
	}
}
