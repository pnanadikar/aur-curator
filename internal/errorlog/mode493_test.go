package errorlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLogFileIsPrivate: errors.log and its directory are created 0600
// and 0750 (domain.md § Workspace › File Modes, #493).
func TestLogFileIsPrivate(t *testing.T) {
	root := t.TempDir()
	if err := Log(context.Background(), root, "test", "message"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{
		filepath.Join(root, errorsDir, errorsFile): 0o600,
		filepath.Join(root, errorsDir):             0o750,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %v, want %v", p, got, want)
		}
	}
}
