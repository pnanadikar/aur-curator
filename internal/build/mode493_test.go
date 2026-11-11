package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

// TestBuildFilesArePrivate: the build log (from makepkg and from an
// interrupted build) and a persisted package are created 0600, their
// directories 0750 (domain.md § Workspace › File Modes, #493).
func TestBuildFilesArePrivate(t *testing.T) {
	t.Run("makepkg log", func(t *testing.T) {
		f := newFixture(t)
		if _, log, err := f.build(t); err != nil {
			t.Fatalf("%v\n%s", err, log)
		}
		wantMode(t, f.logPath, 0o600)
		wantMode(t, filepath.Dir(f.logPath), 0o750)
	})
	t.Run("interrupted note", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "logs", "build.log")
		if err := appendInterruptedNote(logPath, time.Now()); err != nil {
			t.Fatal(err)
		}
		wantMode(t, logPath, 0o600)
		wantMode(t, filepath.Dir(logPath), 0o750)
	})
	t.Run("persisted package", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "x-1-1-any.pkg.tar.zst")
		if err := os.WriteFile(src, []byte("pkg"), 0o644); err != nil {
			t.Fatal(err)
		}
		dst, err := copyArtifactTo(src, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		wantMode(t, dst, 0o600)
	})
}
