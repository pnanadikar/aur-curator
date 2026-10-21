package build

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points TMPDIR at a private directory for the whole package, so
// nothing a test creates in the system temp directory is left in the real
// one. Build workspaces no longer go there (#307: they live in
// .aur-curator/builds/<id>/work), but this keeps a regression back to the
// temp directory from littering /tmp.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aur-curator-build-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
