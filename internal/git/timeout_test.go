package git

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// errStopped stands in for a killed process's own error.
var errStopped = errors.New("signal: killed")

func TestInterrupted_RecordedTimeoutFired(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	err := Interrupted(ctx, errStopped)
	if got, want := err.Error(), "timed out after 20ms"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errStopped) {
		t.Fatalf("errors.Is lost DeadlineExceeded or the call's error: %v", err)
	}
}

// A parent deadline that ends first is the one that fires; the child's
// own 1-hour timeout must not be reported as the duration.
func TestInterrupted_ParentDeadlineFired(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()
	<-ctx.Done()

	if got, want := Interrupted(ctx, errStopped).Error(), "timed out"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// A recorded parent timeout that ends first stays visible through a
// longer recorded child.
func TestInterrupted_RecordedParentFired(t *testing.T) {
	parent, cancelParent := WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()
	<-ctx.Done()

	if got, want := Interrupted(ctx, errStopped).Error(), "timed out after 20ms"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// A shorter plain deadline under a recorded one fires first; the
// recorded duration is not its duration.
func TestInterrupted_UnrecordedChildFired(t *testing.T) {
	parent, cancelParent := WithTimeout(context.Background(), time.Hour)
	defer cancelParent()
	ctx, cancel := context.WithTimeout(parent, 20*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	if got, want := Interrupted(ctx, errStopped).Error(), "timed out"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestInterrupted_Cancelled(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	cancel()

	err := Interrupted(ctx, errStopped)
	if got, want := err.Error(), "cancelled"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false: %v", err)
	}
}

func TestInterrupted_Unchanged(t *testing.T) {
	if err := Interrupted(context.Background(), errStopped); err != errStopped {
		t.Fatalf("live ctx: got %v, want the error unchanged", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Interrupted(ctx, nil); err != nil {
		t.Fatalf("nil error: got %v, want nil", err)
	}
	first := Interrupted(ctx, errStopped)
	wrapped := errors.Join(errors.New("merge"), first)
	if again := Interrupted(ctx, wrapped); again != wrapped {
		t.Fatalf("already labelled: got %v, want it unchanged", again)
	}
}

// The runner tests run real git: a shell alias makes git spawn a child
// process, as fetch does, so the process-group kill is exercised.

func TestRunner_TimeoutSaysSo(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ExecRunner{}.Run(ctx, t.TempDir(), "-c", "alias.slp=!sleep 5", "slp")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasSuffix(err.Error(), ": timed out after 50ms") {
		t.Fatalf("message = %q, want it to end in %q", err, ": timed out after 50ms")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false: %v", err)
	}
}

func TestRunner_CancelSaysSo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := ExecRunner{}.Run(ctx, t.TempDir(), "-c", "alias.slp=!sleep 5", "slp")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasSuffix(err.Error(), ": cancelled") {
		t.Fatalf("message = %q, want it to end in %q", err, ": cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false: %v", err)
	}
}

func TestRunner_EmptyStderrNoTrailingColon(t *testing.T) {
	_, err := ExecRunner{}.Run(context.Background(), t.TempDir(), "-c", "alias.fail=!exit 3", "fail")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, want := err.Error(), "git [-c alias.fail=!exit 3 fail]: exit status 3"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
