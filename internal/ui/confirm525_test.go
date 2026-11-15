package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// TestConfirmBuildID525: Confirm records only the build its modal
// showed; another build by then is refused with nothing recorded
// (workflow.md note 9, #525). No build ID (the modal could not read the
// build) leaves the other guards to decide.
func TestConfirmBuildID525(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		buildID domain.BuildID
		ok      bool
	}{
		{"same build", "build-1", true},
		{"replaced", "build-0", false},
		{"no build ID", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &mirrorTrackingBuildStore{
				latest:      &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
				lastSuccess: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
			}
			app := App{deps: Dependencies{BuildDB: store, Git: &fakeGitProviderOps{}}}
			msg := app.executeOperation(Operation{Kind: OpConfirm, Pkg: "pkg", BuildID: tc.buildID})()
			if tc.ok {
				if _, ok := msg.(msgs.OperationCompletedMsg); !ok || !store.lastSuccess.Confirmed {
					t.Fatalf("got %T %v, confirmed %v", msg, msg, store.lastSuccess.Confirmed)
				}
				return
			}
			failed, ok := msg.(msgs.OperationFailedMsg)
			if !ok || !strings.Contains(failed.Err.Error(), "a newer build replaced the one you inspected; press c again") {
				t.Fatalf("got %T %v", msg, msg)
			}
			if store.lastSuccess.Confirmed || store.lastSuccess.ConfirmedAt != nil {
				t.Error("a refused Confirm recorded something")
			}
		})
	}
}

// TestConfirmRequestCarriesBuildID525: the request's build ID reaches the
// queued operation.
func TestConfirmRequestCarriesBuildID525(t *testing.T) {
	t.Parallel()
	app := App{}
	// Something else is running, so the Confirm stays pending.
	app.ops.Enqueue(Operation{ID: "other", Kind: OpBuild, Pkg: "other"})
	app.ops.Dequeue()
	m, _ := app.Update(msgs.ConfirmRequestedMsg{Pkg: "pkg", BuildID: "build-7", TraceCtx: context.Background()})
	a := m.(App)
	op := a.ops.NextPending()
	if op == nil || op.Kind != OpConfirm || op.BuildID != "build-7" {
		t.Fatalf("pending operation %+v", op)
	}
}
