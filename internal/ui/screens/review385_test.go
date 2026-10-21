package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// Tests for #385: the .SRCINFO, .install and Patches tabs show their files
// and are enabled exactly when the review changes a file of their kind.

func fd385(path string, kind git.FileKind) git.FileDiff {
	return git.FileDiff{Path: path, Kind: kind, Patch: "diff --git a/" + path + " b/" + path + "\n+changed " + path}
}

var (
	f385PKGBUILD = fd385("PKGBUILD", git.FileKindPKGBUILD)
	f385SRCINFO  = fd385(".SRCINFO", git.FileKindSRCINFO)
	f385Install  = fd385("yay.install", git.FileKindInstall)
	f385Patch    = fd385("fix-build.patch", git.FileKindPatch)
	f385Other    = fd385("LICENSE", git.FileKindOther)
)

func paths385(files []git.FileDiff) string {
	var p []string
	for _, f := range files {
		p = append(p, f.Path)
	}
	return strings.Join(p, ",")
}

func TestTabFiles385(t *testing.T) {
	t.Parallel()
	diff := git.GitDiff{Files: []git.FileDiff{f385PKGBUILD, f385SRCINFO, f385Install, f385Patch, f385Other}}
	tests := []struct {
		label string
		want  string
	}{
		{tabDiff, "PKGBUILD,LICENSE"},
		{tabSRCINFO, ".SRCINFO"},
		{tabInstall, "yay.install"},
		{tabPatches, "fix-build.patch"},
		{"Findings", ""},
	}
	for _, tt := range tests {
		if got := paths385(tabFiles(diff, tt.label)); got != tt.want {
			t.Errorf("tabFiles(%q) = %q, want %q", tt.label, got, tt.want)
		}
	}
}

// enabledTabs385 walks the tab bar with Next and returns the labels it can
// reach, in order, starting from the active tab.
func enabledTabs385(m ReviewScreen) string {
	start := m.tabs.Active().Label
	seen := []string{start}
	for i := 0; i < 10; i++ {
		m.tabs.Next()
		if m.tabs.Active().Label == start {
			break
		}
		seen = append(seen, m.tabs.Active().Label)
	}
	return strings.Join(seen, ",")
}

func newScreen385(t *testing.T) ReviewScreen {
	t.Helper()
	m := NewReviewScreen("yay", nil, nil, nil, domain.WorkspaceConfig{})
	m.loading = false
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(ReviewScreen)
}

func TestSetDiffEnablesFileTabs385(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files []git.FileDiff
		want  string
	}{
		{"no changes", nil, "Diff,Findings"},
		{"version bump", []git.FileDiff{f385PKGBUILD, f385SRCINFO}, "Diff,.SRCINFO,Findings"},
		{"install script changed", []git.FileDiff{f385PKGBUILD, f385Install}, "Diff,.install,Findings"},
		{"patch only", []git.FileDiff{f385Patch}, "Diff,Patches,Findings"},
		{"import, every kind present", []git.FileDiff{f385PKGBUILD, f385SRCINFO, f385Install, f385Patch, f385Other}, "Diff,.SRCINFO,.install,Patches,Findings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newScreen385(t)
			m.setDiff(git.GitDiff{Files: tt.files})
			if got := enabledTabs385(m); got != tt.want {
				t.Fatalf("enabled tabs = %q, want %q", got, tt.want)
			}
		})
	}
}

func pressNextTab385(t *testing.T, m ReviewScreen) ReviewScreen {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	return next.(ReviewScreen)
}

// TestTabSwitchShowsTabFiles: switching tabs refills the one diff pane with
// that tab's files only.
func TestTabSwitchShowsTabFiles385(t *testing.T) {
	t.Parallel()
	m := newScreen385(t)
	m.setDiff(git.GitDiff{Files: []git.FileDiff{f385PKGBUILD, f385SRCINFO}})

	if v := m.diffPane.View(); !strings.Contains(v, "changed PKGBUILD") || strings.Contains(v, "changed .SRCINFO") {
		t.Fatalf("Diff tab should show PKGBUILD only:\n%s", v)
	}

	m = pressNextTab385(t, m)
	if got := m.tabs.Active().Label; got != tabSRCINFO {
		t.Fatalf("active = %q, want %q", got, tabSRCINFO)
	}
	if v := m.diffPane.View(); !strings.Contains(v, "changed .SRCINFO") || strings.Contains(v, "changed PKGBUILD") {
		t.Fatalf(".SRCINFO tab should show .SRCINFO only:\n%s", v)
	}
}

// TestReloadDimmingActiveTabFallsBackToDiff: a reloaded diff that no longer
// touches the active tab's kind moves back to Diff and shows its files.
func TestReloadDimmingActiveTabFallsBackToDiff385(t *testing.T) {
	t.Parallel()
	m := newScreen385(t)
	m.setDiff(git.GitDiff{Files: []git.FileDiff{f385PKGBUILD, f385SRCINFO}})
	m = pressNextTab385(t, m)

	m.setDiff(git.GitDiff{Files: []git.FileDiff{f385PKGBUILD}})
	if got := m.tabs.Active().Label; got != tabDiff {
		t.Fatalf("active = %q, want %q", got, tabDiff)
	}
	if v := m.diffPane.View(); !strings.Contains(v, "changed PKGBUILD") {
		t.Fatalf("Diff tab content not shown after fallback:\n%s", v)
	}
}

// TestDiffTabEmptyWhenOnlyOtherKindsChanged: if only .SRCINFO changed, the
// Diff tab must not claim the reviewed range has no changes. The no-change
// text names the reviewed range, not master and upstream/master (#387).
func TestDiffTabEmptyWhenOnlyOtherKindsChanged385(t *testing.T) {
	t.Parallel()
	m := newScreen385(t)
	m.setDiff(git.GitDiff{Files: []git.FileDiff{f385SRCINFO}})
	if v := m.diffPane.View(); strings.Contains(v, "No changes in the reviewed range") || !strings.Contains(v, "see the other tabs") {
		t.Fatalf("Diff tab empty text wrong:\n%s", v)
	}

	m.setDiff(git.GitDiff{})
	if v := m.diffPane.View(); !strings.Contains(v, "No changes in the reviewed range.") {
		t.Fatalf("no-change empty text wrong:\n%s", v)
	}
}
