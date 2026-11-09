package screens

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// loaded488 is a Review screen for "foo" in workspace root, loaded with a
// review of base → up1 and a diff of PKGBUILD and LICENSE (Diff tab) and
// foo.install (.install tab).
func loaded488(t *testing.T, root, base string) ReviewScreen {
	t.Helper()
	model, _ := newTestReviewScreen(t)
	model.pkgName = "foo"
	model.config = domain.WorkspaceConfig{RootPath: root}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	updated, _ = updated.Update(reviewLoadedMsg{
		diff: git.GitDiff{BaseCommit: base, UpstreamCommit: "up1", Files: []git.FileDiff{
			{Path: "PKGBUILD", Kind: git.FileKindPKGBUILD, Patch: "diff --git a/PKGBUILD b/PKGBUILD\n+x"},
			{Path: "LICENSE", Kind: git.FileKindOther, Patch: "diff --git a/LICENSE b/LICENSE\n+y"},
			{Path: "foo.install", Kind: git.FileKindInstall, Patch: "diff --git a/foo.install b/foo.install\n+z"},
		}},
		review: &domain.Review{PackageName: "foo", Status: domain.ReviewApproved, BaseCommit: base, UpstreamCommit: "up1"},
	})
	return updated
}

func pressKey(t *testing.T, m ReviewScreen, k string) (ReviewScreen, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	return updated, cmd
}

func gotoTab(t *testing.T, m ReviewScreen, label string) ReviewScreen {
	t.Helper()
	for i := 0; i < 6 && m.tabs.Active().Label != label; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
		m = updated
	}
	if m.tabs.Active().Label != label {
		t.Fatalf("could not reach tab %q", label)
	}
	return m
}

// TestDifftoolKey488: d runs git difftool over the stored range for every
// file of the active tab, in the package's clone; a decided review too.
func TestDifftoolKey488(t *testing.T) {
	root := t.TempDir()
	for _, tt := range []struct {
		tab   string
		base  string
		paths []string
	}{
		{tabDiff, "base1", []string{"PKGBUILD", "LICENSE"}},
		{tabInstall, "base1", []string{"foo.install"}},
		{tabDiff, "", []string{"PKGBUILD", "LICENSE"}}, // Import: empty tree
	} {
		f := useFakeExec(t)
		m := gotoTab(t, loaded488(t, root, tt.base), tt.tab)

		_, cmd := pressKey(t, m, "d")
		if cmd == nil || f.cmd == nil {
			t.Fatalf("tab %s: d ran nothing", tt.tab)
		}
		base := tt.base
		if base == "" {
			base = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
		}
		want := append([]string{"git", "difftool", base, "up1", "--"}, tt.paths...)
		if !reflect.DeepEqual(f.cmd.Args, want) {
			t.Errorf("tab %s: Args = %q, want %q", tt.tab, f.cmd.Args, want)
		}
		if f.cmd.Dir != filepath.Join(root, "foo") {
			t.Errorf("tab %s: Dir = %q", tt.tab, f.cmd.Dir)
		}
		if msg, _ := f.fn(nil).(msgs.ToolDoneMsg); msg.What != "Diff tool" || msg.Pkg != "foo" {
			t.Errorf("tab %s: done msg = %+v", tt.tab, msg)
		}
	}
}

// TestDifftoolKeyDisabledOnFindings488: no diff on Findings, so d does
// nothing; the full help gives the reason.
func TestDifftoolKeyDisabledOnFindings488(t *testing.T) {
	f := useFakeExec(t)
	m := gotoTab(t, loaded488(t, t.TempDir(), "base1"), "Findings")

	if _, cmd := pressKey(t, m, "d"); cmd != nil || f.cmd != nil {
		t.Error("d ran a command on Findings")
	}
	m, _ = pressKey(t, m, "?")
	if v := m.View(); !strings.Contains(v, "Diff tool — open a tab with a diff") {
		t.Errorf("full help misses the reason:\n%s", v)
	}
}

// TestDifftoolInActionBar488: d is in the compact bar on a diff tab.
func TestDifftoolInActionBar488(t *testing.T) {
	m := loaded488(t, t.TempDir(), "base1")
	if v := m.View(); !strings.Contains(v, "d Diff tool") {
		t.Errorf("action bar misses d:\n%s", v)
	}
}
