package procgroup

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

// TestSignalKV checks each signal trace line is built fresh from the base
// pairs: even length, its own sig only, and the base slice untouched.
func TestSignalKV(t *testing.T) {
	base := []any{"to", "subprocess", "from", "builder", "cmd", "makepkg"}
	orig := append([]any(nil), base...)
	errBoom := errors.New("boom")

	term := signalKV(base, "SIGTERM", "failed", 42, errBoom)
	kill := signalKV(base, "SIGKILL", "ok", 42, nil)

	wantKill := []any{"to", "subprocess", "from", "builder", "cmd", "makepkg",
		"sig", "SIGKILL", "target", "pgid", "status", "ok", "pid", 42}
	if !reflect.DeepEqual(kill, wantKill) {
		t.Fatalf("SIGKILL kv = %v, want %v", kill, wantKill)
	}
	if len(term)%2 != 0 {
		t.Fatalf("SIGTERM kv has odd length %d: %v", len(term), term)
	}
	if term[len(term)-2] != "err" || term[len(term)-1] != errBoom {
		t.Fatalf("SIGTERM kv should end with err pair, got %v", term)
	}
	if !reflect.DeepEqual(base, orig) {
		t.Fatalf("base mutated: %v, want %v", base, orig)
	}
}

func TestRunCompletes(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo hi")
	cmd.Stdout = &buf

	if err := Run(ctx, cmd, "cmd", "echo"); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != "hi\n" {
		t.Fatalf("expected %q, got %q", "hi\n", got)
	}
}

func TestCancelKillsChildHoldingPipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & sleep 30")
	cmd.Stderr = &buf // forces a pipe, like makepkg's MultiWriter

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cmd, "cmd", "sleep")
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected non-nil error after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: Run did not return within 5s after cancel (child process holding pipe survived)")
	}
}

func TestRunPassesFailureThrough(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", "exit 3")

	err := Run(ctx, cmd, "cmd", "exit3")
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 3 {
		t.Fatalf("expected exit code 3, got %d", exitErr.ExitCode())
	}
}
