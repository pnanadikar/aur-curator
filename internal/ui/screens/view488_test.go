package screens

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/git"
)

// pagerFile returns the file a fake pager was given, and its content.
func pagerFile(t *testing.T, f *fakeExec) (string, string) {
	t.Helper()
	if f.cmd == nil {
		t.Fatal("no pager started")
	}
	path := f.cmd.Args[len(f.cmd.Args)-1]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, string(data)
}

// TestReviewViewKey488: v writes the active tab's whole content, without
// colour, to a .diff file on a diff tab and a .txt file on Findings; the
// file is gone after the pager exits.
func TestReviewViewKey488(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PAGER", "")

	for _, tt := range []struct {
		tab, suffix, want string
	}{
		{tabDiff, "-diff.diff", "diff --git a/LICENSE b/LICENSE"},
		{tabInstall, "-install.diff", "+z"},
		{"Findings", "-findings.txt", "Findings point at changes"},
	} {
		f := useFakeExec(t)
		m := gotoTab(t, loaded488(t, t.TempDir(), "base1"), tt.tab)
		if tt.tab == "Findings" {
			m.findingsPane.SetContent("\x1b[2mFindings point at changes\x1b[0m")
		}

		if _, cmd := pressKey(t, m, "v"); cmd == nil {
			t.Fatalf("tab %s: v ran nothing", tt.tab)
		}
		path, content := pagerFile(t, f)
		if !strings.HasSuffix(path, tt.suffix) {
			t.Errorf("tab %s: file %q, want suffix %q", tt.tab, path, tt.suffix)
		}
		if !strings.Contains(content, tt.want) {
			t.Errorf("tab %s: content misses %q:\n%s", tt.tab, tt.want, content)
		}
		if strings.Contains(content, "\x1b") {
			t.Errorf("tab %s: ANSI left in the file: %q", tt.tab, content)
		}
		f.fn(nil)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("tab %s: file not removed", tt.tab)
		}
	}
}

// TestReviewViewKeepsEscapes488: a hidden character in the diff reaches
// the pager as the escape the screen shows, never raw (#491).
func TestReviewViewKeepsEscapes488(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	f := useFakeExec(t)
	m := loaded488(t, t.TempDir(), "base1")
	m.diffPane.SetFiles([]git.FileDiff{{Path: "PKGBUILD", Patch: "diff --git a/PKGBUILD b/PKGBUILD\n+# \x1b[2K\rok \u202e"}})

	pressKey(t, m, "v")
	_, content := pagerFile(t, f)
	if want := `+# \x1b[2K\rok \u202e`; !strings.Contains(content, want) {
		t.Errorf("content misses %q:\n%q", want, content)
	}
	for _, raw := range []string{"\x1b", "\r", "\u202e"} {
		if strings.Contains(content, raw) {
			t.Errorf("raw %q in the pager file", raw)
		}
	}
}

// TestDetailViewKey488: v on Package Details writes the active tab as
// .txt, named after the tab.
func TestDetailViewKey488(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	f := useFakeExec(t)
	m := newTestDetailModel()
	m.tab = TabBuildLog
	m.contentPane.SetContent("\x1b[1m==> Making package\x1b[0m\nline two")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd == nil {
		t.Fatal("v ran nothing")
	}
	_ = updated
	path, content := pagerFile(t, f)
	if !strings.HasSuffix(path, "-build-log.txt") || !strings.Contains(path, "aur-curator-test-pkg-") {
		t.Errorf("path = %q", path)
	}
	if content != "==> Making package\nline two" {
		t.Errorf("content = %q", content)
	}
}

// TestViewKeyWithNothingToShow488: an empty pane opens nothing.
func TestViewKeyWithNothingToShow488(t *testing.T) {
	f := useFakeExec(t)
	m := newTestDetailModel()
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}); cmd != nil || f.cmd != nil {
		t.Error("v opened a pager on an empty pane")
	}
}

// TestViewInActionBars488: v is in both compact bars.
func TestViewInActionBars488(t *testing.T) {
	if v := loaded488(t, t.TempDir(), "base1").View(); !strings.Contains(v, "v View") {
		t.Errorf("Review bar misses v:\n%s", v)
	}
	m := newTestDetailModel()
	m.contentPane.SetContent("summary")
	if v := m.renderActionBar(); !strings.Contains(v, "v View") {
		t.Errorf("Package Details bar misses v:\n%s", v)
	}
}
