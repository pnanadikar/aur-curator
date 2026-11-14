package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runMainEnv makes the test binary run main() with the arguments after
// "--" instead of the tests, so a test can check main's own exits.
const runMainEnv = "AUR_CURATOR_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		for i, a := range os.Args {
			if a == "--" {
				os.Args = append([]string{appName}, os.Args[i+1:]...)
				break
			}
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runMain runs main() in a child process with args and returns its
// stderr and exit code.
func runMain(t *testing.T, args ...string) (string, int) {
	t.Helper()
	_, stderr, code := runMainOut(t, args...)
	return stderr, code
}

// runMainOut is runMain, with stdout too.
func runMainOut(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^$", "--"}, args...)...) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errOut.String(), 0
	case errors.As(err, &exitErr):
		return out.String(), errOut.String(), exitErr.ExitCode()
	default:
		t.Fatalf("run main: %v", err)
		return "", "", 0
	}
}

// A --debug-log path that cannot be opened is reported and ends the run,
// before the config is read (#518).
func TestDebugLogUnopenable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "missing", "debug.log")
	cfg := filepath.Join(dir, "no-such-config.json")

	stderr, code := runMain(t, "--debug-log", logPath, cfg, "check")
	if code != 1 {
		t.Errorf("exit %d, want 1; stderr:\n%s", code, stderr)
	}
	want := "debug-log: cannot open " + logPath + ": "
	if !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if strings.Contains(stderr, "load config") {
		t.Errorf("config read before the debug log was opened:\n%s", stderr)
	}
}

// An openable --debug-log path gets past the log to the config.
func TestDebugLogOpenable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "debug.log")
	cfg := filepath.Join(dir, "no-such-config.json")

	stderr, code := runMain(t, "--debug-log", logPath, cfg, "check")
	if code != 1 || !strings.Contains(stderr, "load config") {
		t.Errorf("exit %d, stderr %q; want exit 1 from load config", code, stderr)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("debug log not created: %v", err)
	}
}
