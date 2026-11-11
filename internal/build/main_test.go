package build

import (
	"fmt"
	"os"
	"testing"

	"github.com/pramod/aur-curator/internal/sandbox"
)

// TestMain points TMPDIR at a private directory for the whole package, so
// nothing a test creates in the system temp directory is left in the real
// one. Build workspaces no longer go there (#307: they live in
// .aur-curator/builds/<id>/work), but this keeps a regression back to the
// temp directory from littering /tmp.
//
// It also makes the test binary the sandbox helper (#496): MakepkgBuilder
// runs Paths.Helper inside bwrap as "<helper> sandbox-exec -- ...", and in
// these tests the helper is this binary, so it answers the hidden first
// arguments as cmd/aur-curator does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case sandbox.ExecArg:
			err := sandbox.Exec(os.Args[2:])
			fmt.Fprintf(os.Stderr, "%s: %v\n", sandbox.ExecArg, err)
			os.Exit(127)
		case sandbox.ProbeArg:
			os.Exit(sandbox.Probe(os.Args[2:], os.Stdout))
		}
	}

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
