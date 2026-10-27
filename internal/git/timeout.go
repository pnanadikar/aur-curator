package git

import (
	"context"
	"errors"
	"time"
)

// timeoutKey keys the timeout WithTimeout records in a context.
type timeoutKey struct{}

// recordedTimeout is what WithTimeout records: the duration asked for and
// the deadline it set. Interrupted compares that deadline with the
// context's actual one to tell whether this timeout is the one that fired.
type recordedTimeout struct {
	d        time.Duration
	deadline time.Time
}

// WithTimeout is context.WithTimeout that also records d, so Interrupted
// can say how long the timeout was when it fires (#417). A nil parent
// means context.Background().
//
// If parent already ends no later than d from now, nothing is recorded:
// the parent's deadline is the one that will fire, and a timeout the
// parent recorded stays the one Interrupted finds.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	deadline := time.Now().Add(d)
	ctx, cancel := context.WithDeadline(parent, deadline)
	if pd, ok := parent.Deadline(); ok && !pd.After(deadline) {
		return ctx, cancel
	}
	return context.WithValue(ctx, timeoutKey{}, recordedTimeout{d: d, deadline: deadline}), cancel
}

// InterruptedError is a call stopped by its context: a timeout or a
// cancel. Its message is only the label ("timed out after 1m0s",
// "timed out", "cancelled"); the stopped call's own error — typically a
// killed process's "signal: killed" — says nothing useful and is kept
// only for errors.Is / errors.As, alongside the context's error.
type InterruptedError struct {
	// Cause is the context's error: context.DeadlineExceeded or
	// context.Canceled.
	Cause error
	// After is the recorded timeout that fired, or 0 when unknown (a
	// deadline WithTimeout did not set) or not a timeout.
	After time.Duration
	// Err is the stopped call's own error.
	Err error
}

func (e *InterruptedError) Error() string {
	switch {
	case errors.Is(e.Cause, context.Canceled):
		return "cancelled"
	case e.After > 0:
		return "timed out after " + e.After.String()
	default:
		return "timed out"
	}
}

func (e *InterruptedError) Unwrap() []error { return []error{e.Cause, e.Err} }

// Interrupted labels err when ctx is done: it returns an *InterruptedError
// saying the call timed out (and after how long, when the deadline that
// fired is one WithTimeout recorded) or was cancelled. Otherwise, or when
// err is nil or already labelled, it returns err unchanged.
//
// A call that failed for its own reason just as ctx ended is labelled as
// interrupted too; callers that have the call's own output (git's
// stderr) should still show it.
func Interrupted(ctx context.Context, err error) error {
	if err == nil || ctx == nil || ctx.Err() == nil {
		return err
	}
	var already *InterruptedError
	if errors.As(err, &already) {
		return err
	}
	ie := &InterruptedError{Cause: ctx.Err(), Err: err}
	if errors.Is(ie.Cause, context.DeadlineExceeded) {
		if rt, ok := ctx.Value(timeoutKey{}).(recordedTimeout); ok {
			if d, ok := ctx.Deadline(); ok && d.Equal(rt.deadline) {
				ie.After = rt.d
			}
		}
	}
	return ie
}
