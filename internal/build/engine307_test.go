package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// Tests for #307: a build's workspace lives in its own build directory,
// .aur-curator/builds/<id>/work, never in the system temp directory. A
// workspace kept by a failure (#360) or left by a hard kill is removed
// with that directory when the package's next failure purges it.

// cloneFailingGit is the build fake git provider with a failing CloneAt.
type cloneFailingGit struct {
	*fakeGitProviderForBuild
}

func (cloneFailingGit) CloneAt(context.Context, domain.PackageName, string, string) error {
	return errors.New("simulated clone failure")
}

// assertGone checks that path does not exist.
func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists (err=%v)", path, err)
	}
}

// assertExists checks that path exists.
func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s missing: %v", path, err)
	}
}

// assertNoBuildDirs checks that .aur-curator/builds is absent or empty.
func assertNoBuildDirs(t *testing.T, paths *store.StorePaths) {
	t.Helper()
	entries, err := os.ReadDir(paths.BuildsDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read builds dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("builds dir not empty: %v", entries)
	}
}

// TestRunBuildWorkspaceInBuildDir: the Builder runs in
// builds/<id>/work/src, and nothing is created in the system temp
// directory.
func TestRunBuildWorkspaceInBuildDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	paths := store.NewStorePaths(t.TempDir())

	builder := newFakeBuilder("content")
	engine := NewEngine(builder, jsonstore.NewBuildStore(paths), jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths)
	b, err := engine.RunBuild(context.Background(), "wspkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	want := filepath.Join(paths.BuildWorkDir(b.ID), "src")
	if builder.gotWorkspace != want {
		t.Fatalf("builder workspace = %q, want %q", builder.gotWorkspace, want)
	}
	assertTempEmpty(t, tmpDir)
}

// TestNextFailurePurgesKeptWorkspace: a failure keeps its workspace until
// the package's next failure, which removes it with the old build
// directory and keeps its own.
func TestNextFailurePurgesKeptWorkspace(t *testing.T) {
	t.Parallel()
	paths := store.NewStorePaths(t.TempDir())
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}

	failing := func() *domain.Build {
		t.Helper()
		b := newFakeBuilder("content")
		b.failNext = true
		if _, err := NewEngine(b, bstore, artStore, gitProvider, paths).RunBuild(context.Background(), "retainpkg"); err == nil {
			t.Fatal("expected failure")
		}
		loaded, err := bstore.Get("retainpkg")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return loaded
	}

	first := failing()
	assertExists(t, filepath.Join(paths.BuildWorkDir(first.ID), "src"))

	second := failing()
	assertGone(t, paths.BuildArtifactsDir(first.ID))
	assertExists(t, filepath.Join(paths.BuildWorkDir(second.ID), "src"))
}

// TestSuccessKeepsLastFailureWorkspace: a success removes only its own
// workspace; the last failure's stays, since it is still the last failure.
func TestSuccessKeepsLastFailureWorkspace(t *testing.T) {
	t.Parallel()
	paths := store.NewStorePaths(t.TempDir())
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}

	fb := newFakeBuilder("content")
	fb.failNext = true
	if _, err := NewEngine(fb, bstore, artStore, gitProvider, paths).RunBuild(context.Background(), "mixpkg"); err == nil {
		t.Fatal("expected failure")
	}
	failed, err := bstore.Get("mixpkg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	ok, err := NewEngine(newFakeBuilder("content"), bstore, artStore, gitProvider, paths).RunBuild(context.Background(), "mixpkg")
	if err != nil {
		t.Fatalf("RunBuild success: %v", err)
	}
	assertExists(t, filepath.Join(paths.BuildWorkDir(failed.ID), "src"))
	assertGone(t, paths.BuildWorkDir(ok.ID))
}

// TestRunBuildCloneFailureRemovesBuildDir: a clone failure happens before
// any record names the build, so its whole directory goes and no record
// is written.
func TestRunBuildCloneFailureRemovesBuildDir(t *testing.T) {
	t.Parallel()
	paths := store.NewStorePaths(t.TempDir())
	bstore := jsonstore.NewBuildStore(paths)

	builder := newFakeBuilder("content")
	git := cloneFailingGit{&fakeGitProviderForBuild{}}
	_, err := NewEngine(builder, bstore, jsonstore.NewArtifactStore(paths), git, paths).RunBuild(context.Background(), "clonepkg")
	if err == nil || !strings.Contains(err.Error(), "cloneAt") {
		t.Fatalf("error = %v, want a cloneAt error", err)
	}
	if builder.gotWorkspace != "" {
		t.Fatal("builder ran after a clone failure")
	}
	assertNoBuildDirs(t, paths)
	if _, err := bstore.Get("clonepkg"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

// TestRunBuildPendingSaveFailureRemovesBuildDir: if the pending record
// cannot be saved, no record names the build, so its directory is removed
// rather than left as an orphan.
func TestRunBuildPendingSaveFailureRemovesBuildDir(t *testing.T) {
	t.Parallel()
	paths := store.NewStorePaths(t.TempDir())
	failing := &failingSaveBuildStore{BuildStore: jsonstore.NewBuildStore(paths), failSaveN: 1}

	builder := newFakeBuilder("content")
	_, err := NewEngine(builder, failing, jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths).RunBuild(context.Background(), "savepkg")
	if err == nil || !strings.Contains(err.Error(), "save pending build") {
		t.Fatalf("error = %v, want a save pending build error", err)
	}
	if builder.gotWorkspace != "" {
		t.Fatal("builder ran after the pending save failed")
	}
	assertNoBuildDirs(t, paths)
}

// TestRecoverInterruptedKeepsWorkspaceUntilNextFailure: the hard-kill case.
// A Running record left by a killed process is marked failed at startup;
// its workspace is kept (it is now the last failure) and the previous
// failure's directory goes. The package's next failure then removes the
// interrupted build's workspace too.
func TestRecoverInterruptedKeepsWorkspaceUntilNextFailure(t *testing.T) {
	t.Parallel()
	paths := store.NewStorePaths(t.TempDir())
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}

	const pkg = domain.PackageName("killedpkg")
	oldFailure := domain.BuildID("old-failure")
	interrupted := domain.BuildID("interrupted")

	// What a kill -9 mid-build leaves on disk: a previous failure's
	// directory, and the interrupted build's half-built workspace and
	// Running record.
	for _, id := range []domain.BuildID{oldFailure, interrupted} {
		src := filepath.Join(paths.BuildWorkDir(id), "src")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "PKGBUILD"), []byte("pkgname=killedpkg\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := bstore.Save(pkg, &domain.Build{
		ID:                 interrupted,
		PackageName:        pkg,
		Backend:            "makepkg",
		Status:             domain.BuildRunning,
		StartTime:          time.Now(),
		LogLocation:        paths.BuildLogPath(interrupted),
		LastFailureBuildID: oldFailure,
	}); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(newFakeBuilder("content"), bstore, artStore, gitProvider, paths)
	recovered, err := engine.RecoverInterrupted(context.Background(), []domain.PackageName{pkg})
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if len(recovered) != 1 || recovered[0] != pkg {
		t.Fatalf("recovered = %v, want [%s]", recovered, pkg)
	}
	assertExists(t, filepath.Join(paths.BuildWorkDir(interrupted), "src", "PKGBUILD"))
	assertGone(t, paths.BuildArtifactsDir(oldFailure))

	loaded, err := bstore.Get(pkg)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.LastFailureBuildID != interrupted {
		t.Fatalf("LastFailureBuildID = %q, want %q", loaded.LastFailureBuildID, interrupted)
	}

	// The next failure removes the interrupted build's workspace.
	fb := newFakeBuilder("content")
	fb.failNext = true
	if _, err := NewEngine(fb, bstore, artStore, gitProvider, paths).RunBuild(context.Background(), pkg); err == nil {
		t.Fatal("expected failure")
	}
	assertGone(t, paths.BuildArtifactsDir(interrupted))
}
