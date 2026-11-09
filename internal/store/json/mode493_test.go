package jsonstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// wantMode fails unless path has exactly the permission bits want.
func wantMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s: mode %v, want %v", path, got, want)
	}
}

// TestStoreFilesArePrivate: records saved to a new workspace are 0600 and
// their directories 0750 (domain.md § Workspace › File Modes, #493).
func TestStoreFilesArePrivate(t *testing.T) {
	paths := store.NewStorePaths(t.TempDir())

	if err := NewReviewStore(paths).Save("yay", &domain.Review{ID: "yay@abc", PackageName: "yay"}); err != nil {
		t.Fatal(err)
	}
	bs := NewBuildStore(paths)
	if err := bs.Save("yay", &domain.Build{ID: "b1"}); err != nil {
		t.Fatal(err)
	}
	if err := bs.SaveLastSuccess("yay", &domain.Build{ID: "b1"}); err != nil {
		t.Fatal(err)
	}
	if err := NewArtifactStore(paths).Save("a1", &domain.Artifact{ID: "a1", PackageName: "yay"}); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		paths.ReviewPath("yay"),
		paths.BuildPath("yay"),
		paths.LastSuccessBuildPath("yay"),
		paths.ArtifactPath("a1"),
	} {
		wantMode(t, p, 0o600)
		wantMode(t, filepath.Dir(p), 0o750)
	}
}
