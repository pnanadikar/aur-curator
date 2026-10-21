package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/widgets"
)

func TestDetailContentFollowsTab(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.width = 200
	m.review = &domain.Review{Status: domain.ReviewPending}
	m.diff = git.GitDiff{Commits: []git.Commit{{Hash: "abc123", Subject: "s"}}}
	m.syncLayout()
	m.refreshContent()

	for i := 0; i < 4; i++ {
		if got := lipgloss.Height(m.View()); got != m.height {
			t.Fatalf("tab %v: view height = %d, want %d", m.tab, got, m.height)
		}
		m = switchTabNext(m)
	}

	// A late build-log load while on Summary must not replace Summary.
	m.tab = TabSummary
	m.refreshContent()
	m, _ = m.Update(buildLogLoadedMsg{content: "late log"})
	if strings.Contains(m.View(), "late log") {
		t.Fatal("buildLogLoadedMsg overwrote Summary content")
	}
}

func TestDetailTabSwitchRestoresCommitsContent(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabCommits
	m.diff = git.GitDiff{
		Commits: []git.Commit{
			{
				Hash:    "abc123",
				Author:  "Alice",
				Subject: "Initial commit",
				Date:    "2026-01-01",
			},
		},
	}
	m.buildLogContent = "build log content"
	m.artifactHistory = []domain.Artifact{
		{Filename: "pkg-1.0.tar.gz"},
	}
	m.syncLayout()

	// Switch to Build Log then back to Commits.
	m = switchTabNext(m)
	if m.tab != TabBuildLog {
		t.Fatalf("tab = %v, want TabBuildLog", m.tab)
	}
	m = switchTabNext(m)
	if m.tab != TabArtifactHistory {
		t.Fatalf("tab = %v, want TabArtifactHistory", m.tab)
	}
	m = switchTabNext(m)
	if m.tab != TabSummary {
		t.Fatalf("tab = %v, want TabSummary", m.tab)
	}
	m = switchTabNext(m)
	if m.tab != TabCommits {
		t.Fatalf("tab = %v, want TabCommits", m.tab)
	}

	view := m.View()
	if !strings.Contains(view, "abc123") {
		t.Fatal("commits tab should show commit hash after tab switch cycle")
	}
	if !strings.Contains(view, "Initial commit") {
		t.Fatal("commits tab should show commit subject after tab switch cycle")
	}
}

func TestDetailTabSwitchToBuildLogShowsContent(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabCommits
	m.buildLogContent = "=== build output ===\nmaking package..."
	m.syncLayout()

	// Switch to Build Log.
	m = switchTabNext(m)
	if m.tab != TabBuildLog {
		t.Fatalf("tab = %v, want TabBuildLog", m.tab)
	}

	view := m.View()
	if !strings.Contains(view, "=== build output ===") {
		t.Fatal("build log tab should show build log content after switch")
	}
	if !strings.Contains(view, "making package") {
		t.Fatal("build log tab should show build log content after switch")
	}
}

func TestDetailTabSwitchToArtifactsShowsContent(t *testing.T) {
	t.Parallel()

	fakeStore := &fakeArtifactStore{arts: []domain.Artifact{
		{Filename: "pkg-1.0-1-x86_64.pkg.tar.zst", Architecture: "x86_64", Version: "1.0"},
	}}
	m := newTestDetailModel()
	m.deps = testDepsWithRoot(deps.Dependencies{
		BuildEngine: build.NewEngine(nil, nil, fakeStore, nil, nil),
	})
	m.syncLayout()

	// Switch to Artifacts via keys; the returned Cmd starts the load.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	if m.tab != TabArtifactHistory {
		t.Fatalf("tab = %v, want TabArtifactHistory", m.tab)
	}
	if cmd == nil {
		t.Fatal("expected a Cmd when switching to Artifacts tab")
	}
	if !strings.Contains(m.View(), "Loading artifacts") {
		t.Fatal("should show loading before Cmd is fed")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "pkg-1.0-1-x86_64.pkg.tar.zst") {
		t.Fatal("artifacts tab should show artifact filename after switch")
	}
	if !strings.Contains(view, "Artifact History") {
		t.Fatal("artifacts tab should show artifact header after switch")
	}
}

func TestDetailTabSwitchEmptyBuildLogShowsEmptyState(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabCommits
	m.syncLayout()

	// Switch to Build Log with no build log content set.
	m = switchTabNext(m)
	if m.tab != TabBuildLog {
		t.Fatalf("tab = %v, want TabBuildLog", m.tab)
	}

	view := m.View()
	if !strings.Contains(view, "No build log available") {
		t.Fatal("build log tab with no content should show empty state message")
	}
}

func TestDetailTabSwitchEmptyArtifactsShowsEmptyState(t *testing.T) {
	t.Parallel()

	fakeStore := &fakeArtifactStore{arts: []domain.Artifact{}}
	m := newTestDetailModel()
	m.tab = TabCommits
	m.deps = testDepsWithRoot(deps.Dependencies{
		BuildEngine: build.NewEngine(nil, nil, fakeStore, nil, nil),
	})
	m.syncLayout()

	// Switch to Build Log — discard the Cmd since we only care about reaching Artifacts.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})

	// Switch to Artifacts via key; the returned Cmd starts the artifact load.
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	if m.tab != TabArtifactHistory {
		t.Fatalf("tab = %v, want TabArtifactHistory", m.tab)
	}
	if cmd == nil {
		t.Fatal("expected a Cmd when switching to Artifacts tab")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "No artifacts recorded") {
		t.Fatal("artifacts tab with no data should show empty state message")
	}
}

func TestDetailTabSwitchEmptyCommitsShowsEmptyState(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabSummary
	m.diff = git.GitDiff{Commits: []git.Commit{}}
	m.syncLayout()

	// Switch to Commits with no commits.
	m = switchTabNext(m)
	if m.tab != TabCommits {
		t.Fatalf("tab = %v, want TabCommits", m.tab)
	}

	view := m.View()
	if !strings.Contains(view, "No commits in diff") {
		t.Fatal("commits tab with no commits should show empty state message")
	}
}

func TestDetailNextTabKeyCyclesAllTabs(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabSummary
	m.diff = git.GitDiff{
		Commits: []git.Commit{{Hash: "aaa", Author: "A", Subject: "First", Date: "2026-01-01"}},
	}
	m.syncLayout()

	expectedTabs := []DetailTab{TabCommits, TabBuildLog, TabArtifactHistory, TabSummary}
	for _, wantTab := range expectedTabs {
		m = switchTabNext(m)
		if m.tab != wantTab {
			t.Fatalf("after nextTab: tab = %v, want %v", m.tab, wantTab)
		}
	}
}

func TestDetailPrevTabKeyCyclesAllTabs(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.tab = TabSummary
	m.diff = git.GitDiff{
		Commits: []git.Commit{{Hash: "aaa", Author: "A", Subject: "First", Date: "2026-01-01"}},
	}
	m.syncLayout()

	expectedTabs := []DetailTab{TabArtifactHistory, TabBuildLog, TabCommits, TabSummary}
	for _, wantTab := range expectedTabs {
		m = switchTabPrev(m)
		if m.tab != wantTab {
			t.Fatalf("after prevTab: tab = %v, want %v", m.tab, wantTab)
		}
	}
}

func TestRenderBuildLogContentStripsCR(t *testing.T) {
	t.Parallel()

	result := renderBuildLogContent("x\r\n\r 0 a\r100 b\n")
	if strings.Contains(result, "\r") {
		t.Error("renderBuildLogContent should contain no \\r characters")
	}
	if !strings.Contains(result, "100 b") {
		t.Error("renderBuildLogContent should contain '100 b' (last segment after \\r)")
	}
}

func TestDetailViewFitsAllocatedHeight(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.width = 200
	m.syncLayout()
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("view height = %d, want %d\n%s", got, m.height, m.View())
	}

	m.opFailure = &opFailure{kind: "Merge", err: fmt.Errorf("boom")}
	m.syncLayout()
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("view height = %d with failure banner, want %d\n%s", got, m.height, m.View())
	}
}

func TestDetailSummaryTabNoReview(t *testing.T) {
	t.Parallel()

	m := newTestDetailModel()
	m.review = nil
	m.tab = TabSummary
	m.syncLayout()
	m.refreshContent()

	view := m.View()
	if !strings.Contains(view, "No review") {
		t.Errorf("Summary tab missing \"No review\", view:\n%s", view)
	}
	if strings.Contains(view, "Not Run") {
		t.Errorf("Summary tab should not contain \"Not Run\" when m.review == nil, view:\n%s", view)
	}
}

// --- helpers ---

func newTestDetailModel() PackageDetailModel {
	return PackageDetailModel{
		deps:        deps.Dependencies{},
		pkgName:     "test-pkg",
		tab:         TabSummary,
		width:       120,
		height:      40,
		contentPane: widgets.NewContentPane(),
		keys:        newDetailKeys(),
	}
}

func switchTabNext(m PackageDetailModel) PackageDetailModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	return updated
}

func switchTabPrev(m PackageDetailModel) PackageDetailModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlPgUp})
	return updated
}
