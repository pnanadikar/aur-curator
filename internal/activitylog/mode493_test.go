package activitylog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLogFileIsPrivate: the activity log and its directory are created
// 0600 and 0750 (domain.md § Workspace › File Modes, #493).
func TestLogFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "activity.log")
	if err := Log(context.Background(), path, "approve", "yay"); err != nil {
		t.Fatal(err)
	}
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
