package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// Tests for #360: a build's disposable workspace is removed only on full
// success or cancel; any failure keeps it and records where it is, and a
// failure to save the build after its artifacts are recorded is recorded
// as a failed build.

// failingSaveBuildStore wraps the JSON build store and fails chosen saves.
type failingSaveBuildStore struct {
	*jsonstore.BuildStore
	saves           int
	failSaveN       int  // 1-based Save call to fail; 0 = never
	failLastSuccess bool // SaveLastSuccess always fails
}

func (s *failingSaveBuildStore) Save(pkg domain.PackageName, b *domain.Build) error {
	s.saves++
	if s.saves == s.failSaveN {
		return errors.New("simulated save failure")
	}
	return s.BuildStore.Save(pkg, b)
}

func (s *failingSaveBuildStore) SaveLastSuccess(pkg domain.PackageName, b *domain.Build) error {
	if s.failLastSuccess {
		return errors.New("simulated last-success save failure")
	}
	return s.BuildStore.SaveLastSuccess(pkg, b)
}

// assertTempEmpty checks that a build left nothing in the system temp
// directory: workspaces live under .aur-curator/builds/<id>/work (#307).
func assertTempEmpty(t *testing.T, tmpDir string) {
	t.Helper()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir not empty: %v", entries)
	}
}

// assertKeptAndNoted checks that b's workspace was kept inside its build
// directory, that the build log names it, and that nothing was left in
// tmpDir.
func assertKeptAndNoted(t *testing.T, tmpDir string, paths *store.StorePaths, b *domain.Build) {
	t.Helper()
	assertTempEmpty(t, tmpDir)
	dir := paths.BuildWorkDir(b.ID)
	if _, err := os.Stat(filepath.Join(dir, "src")); err != nil {
		t.Fatalf("workspace not kept at %s: %v", dir, err)
	}
	data, err := os.ReadFile(b.LogLocation)
	if err != nil {
		t.Fatalf("read build log %s: %v", b.LogLocation, err)
	}
	want := "Build workspace kept for inspection: " + dir
	if !strings.Contains(string(data), want) {
		t.Fatalf("build log lacks %q:\n%s", want, data)
	}
}

// TestRunBuildFailureKeepsWorkspace: a makepkg failure keeps the workspace
// and appends its path to the build log.
func TestRunBuildFailureKeepsWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := jsonstore.NewBuildStore(paths)

	builder := newFakeBuilder("content")
	builder.failNext = true
	engine := NewEngine(builder, bstore, jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths)
	if _, err := engine.RunBuild(context.Background(), "failpkg"); err == nil {
		t.Fatal("expected error from failed build")
	}

	loaded, err := bstore.Get("failpkg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	assertKeptAndNoted(t, tmpDir, paths, loaded)
}

// TestRunBuildCopyFailureKeepsWorkspace: a failure after makepkg succeeded
// (here, copying an artifact) keeps the workspace too. Before #360 it was
// removed.
func TestRunBuildCopyFailureKeepsWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := jsonstore.NewBuildStore(paths)

	builder := newFakeBuilder("content")
	builder.badArtifact = true
	engine := NewEngine(builder, bstore, jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths)
	if _, err := engine.RunBuild(context.Background(), "copypkg"); err == nil {
		t.Fatal("expected error from bad artifact")
	}

	loaded, err := bstore.Get("copypkg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Status != domain.BuildFailed {
		t.Fatalf("status = %q, want %q", loaded.Status, domain.BuildFailed)
	}
	assertKeptAndNoted(t, tmpDir, paths, loaded)
}

// TestRunBuildSuccessRemovesWorkspace: a full success removes the
// workspace and keeps the rest of the build's directory.
func TestRunBuildSuccessRemovesWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)

	engine := NewEngine(newFakeBuilder("content"), jsonstore.NewBuildStore(paths), jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths)
	b, err := engine.RunBuild(context.Background(), "okpkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	assertTempEmpty(t, tmpDir)
	if _, err := os.Stat(paths.BuildWorkDir(b.ID)); !os.IsNotExist(err) {
		t.Fatalf("workspace %s still exists after success (err=%v)", paths.BuildWorkDir(b.ID), err)
	}
	if _, err := os.Stat(paths.BuildLogPath(b.ID)); err != nil {
		t.Fatalf("build log removed with the workspace: %v", err)
	}
}

// saveFailureCase runs a successful build 1, then build 2 with store as
// the build store, and checks build 2 is recorded as a failure that leaves
// build 1 as the last success with its artifacts intact.
func saveFailureCase(t *testing.T, failing *failingSaveBuildStore, wantErr string) {
	t.Helper()
	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}

	build1, err := NewEngine(newFakeBuilder("v1"), bstore, artStore, gitProvider, paths).RunBuild(context.Background(), "savepkg")
	if err != nil {
		t.Fatalf("build 1: %v", err)
	}

	failing.BuildStore = bstore
	_, err = NewEngine(newFakeBuilder("v2"), failing, artStore, gitProvider, paths).RunBuild(context.Background(), "savepkg")
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("build 2 error = %v, want one containing %q", err, wantErr)
	}

	loaded, err := bstore.Get("savepkg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Status != domain.BuildFailed {
		t.Errorf("status = %q, want %q", loaded.Status, domain.BuildFailed)
	}
	if loaded.LastSuccessBuildID != build1.ID {
		t.Errorf("LastSuccessBuildID = %q, want build 1 %q", loaded.LastSuccessBuildID, build1.ID)
	}
	if loaded.LastFailureBuildID != loaded.ID {
		t.Errorf("LastFailureBuildID = %q, want %q", loaded.LastFailureBuildID, loaded.ID)
	}
	if len(loaded.ArtifactIDs) != 0 {
		t.Errorf("ArtifactIDs = %v, want none on a failed build", loaded.ArtifactIDs)
	}

	ls, err := bstore.GetLastSuccess("savepkg")
	if err != nil {
		t.Fatalf("GetLastSuccess: %v", err)
	}
	if ls.ID != build1.ID {
		t.Errorf("last success = %q, want build 1 %q", ls.ID, build1.ID)
	}

	arts, err := artStore.GetArtifactsByPackage("savepkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage: %v", err)
	}
	if len(arts) != 1 || arts[0].BuildID != build1.ID {
		t.Errorf("artifact records = %v, want only build 1's", arts)
	}
}

// TestRunBuildFinalSaveFailureRecordsFailure: the final save of the
// successful build fails (save 3: pending, running, final).
func TestRunBuildFinalSaveFailureRecordsFailure(t *testing.T) {
	t.Parallel()
	saveFailureCase(t, &failingSaveBuildStore{failSaveN: 3}, "save success build")
}

// TestRunBuildLastSuccessSaveFailureRecordsFailure: build.json is saved as
// a success, but last-success.json cannot be; the build must end up
// recorded as failed, never naming itself as the last success.
func TestRunBuildLastSuccessSaveFailureRecordsFailure(t *testing.T) {
	t.Parallel()
	saveFailureCase(t, &failingSaveBuildStore{failLastSuccess: true}, "save last success")
}
