package instancelock

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockFileIsPrivate: the lock and its directory are created 0600 and
// 0750 (domain.md § Workspace › File Modes, #493).
func TestLockFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o750} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %v, want %v", p, got, want)
		}
	}
}
