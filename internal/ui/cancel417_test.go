package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #417 — a Ctrl+T cancel of Fetch, Analyze or Merge is reported as a
// cancel (OperationCancelledMsg), as Build's is, not as a failure. The git
// runner keeps context.Canceled in its error since #417.

var errGitCancelled = fmt.Errorf("git [x]: %w", context.Canceled)

func TestExecuteOperationFetchCancelled417(t *testing.T) {
	t.Parallel()

	app := App{deps: Dependencies{Git: &fakeGitProviderOps{fetchErr: errGitCancelled}}}

	msg := app.executeOperation(Operation{ID: "yay", Kind: OpFetch, Pkg: "yay"})()

	if c, ok := msg.(msgs.OperationCancelledMsg); !ok || c.OpID != "yay" {
		t.Fatalf("got %T %+v, want OperationCancelledMsg for yay", msg, msg)
	}
}

func TestExecuteOperationAnalyzeCancelled417(t *testing.T) {
	t.Parallel()

	stored := &domain.Review{ID: "yay@aaa", PackageName: "yay", UpstreamCommit: "aaa", Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun}
	engine, gp, _ := newTestReviewEngine(stored)
	app := App{deps: Dependencies{Git: gp, Reviews: engine}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg := app.executeOperation(Operation{ID: "yay", Kind: OpAnalyze, Pkg: "yay", ReviewID: "yay@aaa", TraceCtx: ctx})()

	if c, ok := msg.(msgs.OperationCancelledMsg); !ok || c.OpID != "yay" {
		t.Fatalf("got %T %+v, want OperationCancelledMsg for yay", msg, msg)
	}
}

func TestExecuteOperationMergeCancelled417(t *testing.T) {
	t.Parallel()

	seeded := &domain.Review{PackageName: "yay", UpstreamCommit: "abc123", Status: domain.ReviewApproved}
	engine, gp, _ := newTestReviewEngine(seeded)
	gp.mergeErr = errGitCancelled
	app := App{deps: Dependencies{Git: gp, Reviews: engine}}

	msg := app.executeOperation(Operation{ID: "yay", Kind: OpMerge, Pkg: "yay"})()

	if c, ok := msg.(msgs.OperationCancelledMsg); !ok || c.OpID != "yay" {
		t.Fatalf("got %T %+v, want OperationCancelledMsg for yay", msg, msg)
	}
}

// A timeout is still a failure, and says so.
func TestExecuteOperationMergeTimeoutFails417(t *testing.T) {
	t.Parallel()

	seeded := &domain.Review{PackageName: "yay", UpstreamCommit: "abc123", Status: domain.ReviewApproved}
	engine, gp, _ := newTestReviewEngine(seeded)
	gp.mergeErr = fmt.Errorf("git [x]: %w", context.DeadlineExceeded)
	app := App{deps: Dependencies{Git: gp, Reviews: engine}}

	msg := app.executeOperation(Operation{ID: "yay", Kind: OpMerge, Pkg: "yay"})()

	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("got %T, want OperationFailedMsg", msg)
	}
	if !errors.Is(failed.Err, context.DeadlineExceeded) {
		t.Fatalf("Err = %v, want it to wrap context.DeadlineExceeded", failed.Err)
	}
}
