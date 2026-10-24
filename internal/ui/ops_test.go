package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/store"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeBuilder struct {
	calls atomic.Int32
}

func (f *fakeBuilder) Build(_ context.Context, _ string, _ string) (build.BuildResult, error) {
	f.calls.Add(1)
	return build.BuildResult{}, nil
}

type fakePublisher struct {
	calls atomic.Int32
}

func (f *fakePublisher) Publish(_ context.Context, _ string) error {
	f.calls.Add(1)
	return nil
}

// ---------------------------------------------------------------------------
// Store fakes for build engine + review engine preconditions
// ---------------------------------------------------------------------------

type fakeReviewStore struct {
	review *domain.Review
}

func (f *fakeReviewStore) Get(_ domain.PackageName) (*domain.Review, error) {
	if f.review == nil {
		return nil, domain.ErrNotFound
	}
	return f.review, nil
}

func (f *fakeReviewStore) Save(_ domain.PackageName, _ *domain.Review) error { return nil }
func (f *fakeReviewStore) Delete(_ domain.PackageName) error                 { return nil }

type fakeBuildStore struct{}

func (f *fakeBuildStore) Get(_ domain.PackageName) (*domain.Build, error) {
	return nil, nil
}

func (f *fakeBuildStore) Save(_ domain.PackageName, _ *domain.Build) error { return nil }

func (f *fakeBuildStore) ReadBuildLog(_ domain.BuildID, _ string) (string, error) {
	return "", nil
}

func (f *fakeBuildStore) Delete(_ domain.PackageName) error { return nil }

func (f *fakeBuildStore) GetLastSuccess(_ domain.PackageName) (*domain.Build, error) {
	return nil, nil
}

func (f *fakeBuildStore) SaveLastSuccess(_ domain.PackageName, _ *domain.Build) error { return nil }

type fakeConfirmedBuildStore struct{}

func (f *fakeConfirmedBuildStore) Get(_ domain.PackageName) (*domain.Build, error) {
	return &domain.Build{Confirmed: true}, nil
}

func (f *fakeConfirmedBuildStore) Save(_ domain.PackageName, _ *domain.Build) error { return nil }

func (f *fakeConfirmedBuildStore) ReadBuildLog(_ domain.BuildID, _ string) (string, error) {
	return "", nil
}

func (f *fakeConfirmedBuildStore) Delete(_ domain.PackageName) error { return nil }

func (f *fakeConfirmedBuildStore) GetLastSuccess(_ domain.PackageName) (*domain.Build, error) {
	return &domain.Build{ID: "ls-1", Status: domain.BuildSuccess, Confirmed: true, Published: false, ReviewedCommit: "abc123"}, nil
}

func (f *fakeConfirmedBuildStore) SaveLastSuccess(_ domain.PackageName, _ *domain.Build) error {
	return nil
}

type fakeUnconfirmedBuildStore struct{}

func (f *fakeUnconfirmedBuildStore) Get(_ domain.PackageName) (*domain.Build, error) {
	return &domain.Build{Confirmed: false}, nil
}

func (f *fakeUnconfirmedBuildStore) Save(_ domain.PackageName, _ *domain.Build) error { return nil }

func (f *fakeUnconfirmedBuildStore) ReadBuildLog(_ domain.BuildID, _ string) (string, error) {
	return "", nil
}

func (f *fakeUnconfirmedBuildStore) Delete(_ domain.PackageName) error { return nil }

func (f *fakeUnconfirmedBuildStore) GetLastSuccess(_ domain.PackageName) (*domain.Build, error) {
	return &domain.Build{ID: "ls-2", Status: domain.BuildSuccess, Confirmed: false, Published: false, ReviewedCommit: "abc123"}, nil
}

func (f *fakeUnconfirmedBuildStore) SaveLastSuccess(_ domain.PackageName, _ *domain.Build) error {
	return nil
}

type fakeArtifactStore struct{}

func (f *fakeArtifactStore) Get(_ domain.ArtifactID) (*domain.Artifact, error) {
	return nil, nil
}

func (f *fakeArtifactStore) Save(_ domain.ArtifactID, _ *domain.Artifact) error {
	return nil
}

func (f *fakeArtifactStore) GetArtifactsByPackage(_ domain.PackageName) ([]domain.Artifact, error) {
	return nil, nil
}

func (f *fakeArtifactStore) Delete(_ domain.ArtifactID) error { return nil }

func (f *fakeArtifactStore) ListAll() ([]domain.Artifact, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// SN-06 fakes — git.Provider, ReviewStore, Analyzer.Manager, policy.Engine
// ---------------------------------------------------------------------------

type fakeGitProviderOps struct {
	fetchCalls        atomic.Int32
	fetchErr          error
	commitsBehind     int
	commitsBehindErr  error
	upstreamCommit    string
	upstreamCommitErr error
	mergeCalls        atomic.Int32
	mergeErr          error
	mergeCommit       atomic.Value // last commit passed to Merge (string)
	merged            bool         // IsMerged result (#406)
	mergedErr         error
	isMergedCommit    atomic.Value // last commit passed to IsMerged (string)
}

func (f *fakeGitProviderOps) Import(context.Context, domain.PackageName) error { return nil }

func (f *fakeGitProviderOps) Fetch(_ context.Context, _ domain.PackageName) error {
	f.fetchCalls.Add(1)
	return f.fetchErr
}

func (f *fakeGitProviderOps) Status(context.Context, domain.PackageName) (*git.RepositoryStatus, error) {
	return &git.RepositoryStatus{}, nil
}

func (f *fakeGitProviderOps) CommitsBehind(context.Context, domain.PackageName) (int, error) {
	if f.commitsBehindErr != nil {
		return 0, f.commitsBehindErr
	}
	return f.commitsBehind, nil
}

func (f *fakeGitProviderOps) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	if f.upstreamCommitErr != nil {
		return "", f.upstreamCommitErr
	}
	return f.upstreamCommit, nil
}

func (f *fakeGitProviderOps) CloneAt(context.Context, domain.PackageName, string, string) error {
	return nil
}
func (f *fakeGitProviderOps) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return "abc123", nil
}
func (f *fakeGitProviderOps) FileAtCommit(context.Context, domain.PackageName, string, string) (string, error) {
	return "", nil
}

// fakeGitProviderOpsDynamic is a fake that can return different values on
// successive calls to UpstreamCommit. It wraps a base fakeGitProviderOps
// and overrides UpstreamCommit behavior.
type fakeGitProviderOpsDynamic struct {
	*fakeGitProviderOps
	beforeHash string
	afterHash  string
	afterErr   error
	called     atomic.Int32
}

func (f *fakeGitProviderOpsDynamic) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	n := f.called.Add(1)
	if n == 1 {
		return f.beforeHash, f.upstreamCommitErr
	}
	if f.afterErr != nil {
		return "", f.afterErr
	}
	return f.afterHash, nil
}

func (f *fakeGitProviderOpsDynamic) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	if f.upstreamCommit == "" {
		return &git.GitDiff{}, nil
	}
	return &git.GitDiff{UpstreamCommit: f.upstreamCommit}, nil
}
func (f *fakeGitProviderOpsDynamic) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return false, nil
}

type fakeGitProviderOpsFetchWithReview struct {
	*fakeGitProviderOps
	beforeHash         string
	afterHash          string
	afterErr           error
	beforeCalled       atomic.Int32
	diffUpstreamCommit string
}

func (f *fakeGitProviderOpsFetchWithReview) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	n := f.beforeCalled.Add(1)
	if n == 1 {
		return f.beforeHash, f.upstreamCommitErr
	}
	if f.afterErr != nil {
		return "", f.afterErr
	}
	return f.afterHash, nil
}

func (f *fakeGitProviderOpsFetchWithReview) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	if f.diffUpstreamCommit == "" {
		return &git.GitDiff{}, nil
	}
	return &git.GitDiff{UpstreamCommit: f.diffUpstreamCommit}, nil
}
func (f *fakeGitProviderOpsFetchWithReview) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return false, nil
}

func (f *fakeGitProviderOps) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}

// ReviewDiff is shared by the two fakes that embed fakeGitProviderOps.
func (f *fakeGitProviderOps) ReviewDiff(context.Context, domain.PackageName, string, string) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}

func (f *fakeGitProviderOps) Merge(_ context.Context, _ domain.PackageName, commit string) error {
	f.mergeCalls.Add(1)
	f.mergeCommit.Store(commit)
	return f.mergeErr
}

func (f *fakeGitProviderOps) LocalOnlyCommits(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}

func (f *fakeGitProviderOps) Metadata(context.Context, domain.PackageName) (*git.RepositoryMetadata, error) {
	return &git.RepositoryMetadata{}, nil
}
func (f *fakeGitProviderOps) IsMerged(_ context.Context, _ domain.PackageName, commit string) (bool, error) {
	f.isMergedCommit.Store(commit)
	return f.merged, f.mergedErr
}

type fakeReviewStoreOps struct {
	review *domain.Review
}

func (f *fakeReviewStoreOps) Get(domain.PackageName) (*domain.Review, error) {
	if f.review == nil {
		return nil, domain.ErrNotFound
	}
	r := *f.review
	return &r, nil
}

func (f *fakeReviewStoreOps) Save(_ domain.PackageName, review *domain.Review) error {
	r := *review
	f.review = &r
	return nil
}

func (f *fakeReviewStoreOps) Delete(domain.PackageName) error {
	f.review = nil
	return nil
}

type fakeAnalyzerManagerOps struct{}

func (fakeAnalyzerManagerOps) Run(context.Context, *git.GitDiff) ([]domain.Finding, error) {
	return nil, nil
}

func newTestReviewEngine(initial *domain.Review) (*review.Engine, *fakeGitProviderOps, *fakeReviewStoreOps) {
	git := &fakeGitProviderOps{}
	store := &fakeReviewStoreOps{review: initial}
	policyEngine := policy.NewEngine(domain.PolicyConfig{})
	engine := review.NewEngine(git, store, fakeAnalyzerManagerOps{}, policyEngine)
	return engine, git, store
}

func newTestReviewEngineWithGit(initial *domain.Review, git git.Provider) (*review.Engine, *fakeGitProviderOps, *fakeReviewStoreOps) {
	store := &fakeReviewStoreOps{review: initial}
	policyEngine := policy.NewEngine(domain.PolicyConfig{})
	engine := review.NewEngine(git, store, fakeAnalyzerManagerOps{}, policyEngine)
	fakeOps, _ := git.(*fakeGitProviderOps)
	return engine, fakeOps, store
}

// ---------------------------------------------------------------------------
// Bug 1 — drainQueueCmd actually invokes the command
// ---------------------------------------------------------------------------

func TestDrainQueueCmdActuallyInvokes(t *testing.T) {
	t.Parallel()

	git := &fakeGitProviderOps{}
	app := App{
		deps: Dependencies{Git: git},
	}

	app.ops.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "test-pkg"})

	_, cmd := app.drainQueueCmd()
	if cmd == nil {
		t.Fatal("drainQueueCmd returned nil")
	}

	msg := cmd()
	if msg == nil {
		t.Fatal("expected a message from executing the operation, got nil")
	}

	_, ok := msg.(msgs.OperationCompletedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}

	if git.fetchCalls.Load() != 1 {
		t.Fatalf("Fetch called %d times, want 1", git.fetchCalls.Load())
	}
}

// ---------------------------------------------------------------------------
// Bug 2 — Dequeue refuses when active
// ---------------------------------------------------------------------------

func TestDequeueRefusesWhenActive(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	q.Enqueue(Operation{ID: "op-1", Kind: OpBuild, Pkg: "pkg-1"})
	q.Enqueue(Operation{ID: "op-2", Kind: OpBuild, Pkg: "pkg-2"})

	// First dequeue succeeds and sets active.
	op, ok := q.Dequeue()
	if !ok {
		t.Fatal("first dequeue should succeed")
	}
	if op.ID != "op-1" {
		t.Fatalf("dequeue returned op %q, want %q", op.ID, "op-1")
	}

	// Second dequeue should refuse because active is set.
	_, ok = q.Dequeue()
	if ok {
		t.Fatal("second dequeue should refuse when active, but succeeded")
	}

	// Pending still has op-2.
	if q.LenPending() != 1 {
		t.Fatalf("pending len = %d, want 1", q.LenPending())
	}

	// After Complete, dequeue should succeed again.
	q.Complete()
	op, ok = q.Dequeue()
	if !ok {
		t.Fatal("dequeue after Complete should succeed")
	}
	if op.ID != "op-2" {
		t.Fatalf("dequeue returned op %q, want %q", op.ID, "op-2")
	}
}

func TestDequeueEmptyReturnsFalse(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	_, ok := q.Dequeue()
	if ok {
		t.Fatal("dequeue on empty queue should return false")
	}
}

// ---------------------------------------------------------------------------
// Full flow — enqueue, drain, complete, drain again
// ---------------------------------------------------------------------------

func TestQueueFullFlow(t *testing.T) {
	t.Parallel()

	git := &fakeGitProviderOps{}
	app := App{
		deps: Dependencies{Git: git},
	}

	// Enqueue two operations.
	app.ops.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "pkg-1"})
	app.ops.Enqueue(Operation{ID: "op-2", Kind: OpFetch, Pkg: "pkg-2"})

	// Drain first time → executes op-1.
	var next App
	next, cmd := app.drainQueueCmd()
	if cmd == nil {
		t.Fatal("drainQueueCmd returned nil")
	}
	msg1 := cmd()
	if msg1 == nil {
		t.Fatal("expected message from first drain")
	}
	if _, ok := msg1.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg from first drain, got %T", msg1)
	}

	// Fetch called once.
	if git.fetchCalls.Load() != 1 {
		t.Fatalf("Fetch called %d times after first drain, want 1", git.fetchCalls.Load())
	}

	// Queue is now active, draining again should produce nil (Dequeue refuses).
	_, cmd2 := next.drainQueueCmd()
	if cmd2 != nil {
		msg2 := cmd2()
		if msg2 != nil {
			t.Fatalf("expected nil from second drain (active guard), got %T", msg2)
		}
	}

	// Simulate completing the operation — this is what happens when
	// msgs.OperationCompletedMsg reaches App.Update.
	next.ops.Complete()

	// Drain again → executes op-2.
	_, cmd3 := next.drainQueueCmd()
	msg3 := cmd3()
	if msg3 == nil {
		t.Fatal("expected message from third drain")
	}
	if _, ok := msg3.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg from third drain, got %T", msg3)
	}

	if git.fetchCalls.Load() != 2 {
		t.Fatalf("Fetch called %d times after second drain, want 2", git.fetchCalls.Load())
	}
}

// ---------------------------------------------------------------------------
// Failure path — msgs.OperationFailedMsg also clears active
// ---------------------------------------------------------------------------

func TestFailureClearsActive(t *testing.T) {
	t.Parallel()

	app := App{} // nil Builder — will trigger "not implemented" failure for OpBuild

	app.ops.Enqueue(Operation{ID: "fail-op", Kind: OpBuild, Pkg: "pkg-fail"})
	app.ops.Enqueue(Operation{ID: "next-op", Kind: OpBuild, Pkg: "pkg-next"})

	var next App
	next, cmd := app.drainQueueCmd()
	if cmd == nil {
		t.Fatal("drainQueueCmd returned nil")
	}

	msg := cmd()
	if msg == nil {
		t.Fatal("expected message from drain")
	}

	failedMsg, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
	}
	if failedMsg.Err == nil {
		t.Fatal("expected non-nil error from failed operation")
	}

	// Active should still be set at this point — Complete hasn't been called.
	if next.ops.active == nil {
		t.Fatal("active should still be set before Complete is called")
	}

	// Simulate App.Update handling msgs.OperationFailedMsg.
	next.ops.Complete()

	// Now drain should pick up the next pending operation.
	_, cmd2 := next.drainQueueCmd()
	msg2 := cmd2()
	if msg2 == nil {
		t.Fatal("expected message from drain after failure")
	}
	if _, ok := msg2.(msgs.OperationFailedMsg); !ok {
		t.Fatalf("expected msgs.OperationFailedMsg for next op, got %T", msg2)
	}
}

// ---------------------------------------------------------------------------
// IsIdle edge cases
// ---------------------------------------------------------------------------

func TestIsIdle(t *testing.T) {
	t.Parallel()

	var q OperationsQueue

	if !q.IsIdle() {
		t.Fatal("empty queue should be idle")
	}

	q.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "pkg"})

	if q.IsIdle() {
		t.Fatal("queue with pending op should not be idle")
	}

	// Dequeue makes it active — not idle.
	q.Dequeue()
	if q.IsIdle() {
		t.Fatal("queue with active op should not be idle")
	}

	// Complete should make it idle again (no pending).
	q.Complete()
	if !q.IsIdle() {
		t.Fatal("queue should be idle after completing the only op")
	}
}

// ---------------------------------------------------------------------------
// drainQueueCmd with nil dependency — no panic, returns error message
// ---------------------------------------------------------------------------

func TestDrainQueueNilDeps(t *testing.T) {
	t.Parallel()

	app := App{} // Builder is nil

	app.ops.Enqueue(Operation{ID: "nil-dep", Kind: OpBuild, Pkg: "pkg"})

	_, cmd := app.drainQueueCmd()
	msg := cmd()

	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg for nil dep, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected non-nil error from nil dep operation")
	}
}

// ---------------------------------------------------------------------------
// CMD-012 — Enqueue de-dup
// ---------------------------------------------------------------------------

func TestEnqueueDedup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(*OperationsQueue)
		enqueueID   string
		enqueueKind OperationKind
		wantOK      bool
		wantPending int
	}{
		{
			name:        "first enqueue succeeds",
			setup:       func(q *OperationsQueue) {},
			enqueueID:   "op-1",
			enqueueKind: OpFetch,
			wantOK:      true,
			wantPending: 1,
		},
		{
			name: "duplicate pending ID is refused",
			setup: func(q *OperationsQueue) {
				q.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "pkg"})
			},
			enqueueID:   "op-1",
			enqueueKind: OpBuild,
			wantOK:      false,
			wantPending: 1,
		},
		{
			name: "different IDs both succeed",
			setup: func(q *OperationsQueue) {
				q.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "pkg-1"})
			},
			enqueueID:   "op-2",
			enqueueKind: OpBuild,
			wantOK:      true,
			wantPending: 2,
		},
		{
			name: "enqueue matching active ID is refused",
			setup: func(q *OperationsQueue) {
				q.Enqueue(Operation{ID: "op-1", Kind: OpFetch, Pkg: "pkg"})
				q.Dequeue()
			},
			enqueueID:   "op-1",
			enqueueKind: OpBuild,
			wantOK:      false,
			wantPending: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var q OperationsQueue
			tt.setup(&q)

			ok, depth := q.Enqueue(Operation{ID: tt.enqueueID, Kind: tt.enqueueKind, Pkg: "pkg"})
			if ok != tt.wantOK {
				t.Fatalf("Enqueue() = %v, want %v", ok, tt.wantOK)
			}
			if ok && depth != tt.wantPending-1 {
				t.Fatalf("Enqueue() depth = %d, want %d (pending %d - 1)", depth, tt.wantPending-1, tt.wantPending)
			}
			if !ok && depth != 0 {
				t.Fatalf("Enqueue() depth = %d on rejection, want 0", depth)
			}
			if got := q.LenPending(); got != tt.wantPending {
				t.Fatalf("LenPending() = %d, want %d", got, tt.wantPending)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dequeue with same package — different kinds
// ---------------------------------------------------------------------------

func TestDequeueDifferentKinds(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	q.Enqueue(Operation{ID: "f1", Kind: OpFetch, Pkg: "yay"})
	q.Enqueue(Operation{ID: "a1", Kind: OpAnalyze, Pkg: "yay"})
	q.Enqueue(Operation{ID: "b1", Kind: OpBuild, Pkg: "yay"})

	op1, ok := q.Dequeue()
	if !ok || op1.Kind != OpFetch {
		t.Fatal("should dequeue OpFetch first")
	}

	_, ok = q.Dequeue()
	if ok {
		t.Fatal("should refuse while active")
	}

	q.Complete()

	op2, ok := q.Dequeue()
	if !ok || op2.Kind != OpAnalyze {
		t.Fatal("should dequeue OpAnalyze second")
	}

	q.Complete()

	op3, ok := q.Dequeue()
	if !ok || op3.Kind != OpBuild {
		t.Fatal("should dequeue OpBuild third")
	}
}

// ---------------------------------------------------------------------------
// executeOperation dispatches correct kind
// ---------------------------------------------------------------------------

func TestExecuteOperationDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind      OperationKind
		expectMsg any
		expectErr bool
		needDep   bool
		depName   string
	}{
		{OpBuild, msgs.OperationCompletedMsg{}, false, true, "builder"},
		{OpBuild, msgs.OperationFailedMsg{}, true, false, "nil builder"},
		{OpPublish, msgs.OperationCompletedMsg{}, false, true, "publisher"},
		{OpPublish, msgs.OperationFailedMsg{}, true, false, "nil publisher"},
		{OpFetch, msgs.OperationFailedMsg{}, true, false, "nil git"},
		{OpMerge, msgs.OperationFailedMsg{}, true, false, "nil git+reviews"},
		{OpAnalyze, msgs.OperationFailedMsg{}, true, false, "nil git+reviews"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.kind, tt.depName), func(t *testing.T) {
			app := App{}
			if tt.needDep {
				switch tt.kind {
				case OpBuild:
					// The reviewed commit is merged (#406).
					app.deps.Git = &fakeGitProviderOps{merged: true}
					app.deps.Reviews = review.NewEngine(nil, &fakeReviewStore{
						review: &domain.Review{Status: domain.ReviewApproved},
					}, nil, nil)
					app.deps.BuildEngine = build.NewEngine(
						&fakeBuilder{},
						&fakeBuildStore{},
						&fakeArtifactStore{},
						&fakeGitProviderOps{},
						store.NewStorePaths(""),
					)
				case OpPublish:
					app.deps.Publisher = &fakePublisher{}
					app.deps.BuildDB = &fakeConfirmedBuildStore{}
					// #309 R4: the commit guard needs HEAD; abc123 matches the
					// fake's last success.
					app.deps.Git = &fakeGitProviderOps{}
				}
			}

			cmd := app.executeOperation(Operation{Kind: tt.kind, Pkg: "pkg"})
			msg := cmd()

			switch expect := tt.expectMsg.(type) {
			case msgs.OperationCompletedMsg:
				if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
					t.Fatalf("expected %T, got %T", expect, msg)
				}
			case msgs.OperationFailedMsg:
				if _, ok := msg.(msgs.OperationFailedMsg); !ok {
					t.Fatalf("expected %T, got %T", expect, msg)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CMD-056 — OpPublish rejects unconfirmed build (negative gate test)
// ---------------------------------------------------------------------------

func TestExecuteOperationPublishRejectsUnconfirmedBuild(t *testing.T) {
	t.Parallel()

	pub := &fakePublisher{}
	app := App{
		deps: Dependencies{
			Publisher: pub,
			BuildDB:   &fakeUnconfirmedBuildStore{},
		},
	}

	cmd := app.executeOperation(Operation{Kind: OpPublish, Pkg: "pkg"})
	msg := cmd()

	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
	}

	if failed.Err == nil {
		t.Fatal("expected non-nil error from unconfirmed build rejection")
	}

	if pub.calls.Load() != 0 {
		t.Fatalf("fakePublisher.calls = %d, want 0 (Publish must not be called when build is unconfirmed)", pub.calls.Load())
	}
}

// ---------------------------------------------------------------------------
// SN-06 — success paths for OpFetch and OpAnalyze
// ---------------------------------------------------------------------------

func TestExecuteOperationFetchSuccess(t *testing.T) {
	t.Parallel()

	git := &fakeGitProviderOps{}
	app := App{deps: Dependencies{Git: git}}

	cmd := app.executeOperation(Operation{Kind: OpFetch, Pkg: "yay"})
	msg := cmd()

	if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}
	if git.fetchCalls.Load() != 1 {
		t.Fatalf("Fetch called %d times, want 1", git.fetchCalls.Load())
	}
}

func TestExecuteOperationAnalyzeSuccess(t *testing.T) {
	t.Parallel()

	stored := &domain.Review{ID: "yay@aaa", PackageName: "yay", UpstreamCommit: "aaa", Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun}
	engine, gp, store := newTestReviewEngine(stored)
	app := App{deps: Dependencies{Git: gp, Reviews: engine}}

	cmd := app.executeOperation(Operation{ID: "yay", Kind: OpAnalyze, Pkg: "yay", ReviewID: "yay@aaa"})
	msg := cmd()

	completed, ok := msg.(msgs.OperationCompletedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}
	if completed.OpID != "yay" {
		t.Fatalf("OpID = %q, want %q", completed.OpID, "yay")
	}
	if store.review.FindingsStatus != domain.FindingsRun {
		t.Fatalf("FindingsStatus = %q, want %q", store.review.FindingsStatus, domain.FindingsRun)
	}
	if store.review.ID != "yay@aaa" {
		t.Fatalf("stored review ID = %q, want %q (Analyze must not replace the review)", store.review.ID, "yay@aaa")
	}
	if got := gp.fetchCalls.Load(); got != 0 {
		t.Fatalf("Fetch called %d times, want 0 (#399)", got)
	}
}

// TestExecuteOperationAnalyzeReviewChanged: when the stored review is not
// the one the screen showed, Analyze fails with *review.ReviewChangedError
// and runs nothing (#399).
func TestExecuteOperationAnalyzeReviewChanged(t *testing.T) {
	t.Parallel()

	stored := &domain.Review{ID: "yay@bbb", PackageName: "yay", UpstreamCommit: "bbb", Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun}
	engine, gp, store := newTestReviewEngine(stored)
	app := App{deps: Dependencies{Git: gp, Reviews: engine}}

	cmd := app.executeOperation(Operation{ID: "yay", Kind: OpAnalyze, Pkg: "yay", ReviewID: "yay@aaa"})
	msg := cmd()

	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
	}
	if failed.OpID != "yay" {
		t.Fatalf("OpID = %q, want %q", failed.OpID, "yay")
	}
	var rce *review.ReviewChangedError
	if !errors.As(failed.Err, &rce) {
		t.Fatalf("Err = %v, want *review.ReviewChangedError", failed.Err)
	}
	if rce.Expected != "yay@aaa" || rce.Current.ID != "yay@bbb" {
		t.Fatalf("Expected/Current = %q/%q, want %q/%q", rce.Expected, rce.Current.ID, "yay@aaa", "yay@bbb")
	}
	if store.review.FindingsStatus != domain.FindingsNotRun {
		t.Fatalf("FindingsStatus = %q, want %q (nothing must run)", store.review.FindingsStatus, domain.FindingsNotRun)
	}
	if got := gp.fetchCalls.Load(); got != 0 {
		t.Fatalf("Fetch called %d times, want 0", got)
	}
}

func TestExecuteOperationFetchBeforeErr(t *testing.T) {
	t.Parallel()

	git := &fakeGitProviderOps{upstreamCommitErr: assertError{msg: "git error"}}
	app := App{deps: Dependencies{Git: git}}

	cmd := app.executeOperation(Operation{Kind: OpFetch, Pkg: "yay"})
	msg := cmd()

	completed, ok := msg.(msgs.OperationCompletedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}
	if reason, ok := completed.Result.(string); ok && reason != "" {
		t.Fatalf("Result = %q, want empty", reason)
	}
}

func TestExecuteOperationFetchAfterErr(t *testing.T) {
	t.Parallel()

	git := &fakeGitProviderOpsDynamic{
		fakeGitProviderOps: &fakeGitProviderOps{upstreamCommit: "abc123"},
		beforeHash:         "abc123",
		afterHash:          "def456",
		afterErr:           assertError{msg: "boom"},
	}
	app := App{deps: Dependencies{Git: git}}

	cmd := app.executeOperation(Operation{Kind: OpFetch, Pkg: "yay"})
	msg := cmd()

	completed, ok := msg.(msgs.OperationCompletedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}
	if reason, ok := completed.Result.(string); ok && reason != "" {
		t.Fatalf("Result = %q, want empty", reason)
	}
}

// ---------------------------------------------------------------------------
// CMD-056 — (OperationKind).Lane()
// ---------------------------------------------------------------------------

func TestOperationKindLane(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind OperationKind
		want Lane
	}{
		{OpFetch, LaneFetch},
		{OpAnalyze, LaneAnalyze},
		{OpBuild, LaneBuild},
		{OpMerge, LaneFetch},
		{OpPublish, LanePublish},
		{OpConfirm, LanePublish},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			t.Parallel()
			if got := tt.kind.Lane(); got != tt.want {
				t.Fatalf("Lane() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CMD-056 — MarkStarted, AppendLog, MarkCompleted, MarkFailed
// ---------------------------------------------------------------------------

func TestMarkStarted(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpBuild, Pkg: "test-pkg"}

	q.MarkStarted(op)

	ls := q.LaneState(LaneBuild)
	if ls.Status != StatusRunning {
		t.Fatalf("Status = %q, want %q", ls.Status, StatusRunning)
	}
	if len(ls.Log) != 1 {
		t.Fatalf("Log length = %d, want 1", len(ls.Log))
	}
	if ls.Log[0].Message != "build" {
		t.Fatalf("Log[0].Message = %q, want %q", ls.Log[0].Message, "build")
	}
}

func TestMarkCompleted(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpFetch, Pkg: "yay"}

	q.MarkStarted(op)
	q.MarkCompleted(op, "Updated 2 packages")

	ls := q.LaneState(LaneFetch)
	if ls.Status != StatusCompleted {
		t.Fatalf("Status = %q, want %q", ls.Status, StatusCompleted)
	}
	if len(ls.Log) != 2 {
		t.Fatalf("Log length = %d, want 2", len(ls.Log))
	}
	if ls.Log[1].Message != "Updated 2 packages" {
		t.Fatalf("Log[1].Message = %q, want %q", ls.Log[1].Message, "Updated 2 packages")
	}
}

func TestMarkFailed(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpBuild, Pkg: "broken-pkg"}

	q.MarkStarted(op)
	q.MarkFailed(op, fmt.Errorf("build failed"))

	ls := q.LaneState(LaneBuild)
	if ls.Status != StatusFailed {
		t.Fatalf("Status = %q, want %q", ls.Status, StatusFailed)
	}
	if len(ls.Log) != 2 {
		t.Fatalf("Log length = %d, want 2", len(ls.Log))
	}
	if ls.Log[1].Message != "build failed" {
		t.Fatalf("Log[1].Message = %q, want %q", ls.Log[1].Message, "build failed")
	}
}

func TestAppendLog(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpAnalyze, Pkg: "test"}

	q.MarkStarted(op)
	q.AppendLog(op, "Running analyzer: shell")
	q.AppendLog(op, "Running analyzer: checksum")

	ls := q.LaneState(LaneAnalyze)
	if len(ls.Log) != 3 {
		t.Fatalf("Log length = %d, want 3", len(ls.Log))
	}
	if ls.Log[1].Message != "Running analyzer: shell" {
		t.Fatalf("Log[1].Message = %q, want %q", ls.Log[1].Message, "Running analyzer: shell")
	}
	if ls.Log[2].Message != "Running analyzer: checksum" {
		t.Fatalf("Log[2].Message = %q, want %q", ls.Log[2].Message, "Running analyzer: checksum")
	}
}

func TestMarkCompletedAppendsLog(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpPublish, Pkg: "pkg"}

	q.MarkStarted(op)
	q.MarkCompleted(op, "Published to AUR")

	ls := q.LaneState(LanePublish)
	if len(ls.Log) != 2 {
		t.Fatalf("Log length = %d, want 2 (start + completed)", len(ls.Log))
	}
}

func TestMarkFailedAppendsLog(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op := Operation{ID: "op-1", Kind: OpBuild, Pkg: "pkg"}

	q.MarkStarted(op)
	q.MarkFailed(op, fmt.Errorf("compilation error"))

	ls := q.LaneState(LaneBuild)
	if len(ls.Log) != 2 {
		t.Fatalf("Log length = %d, want 2 (start + failed)", len(ls.Log))
	}
}

func TestMultipleOperationsSameLane(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	op1 := Operation{ID: "op-1", Kind: OpBuild, Pkg: "pkg-1"}
	op2 := Operation{ID: "op-2", Kind: OpBuild, Pkg: "pkg-2"}

	q.MarkStarted(op1)
	q.MarkCompleted(op1, "pkg-1 done")
	q.MarkStarted(op2)
	q.MarkCompleted(op2, "pkg-2 done")

	ls := q.LaneState(LaneBuild)
	if len(ls.Log) != 4 {
		t.Fatalf("Log length = %d, want 4", len(ls.Log))
	}
	if ls.Log[3].Message != "pkg-2 done" {
		t.Fatalf("Log[3].Message = %q, want %q", ls.Log[3].Message, "pkg-2 done")
	}
}

// ---------------------------------------------------------------------------
// CMD-056 — LaneState QueueDepth is derived
// ---------------------------------------------------------------------------

func TestLaneStateQueueDepth(t *testing.T) {
	t.Parallel()

	var q OperationsQueue

	// Initially idle, depth 0.
	ls := q.LaneState(LaneFetch)
	if ls.QueueDepth != 0 {
		t.Fatalf("QueueDepth = %d, want 0 for empty queue", ls.QueueDepth)
	}

	// Enqueue 2 fetch ops.
	q.Enqueue(Operation{ID: "f1", Kind: OpFetch, Pkg: "pkg1"})
	q.Enqueue(Operation{ID: "f2", Kind: OpFetch, Pkg: "pkg2"})
	ls = q.LaneState(LaneFetch)
	if ls.QueueDepth != 2 {
		t.Fatalf("QueueDepth = %d, want 2", ls.QueueDepth)
	}

	// Dequeue one — depth should be 1.
	q.Dequeue()
	q.MarkStarted(*q.active)
	ls = q.LaneState(LaneFetch)
	if ls.QueueDepth != 1 {
		t.Fatalf("QueueDepth = %d, want 1 after dequeue", ls.QueueDepth)
	}

	// Complete and dequeue next.
	q.Complete()
	q.Dequeue()
	q.MarkStarted(*q.active)
	ls = q.LaneState(LaneFetch)
	if ls.QueueDepth != 0 {
		t.Fatalf("QueueDepth = %d, want 0 after second dequeue", ls.QueueDepth)
	}

	// Enqueue non-fetch op — should not affect LaneFetch depth.
	q.Enqueue(Operation{ID: "b1", Kind: OpBuild, Pkg: "pkg3"})
	ls = q.LaneState(LaneFetch)
	if ls.QueueDepth != 0 {
		t.Fatalf("QueueDepth for LaneFetch = %d, want 0 (build op should not affect fetch lane)", ls.QueueDepth)
	}
	lsBuild := q.LaneState(LaneBuild)
	if lsBuild.QueueDepth != 1 {
		t.Fatalf("QueueDepth for LaneBuild = %d, want 1", lsBuild.QueueDepth)
	}
}

// ---------------------------------------------------------------------------
// CMD-056 — LaneState returns copy, not pointer
// ---------------------------------------------------------------------------

func TestLaneStateReturnsCopy(t *testing.T) {
	t.Parallel()

	var q OperationsQueue
	q.Enqueue(Operation{ID: "f1", Kind: OpFetch, Pkg: "pkg"})

	ls1 := q.LaneState(LaneFetch)
	ls2 := q.LaneState(LaneFetch)

	if ls1.Status != ls2.Status {
		t.Fatal("LaneState should return consistent copies")
	}
	if ls1.QueueDepth != ls2.QueueDepth {
		t.Fatal("LaneState QueueDepth should be consistent")
	}

	// Mutating one copy should not affect the other.
	ls1.Status = "mutated"
	if ls2.Status == "mutated" {
		t.Fatal("LaneState should return a copy, not a reference")
	}
}

// ---------------------------------------------------------------------------
// OpMerge handler — re-confirmation before Merge
// ---------------------------------------------------------------------------

func TestExecuteOperationMergeRefusesWhenNotApproved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status domain.ReviewStatus
	}{
		{"pending review", domain.ReviewPending},
		{"rejected review", domain.ReviewRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			seeded := &domain.Review{
				PackageName:    "yay",
				UpstreamCommit: "abc123",
				Status:         tt.status,
			}
			engine, git, _ := newTestReviewEngine(seeded)
			app := App{deps: Dependencies{Git: git, Reviews: engine}}

			cmd := app.executeOperation(Operation{Kind: OpMerge, Pkg: "yay"})
			msg := cmd()

			failed, ok := msg.(msgs.OperationFailedMsg)
			if !ok {
				t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
			}
			if git.mergeCalls.Load() != 0 {
				t.Fatalf("Merge called %d times, want 0 when status is %s", git.mergeCalls.Load(), tt.status)
			}
			if failed.Err == nil {
				t.Fatal("expected non-nil error from merge when not approved")
			}
		})
	}
}

func TestExecuteOperationMergeSucceedsWhenApproved(t *testing.T) {
	t.Parallel()

	seeded := &domain.Review{
		PackageName:    "yay",
		UpstreamCommit: "abc123",
		Status:         domain.ReviewApproved,
	}
	engine, git, _ := newTestReviewEngine(seeded)
	app := App{deps: Dependencies{Git: git, Reviews: engine}}

	cmd := app.executeOperation(Operation{Kind: OpMerge, Pkg: "yay"})
	msg := cmd()

	completed, ok := msg.(msgs.OperationCompletedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T", msg)
	}
	if completed.Result != "Merge completed" {
		t.Fatalf("Result = %v, want %q", completed.Result, "Merge completed")
	}
	if git.mergeCalls.Load() != 1 {
		t.Fatalf("Merge called %d times, want 1", git.mergeCalls.Load())
	}
	if got, _ := git.mergeCommit.Load().(string); got != "abc123" {
		t.Fatalf("Merge got commit %q, want the review's UpstreamCommit %q", got, "abc123")
	}
}

func TestExecuteOperationMergeFailsOnGitError(t *testing.T) {
	t.Parallel()

	seeded := &domain.Review{
		PackageName:    "yay",
		UpstreamCommit: "abc123",
		Status:         domain.ReviewApproved,
	}
	engine, git, _ := newTestReviewEngine(seeded)
	git.mergeErr = assertError{msg: "git push failed"}
	app := App{deps: Dependencies{Git: git, Reviews: engine}}

	cmd := app.executeOperation(Operation{Kind: OpMerge, Pkg: "yay"})
	msg := cmd()

	_, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
	}
	if git.mergeCalls.Load() != 1 {
		t.Fatalf("Merge called %d times, want 1", git.mergeCalls.Load())
	}
}

func TestExecuteOperationMergeNilGitProvider(t *testing.T) {
	t.Parallel()

	app := App{} // no Git dep

	cmd := app.executeOperation(Operation{Kind: OpMerge, Pkg: "yay"})
	msg := cmd()

	_, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
	}
}

// ---------------------------------------------------------------------------
// Mirror tests — Confirm and Publish update last-success when IDs match
// ---------------------------------------------------------------------------

type mirrorTrackingBuildStore struct {
	lastSuccess *domain.Build
	latest      *domain.Build
}

func (m *mirrorTrackingBuildStore) Get(_ domain.PackageName) (*domain.Build, error) {
	if m.latest == nil {
		return nil, domain.ErrNotFound
	}
	cp := *m.latest
	return &cp, nil
}

func (m *mirrorTrackingBuildStore) Save(_ domain.PackageName, build *domain.Build) error {
	cp := *build
	m.latest = &cp
	return nil
}

func (m *mirrorTrackingBuildStore) GetLastSuccess(_ domain.PackageName) (*domain.Build, error) {
	if m.lastSuccess == nil {
		return nil, domain.ErrNotFound
	}
	cp := *m.lastSuccess
	return &cp, nil
}

func (m *mirrorTrackingBuildStore) SaveLastSuccess(_ domain.PackageName, build *domain.Build) error {
	cp := *build
	m.lastSuccess = &cp
	return nil
}

func (m *mirrorTrackingBuildStore) ReadBuildLog(_ domain.BuildID, _ string) (string, error) {
	return "", nil
}

func (m *mirrorTrackingBuildStore) Delete(_ domain.PackageName) error { return nil }

// The transitional mirror tests from #309 part (a) are replaced: since
// part (b), Confirm and Publish read and write ONLY the last success.

// TestConfirmWritesOnlyLastSuccess: a later attempt failed; Confirm acts on
// the last success (the build of HEAD) and leaves the latest attempt alone.
func TestConfirmWritesOnlyLastSuccess(t *testing.T) {
	t.Parallel()

	store := &mirrorTrackingBuildStore{
		latest:      &domain.Build{ID: "build-2", Status: domain.BuildFailed, ReviewedCommit: "abc123"},
		lastSuccess: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
	}
	app := App{deps: Dependencies{BuildDB: store, Git: &fakeGitProviderOps{}}}

	msg := app.executeOperation(Operation{Kind: OpConfirm, Pkg: "pkg"})()
	if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T: %v", msg, msg)
	}
	if !store.lastSuccess.Confirmed || store.lastSuccess.ConfirmedAt == nil {
		t.Error("last success should be confirmed with ConfirmedAt set")
	}
	if store.latest.ID != "build-2" || store.latest.Confirmed || store.latest.Status != domain.BuildFailed {
		t.Errorf("latest attempt must be untouched, got %+v", store.latest)
	}
}

// TestPublishUsesLastSuccess: a later attempt failed; Publish publishes the
// last success and records Published on it only.
func TestPublishUsesLastSuccess(t *testing.T) {
	t.Parallel()

	store := &mirrorTrackingBuildStore{
		latest:      &domain.Build{ID: "build-2", Status: domain.BuildFailed, ReviewedCommit: "abc123"},
		lastSuccess: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, Confirmed: true, ReviewedCommit: "abc123"},
	}
	pub := &fakePublisher{}
	app := App{deps: Dependencies{BuildDB: store, Git: &fakeGitProviderOps{}, Publisher: pub}}

	msg := app.executeOperation(Operation{Kind: OpPublish, Pkg: "pkg"})()
	if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg, got %T: %v", msg, msg)
	}
	if pub.calls.Load() != 1 {
		t.Errorf("publisher calls = %d, want 1", pub.calls.Load())
	}
	if !store.lastSuccess.Published || store.lastSuccess.PublishedAt == nil {
		t.Error("last success should be published with PublishedAt set")
	}
	if store.latest.Published {
		t.Error("latest attempt must not be marked published")
	}
}

// TestConfirmPublishRefuseWithoutGit: the commit guard cannot be verified
// without a git provider, so both operations refuse, write nothing, and
// log the refusal (#309 R4 — refuse, never skip).
func TestConfirmPublishRefuseWithoutGit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		kind OperationKind
		ls   domain.Build
	}{
		{OpConfirm, domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"}},
		{OpPublish, domain.Build{ID: "build-1", Status: domain.BuildSuccess, Confirmed: true, ReviewedCommit: "abc123"}},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			t.Parallel()

			ls := tc.ls
			store := &mirrorTrackingBuildStore{lastSuccess: &ls}
			pub := &fakePublisher{}
			logPath := filepath.Join(t.TempDir(), "activity.log")
			app := App{deps: Dependencies{
				BuildDB:   store,
				Publisher: pub,
				Config:    domain.WorkspaceConfig{ActivityLogPath: logPath},
			}}

			msg := app.executeOperation(Operation{Kind: tc.kind, Pkg: "pkg"})()
			failed, ok := msg.(msgs.OperationFailedMsg)
			if !ok {
				t.Fatalf("expected msgs.OperationFailedMsg, got %T", msg)
			}
			if !strings.Contains(failed.Err.Error(), "cannot verify") {
				t.Errorf("error = %v, want a commit-verification refusal", failed.Err)
			}
			got := store.lastSuccess
			if got.Confirmed != tc.ls.Confirmed || got.Published != tc.ls.Published ||
				got.ConfirmedAt != nil || got.PublishedAt != nil {
				t.Errorf("last success modified on refusal: %+v", got)
			}
			if pub.calls.Load() != 0 {
				t.Errorf("publisher called %d times on refusal", pub.calls.Load())
			}
			data, err := os.ReadFile(logPath)
			if err != nil || !strings.Contains(string(data), `status="failed"`) {
				t.Errorf("activity log should record the refusal, got %q (err %v)", data, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Missing test 1: Confirm with no LastSuccess returns OperationFailedMsg
// ---------------------------------------------------------------------------

func TestMissingLastSuccessConfirm(t *testing.T) {
	t.Parallel()

	store := &mirrorTrackingBuildStore{
		latest: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
	}
	app := App{deps: Dependencies{BuildDB: store}}

	msg := app.executeOperation(Operation{Kind: OpConfirm, Pkg: "pkg"})()
	if _, ok := msg.(msgs.OperationFailedMsg); !ok {
		t.Fatalf("expected msgs.OperationFailedMsg when LastSuccess is missing, got %T", msg)
	}
}

// ---------------------------------------------------------------------------
// Missing test 2: Publish with no LastSuccess returns OperationFailedMsg
// ---------------------------------------------------------------------------

func TestMissingLastSuccessPublish(t *testing.T) {
	t.Parallel()

	store := &mirrorTrackingBuildStore{
		latest: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123", Confirmed: true},
	}
	app := App{deps: Dependencies{BuildDB: store}}

	msg := app.executeOperation(Operation{Kind: OpPublish, Pkg: "pkg"})()
	if _, ok := msg.(msgs.OperationFailedMsg); !ok {
		t.Fatalf("expected msgs.OperationFailedMsg when LastSuccess is missing, got %T", msg)
	}
}

// ---------------------------------------------------------------------------
// Missing test 3: LastSuccess already confirmed returns OperationFailedMsg
// ---------------------------------------------------------------------------

func TestLastSuccessAlreadyConfirmed(t *testing.T) {
	t.Parallel()

	ls := &domain.Build{
		ID:             "build-1",
		Status:         domain.BuildSuccess,
		Confirmed:      true,
		ConfirmedAt:    func() *time.Time { t := time.Now(); return &t }(),
		ReviewedCommit: "abc123",
	}

	store := &mirrorTrackingBuildStore{
		latest:      &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
		lastSuccess: ls,
	}
	app := App{deps: Dependencies{BuildDB: store}}

	msg := app.executeOperation(Operation{Kind: OpConfirm, Pkg: "pkg"})()
	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg for already-confirmed LastSuccess, got %T", msg)
	}
	if !strings.Contains(failed.Err.Error(), "already confirmed") {
		t.Errorf("expected error mentioning 'already confirmed', got: %v", failed.Err)
	}
}

// ---------------------------------------------------------------------------
// Missing test 4: LastSuccess already published (publish proceeds with fake publisher)
// ---------------------------------------------------------------------------

func TestLastSuccessAlreadyPublished(t *testing.T) {
	t.Parallel()

	ls := &domain.Build{
		ID:             "build-1",
		Status:         domain.BuildSuccess,
		Confirmed:      true,
		ConfirmedAt:    func() *time.Time { t := time.Now(); return &t }(),
		Published:      true,
		ReviewedCommit: "abc123",
	}

	store := &mirrorTrackingBuildStore{
		latest:      &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "abc123", Confirmed: true},
		lastSuccess: ls,
	}

	pub := &fakePublisher{}

	app := App{deps: Dependencies{
		BuildDB:   store,
		Publisher: pub,
		Git:       &fakeGitProviderOps{}, // #309 R4: guard needs HEAD (abc123)
		Config:    domain.WorkspaceConfig{ActivityLogPath: t.TempDir() + "/activity.log"},
	}}

	msg := app.executeOperation(Operation{Kind: OpPublish, Pkg: "pkg"})()
	if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
		t.Fatalf("expected msgs.OperationCompletedMsg for already-published LastSuccess, got %T: %v", msg, msg)
	}
	if pub.calls.Load() != 1 {
		t.Errorf("expected publisher to be called once, got %d", pub.calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Missing test 5: Commit mismatch on Confirm returns OperationFailedMsg
// ---------------------------------------------------------------------------

func TestCommitMismatchConfirm(t *testing.T) {
	t.Parallel()

	ls := &domain.Build{
		ID:             "build-1",
		Status:         domain.BuildSuccess,
		Confirmed:      false,
		ReviewedCommit: "oldcommit",
	}

	store := &mirrorTrackingBuildStore{latest: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "oldcommit"}}
	store.lastSuccess = ls

	git := &fakeGitProviderOps{upstreamCommit: "abc123"}

	app := App{deps: Dependencies{
		BuildDB: store,
		Git:     git,
		Config:  domain.WorkspaceConfig{ActivityLogPath: t.TempDir() + "/activity.log"},
	}}

	msg := app.executeOperation(Operation{Kind: OpConfirm, Pkg: "pkg"})()
	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg for commit mismatch, got %T", msg)
	}
	if !strings.Contains(failed.Err.Error(), "older commit") {
		t.Errorf("expected error mentioning 'older commit', got: %v", failed.Err)
	}
}

// ---------------------------------------------------------------------------
// Missing test 6: Commit mismatch on Publish returns OperationFailedMsg
// ---------------------------------------------------------------------------

func TestCommitMismatchPublish(t *testing.T) {
	t.Parallel()

	ls := &domain.Build{
		ID:             "build-1",
		Status:         domain.BuildSuccess,
		Confirmed:      true,
		ConfirmedAt:    func() *time.Time { t := time.Now(); return &t }(),
		ReviewedCommit: "oldcommit",
	}

	store := &mirrorTrackingBuildStore{latest: &domain.Build{ID: "build-1", Status: domain.BuildSuccess, ReviewedCommit: "oldcommit", Confirmed: true}}
	store.lastSuccess = ls

	git := &fakeGitProviderOps{upstreamCommit: "abc123"}
	pub := &fakePublisher{}

	app := App{deps: Dependencies{
		BuildDB:   store,
		Git:       git,
		Publisher: pub,
		Config:    domain.WorkspaceConfig{ActivityLogPath: t.TempDir() + "/activity.log"},
	}}

	msg := app.executeOperation(Operation{Kind: OpPublish, Pkg: "pkg"})()
	failed, ok := msg.(msgs.OperationFailedMsg)
	if !ok {
		t.Fatalf("expected msgs.OperationFailedMsg for commit mismatch, got %T", msg)
	}
	if !strings.Contains(failed.Err.Error(), "older commit") {
		t.Errorf("expected error mentioning 'older commit', got: %v", failed.Err)
	}
	if pub.calls.Load() != 0 {
		t.Errorf("publisher should not be called on commit mismatch, got %d calls", pub.calls.Load())
	}
}

// TestExecuteOperationFetchReviewOutcomes covers what a TUI Fetch reports
// and stores, decided from the stored review rather than from whether the
// ref moved during this fetch (#405). "stuck" is the #405 case: upstream
// moved before this fetch, so the fetch itself sees no movement.
func TestExecuteOperationFetchReviewOutcomes(t *testing.T) {
	t.Parallel()

	const (
		upToDate = "Already up to date — no new upstream commits since last fetch"
		started  = "New upstream commit def456 — review started"
		moved    = "Upstream changed (refs: abc123 → def456)"
	)
	stored := func(commit string, s domain.ReviewStatus) *domain.Review {
		return &domain.Review{
			ID:             domain.ReviewID("yay@" + commit),
			PackageName:    "yay",
			UpstreamCommit: commit,
			Status:         s,
		}
	}

	tests := []struct {
		name       string
		before     string
		after      string
		stored     *domain.Review
		wantResult string
		wantID     domain.ReviewID
		wantCommit string
		wantStatus domain.ReviewStatus
	}{
		{"up to date", "def456", "def456", stored("def456", domain.ReviewApproved), upToDate, "yay@def456", "def456", domain.ReviewApproved},
		{"moved, no review stored", "abc123", "def456", nil, started, "yay@def456", "def456", domain.ReviewPending},
		{"moved, approved at old commit", "abc123", "def456", stored("abc123", domain.ReviewApproved), started, "yay@def456", "def456", domain.ReviewPending},
		{"moved, review already current", "abc123", "def456", stored("def456", domain.ReviewApproved), moved, "yay@def456", "def456", domain.ReviewApproved},
		{"stuck, approved at old commit", "def456", "def456", stored("abc123", domain.ReviewApproved), started, "yay@def456", "def456", domain.ReviewPending},
		{"stuck, rejected at old commit", "def456", "def456", stored("abc123", domain.ReviewRejected), started, "yay@def456", "def456", domain.ReviewPending},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeGit := &fakeGitProviderOpsFetchWithReview{
				fakeGitProviderOps: &fakeGitProviderOps{upstreamCommit: tc.before},
				beforeHash:         tc.before,
				afterHash:          tc.after,
				diffUpstreamCommit: tc.after,
			}
			engine, _, store := newTestReviewEngineWithGit(tc.stored, fakeGit)
			app := App{deps: Dependencies{Git: fakeGit, Reviews: engine}}

			msg := app.executeOperation(Operation{Kind: OpFetch, Pkg: "yay"})()

			completed, ok := msg.(msgs.OperationCompletedMsg)
			if !ok {
				t.Fatalf("got %T, want msgs.OperationCompletedMsg", msg)
			}
			if got, _ := completed.Result.(string); got != tc.wantResult {
				t.Fatalf("Result = %q, want %q", got, tc.wantResult)
			}
			if store.review == nil {
				t.Fatal("no stored review after Fetch")
			}
			r := store.review
			if r.ID != tc.wantID || r.UpstreamCommit != tc.wantCommit || r.Status != tc.wantStatus {
				t.Fatalf("stored review = {ID %q, UpstreamCommit %q, Status %q}, want {ID %q, UpstreamCommit %q, Status %q}",
					r.ID, r.UpstreamCommit, r.Status, tc.wantID, tc.wantCommit, tc.wantStatus)
			}
		})
	}
}

// TestExecuteOperationBuildGate406: Build requires the reviewed commit to be
// merged, and does not care where upstream's tip is (#406).
func TestExecuteOperationBuildGate406(t *testing.T) {
	t.Parallel()

	const reviewed = "upAAAAAAA1"
	tests := []struct {
		name          string
		merged        bool
		mergedErr     error
		commitsBehind int
		wantErr       string // "" = build runs
	}{
		{name: "merged, upstream ahead", merged: true, commitsBehind: 3},
		{name: "merged, upstream level", merged: true, commitsBehind: 0},
		{name: "not merged, nothing behind", merged: false, commitsBehind: 0,
			wantErr: "build: pkg not yet merged (reviewed commit upAAAAA is not in master)"},
		{name: "merge check fails", mergedErr: errors.New("boom"),
			wantErr: "build: failed to check merge state for pkg: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gp := &fakeGitProviderOps{merged: tt.merged, mergedErr: tt.mergedErr, commitsBehind: tt.commitsBehind}
			builder := &fakeBuilder{}
			app := App{}
			app.deps.Git = gp
			app.deps.Reviews = review.NewEngine(nil, &fakeReviewStore{
				review: &domain.Review{Status: domain.ReviewApproved, UpstreamCommit: reviewed},
			}, nil, nil)
			app.deps.BuildEngine = build.NewEngine(
				builder,
				&fakeBuildStore{},
				&fakeArtifactStore{},
				&fakeGitProviderOps{},
				store.NewStorePaths(""),
			)

			msg := app.executeOperation(Operation{ID: "pkg", Kind: OpBuild, Pkg: "pkg"})()

			if got, _ := gp.isMergedCommit.Load().(string); got != reviewed {
				t.Fatalf("IsMerged asked about %q, want the reviewed commit %q", got, reviewed)
			}
			if tt.wantErr == "" {
				if _, ok := msg.(msgs.OperationCompletedMsg); !ok {
					t.Fatalf("got %#v, want OperationCompletedMsg", msg)
				}
				if builder.calls.Load() != 1 {
					t.Fatalf("builder calls = %d, want 1", builder.calls.Load())
				}
				return
			}
			failed, ok := msg.(msgs.OperationFailedMsg)
			if !ok || failed.Err == nil || failed.Err.Error() != tt.wantErr {
				t.Fatalf("got %#v\nwant OperationFailedMsg %q", msg, tt.wantErr)
			}
			if builder.calls.Load() != 0 {
				t.Fatalf("builder calls = %d, want 0 (refused)", builder.calls.Load())
			}
		})
	}
}
