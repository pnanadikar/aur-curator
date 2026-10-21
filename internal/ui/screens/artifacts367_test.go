package screens

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	"github.com/pramod/aur-curator/internal/theme"
)

// fakeArtifactStore is a minimal ArtifactStore whose GetArtifactsByPackage
// returns a configured slice and error.
type fakeArtifactStore struct {
	store.ArtifactStore
	arts []domain.Artifact
	err  error
}

func (f *fakeArtifactStore) GetArtifactsByPackage(domain.PackageName) ([]domain.Artifact, error) {
	return f.arts, f.err
}

// detailWithArtifacts builds a model with a BuildEngine backed by the
// given fakeArtifactStore, using the same pattern as detailWithLog.
func detailWithArtifacts(st *fakeArtifactStore) PackageDetailModel {
	m := detailAt(nil, "A")
	m.deps = testDepsWithRoot(deps.Dependencies{
		BuildEngine: build.NewEngine(nil, nil, st, nil, nil),
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	return m
}

// detailWithNilEngine builds a model with a nil BuildEngine.
func detailWithNilEngine() PackageDetailModel {
	m := detailAt(nil, "A")
	m.deps = testDepsWithRoot(deps.Dependencies{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	return m
}

func TestArtifactLoadErrorShowsError(t *testing.T) {
	t.Parallel()

	st := &fakeArtifactStore{err: errors.New("permission denied")}
	m := detailWithArtifacts(st)
	m.tab = TabArtifactHistory
	m.artifactsLoading = true
	m.artifactErr = nil
	cmd := m.tabLoadCmd()
	if cmd == nil {
		t.Fatal("expected a Cmd when loading Artifacts tab")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "Could not load artifacts") {
		t.Fatalf("view missing error message:\n%s", view)
	}
	if strings.Contains(view, "No artifacts recorded") {
		t.Fatal("error view should not contain empty-state message")
	}
	if !strings.Contains(view, "permission denied") {
		t.Fatalf("view missing error details:\n%s", view)
	}
}

func TestArtifactStoreNotConfigured(t *testing.T) {
	t.Parallel()

	m := detailWithNilEngine()
	m.tab = TabArtifactHistory
	m.artifactsLoading = true
	m.artifactErr = nil
	cmd := m.tabLoadCmd()
	if cmd == nil {
		t.Fatal("expected a Cmd when loading Artifacts tab")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "Could not load artifacts") {
		t.Fatalf("view missing error message:\n%s", view)
	}
	if !strings.Contains(view, "artifact store not configured") {
		t.Fatalf("view missing configuration error:\n%s", view)
	}
}

func TestArtifactLoadingState(t *testing.T) {
	t.Parallel()

	st := &fakeArtifactStore{arts: []domain.Artifact{{Filename: "pkg.tar.zst"}}}
	m := detailWithArtifacts(st)
	m.tab = TabArtifactHistory
	m.artifactsLoading = true
	m.artifactErr = nil
	m.refreshContent()
	// The Cmd is returned but not fed yet — view should show loading.
	cmd := m.tabLoadCmd()
	if cmd == nil {
		t.Fatal("expected a Cmd when loading Artifacts tab")
	}
	_ = cmd // don't feed it

	view := m.View()
	if !strings.Contains(view, "Loading artifacts") {
		t.Fatalf("loading view missing loading indicator:\n%s", view)
	}
	if strings.Contains(view, "No artifacts recorded") {
		t.Fatal("loading view should not contain empty-state message")
	}
}

func TestArtifactStaleResultIgnored(t *testing.T) {
	t.Parallel()

	// Build a model for package B and send a stale msg for package A.
	m := newTestDetailModel()
	m.pkgName = "B"
	m.artifactsLoading = true // pretend we were loading

	// Send a stale artifactLoadedMsg for package A.
	m2, _ := m.Update(artifactLoadedMsg{pkg: "A", artifacts: []domain.Artifact{{Filename: "pkg.tar.zst"}}})

	// The stale message should be ignored due to pkg mismatch.
	if m2.artifactHistory != nil {
		t.Fatal("stale message should not have set artifactHistory")
	}
	if m2.artifactsLoading != true {
		t.Fatal("stale message should not have changed artifactsLoading")
	}
	if m2.artifactErr != nil {
		t.Fatal("stale message should not have set artifactErr")
	}
}

func TestArtifactLoadSuccess(t *testing.T) {
	t.Parallel()

	arts := []domain.Artifact{
		{Filename: "pkg-1.0-1-x86_64.pkg.tar.zst", Architecture: "x86_64", Version: "1.0"},
		{Filename: "pkg-debug-1.0-1-x86_64.pkg.tar.zst", Architecture: "x86_64", Version: "1.0"},
	}
	st := &fakeArtifactStore{arts: arts}
	m := detailWithArtifacts(st)
	m.tab = TabArtifactHistory
	m.artifactsLoading = true
	m.artifactErr = nil
	cmd := m.tabLoadCmd()
	if cmd == nil {
		t.Fatal("expected a Cmd when loading Artifacts tab")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "pkg-1.0-1-x86_64.pkg.tar.zst") {
		t.Fatalf("view missing artifact filename:\n%s", view)
	}
	if !strings.Contains(view, "Artifact History") {
		t.Fatalf("view missing artifact header:\n%s", view)
	}
}

func TestArtifactLoadSuccessEmpty(t *testing.T) {
	t.Parallel()

	st := &fakeArtifactStore{arts: []domain.Artifact{}}
	m := detailWithArtifacts(st)
	m.tab = TabArtifactHistory
	m.artifactsLoading = true
	m.artifactErr = nil
	cmd := m.tabLoadCmd()
	if cmd == nil {
		t.Fatal("expected a Cmd when loading Artifacts tab")
	}
	m, _ = m.Update(cmd())

	view := m.View()
	if !strings.Contains(view, "No artifacts recorded") {
		t.Fatalf("view missing empty-state message:\n%s", view)
	}
}

func TestRenderArtifactsViewPartialTwoRecords(t *testing.T) {
	t.Parallel()

	arts := []domain.Artifact{{Filename: "pkg-1.0.pkg.tar.zst"}}
	err := errors.Join(errors.New("artifact a1: decode: x"), errors.New("artifact a2: read: y"))
	view := renderArtifactsView(false, arts, err)
	if !strings.Contains(view, "2 artifact records could not be read") {
		t.Fatalf("view missing record count:\n%s", view)
	}
	if !strings.Contains(view, "may belong to any package") {
		t.Fatalf("view missing package disclaimer:\n%s", view)
	}
	if !strings.Contains(view, "artifact a1: decode: x") {
		t.Fatalf("view missing first error:\n%s", view)
	}
	if !strings.Contains(view, "artifact a2: read: y") {
		t.Fatalf("view missing second error:\n%s", view)
	}
	if !strings.Contains(view, "pkg-1.0.pkg.tar.zst") {
		t.Fatalf("view missing artifact filename:\n%s", view)
	}
	if strings.Contains(view, "; ") {
		t.Fatalf("partial errors should not contain '; ': %s", view)
	}
	if strings.Contains(view, "Could not load artifacts") {
		t.Fatalf("partial errors should not show full-load message:\n%s", view)
	}
}

func TestRenderArtifactsViewPartialOneRecordEmptyList(t *testing.T) {
	t.Parallel()

	var arts []domain.Artifact
	err := errors.Join(errors.New("artifact a1: decode: x"))
	view := renderArtifactsView(false, arts, err)
	if !strings.Contains(view, "1 artifact record could not be read") {
		t.Fatalf("view missing single-record header:\n%s", view)
	}
	if !strings.Contains(view, "artifact a1: decode: x") {
		t.Fatalf("view missing error:\n%s", view)
	}
	if !strings.Contains(view, "No artifacts recorded") {
		t.Fatalf("view missing empty-state:\n%s", view)
	}
	if strings.Contains(view, "Could not load artifacts") {
		t.Fatalf("partial errors should not show full-load message:\n%s", view)
	}
}

func TestRenderArtifactsViewWholeStoreFailure(t *testing.T) {
	t.Parallel()

	var arts []domain.Artifact
	err := errors.New("read artifacts directory: permission denied")
	view := renderArtifactsView(false, arts, err)
	if !strings.Contains(view, "Could not load artifacts:") {
		t.Fatalf("view missing full-load error:\n%s", view)
	}
	if !strings.Contains(view, "permission denied") {
		t.Fatalf("view missing error detail:\n%s", view)
	}
	if strings.Contains(view, "could not be read") {
		t.Fatalf("whole-store error should not show per-record message:\n%s", view)
	}
	if strings.Contains(view, "No artifacts recorded") {
		t.Fatalf("whole-store error should not show empty-state:\n%s", view)
	}
}

func TestRenderArtifactsViewSeparateLines(t *testing.T) {
	t.Parallel()

	err := errors.Join(errors.New("artifact a1: decode: x"), errors.New("artifact a2: read: y"))
	view := renderArtifactsView(false, nil, err)
	lines := strings.Split(view, "\n")
	i1, i2 := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "artifact a1: decode: x") {
			i1 = i
		}
		if strings.Contains(line, "artifact a2: read: y") {
			i2 = i
		}
	}
	if i1 == -1 {
		t.Fatalf("first error not found in any line:\n%s", view)
	}
	if i2 == -1 {
		t.Fatalf("second error not found in any line:\n%s", view)
	}
	if i1 == i2 {
		t.Fatalf("both errors found on same line %d:\n%s", i1, view)
	}
}

func TestRenderArtifactsViewEmptySuccess(t *testing.T) {
	t.Parallel()

	view := renderArtifactsView(false, []domain.Artifact{}, nil)
	if !strings.Contains(view, "No artifacts recorded") {
		t.Fatalf("view missing empty-state message:\n%s", view)
	}
}
