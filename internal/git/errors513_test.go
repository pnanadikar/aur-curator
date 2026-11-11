package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCannotStartExecRunner513 runs the real ExecRunner with no git on
// PATH, as in #513's reproduction. A file named git that is not
// executable is skipped by the PATH lookup, so it reads as not found too.
// Not parallel: t.Setenv changes PATH for the whole process.
func TestCannotStartExecRunner513(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := ExecRunner{}.Run(context.Background(), dir, "branch", "--show-current")
	if err == nil {
		t.Fatal("Run succeeded with no git on PATH")
	}
	if !CannotStart(err) {
		t.Errorf("CannotStart(%v) = false, want true", err)
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("errors.Is(%v, exec.ErrNotFound) = false", err)
	}
}

// TestCannotStart513: only git failing to start counts; git running and
// failing, an ended context and other errors do not.
func TestCannotStart513(t *testing.T) {
	t.Parallel()
	exitErr := exec.Command("sh", "-c", "exit 3").Run()
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("sh exit 3 gave %T %v, want *exec.ExitError", exitErr, exitErr)
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"not found, wrapped", fmt.Errorf("git [status]: %w", &exec.Error{Name: "git", Err: exec.ErrNotFound}), true},
		{"not executable", &exec.Error{Name: "git", Err: os.ErrPermission}, true},
		{"git exited non-zero", fmt.Errorf("git [remote get-url upstream]: %w: No such remote", exitErr), false},
		{"context cancelled", &InterruptedError{Cause: context.Canceled, Err: errors.New("signal: killed")}, false},
		{"other error", errors.New("resolve repository"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CannotStart(tt.err); got != tt.want {
				t.Errorf("CannotStart(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
