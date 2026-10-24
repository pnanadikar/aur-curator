package review

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// TestNewReviewStoresBaseCommit387: a review created by EnsureReview or
// InitiateReview stores the diff's base commit as the start of its range.
func TestNewReviewStoresBaseCommit387(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(e *Engine) (*domain.Review, error)
	}{
		{"EnsureReview", func(e *Engine) (*domain.Review, error) {
			return e.EnsureReview(context.Background(), "yay")
		}},
		{"InitiateReview", func(e *Engine) (*domain.Review, error) {
			return e.InitiateReview(context.Background(), "yay")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeReviewStore{}
			g := &fakeGitProvider{diff: &git.GitDiff{BaseCommit: "base1", UpstreamCommit: "up1"}}
			e := NewEngine(g, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

			r, err := tc.run(e)
			if err != nil {
				t.Fatal(err)
			}
			if r.BaseCommit != "base1" || store.review.BaseCommit != "base1" {
				t.Fatalf("BaseCommit = %q (stored %q), want base1", r.BaseCommit, store.review.BaseCommit)
			}
			if r.ID != "yay@up1" {
				t.Fatalf("ID = %q, want yay@up1 (identity unchanged)", r.ID)
			}
		})
	}
}

// TestInitiateReviewKeepsBaseWhenUnchanged387: re-fetching the same
// upstream commit keeps the stored base, even if master has moved since
// (e.g. after Merge the live merge base is the upstream commit itself).
func TestInitiateReviewKeepsBaseWhenUnchanged387(t *testing.T) {
	store := &fakeReviewStore{review: &domain.Review{
		ID: "yay@up1", PackageName: "yay", UpstreamCommit: "up1", BaseCommit: "base1",
		Status: domain.ReviewApproved,
	}}
	g := &fakeGitProvider{diff: &git.GitDiff{BaseCommit: "up1", UpstreamCommit: "up1"}}
	e := NewEngine(g, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := e.InitiateReview(context.Background(), "yay")
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseCommit != "base1" || store.saveCalls != 0 {
		t.Fatalf("BaseCommit = %q, saves = %d; want base1 and no save", r.BaseCommit, store.saveCalls)
	}
}

// TestRunAnalyzersUsesStoredRange387: analyzers run on the review's
// stored range, never on the live comparison.
func TestRunAnalyzersUsesStoredRange387(t *testing.T) {
	store := &fakeReviewStore{review: &domain.Review{
		ID: "yay@up1", PackageName: "yay", UpstreamCommit: "up1", BaseCommit: "base1",
		Status: domain.ReviewPending,
	}}
	g := &fakeGitProvider{diff: &git.GitDiff{BaseCommit: "base1", UpstreamCommit: "up1"}}
	e := NewEngine(g, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	if _, err := e.RunAnalyzers(context.Background(), "yay", "yay@up1"); err != nil {
		t.Fatal(err)
	}
	if g.reviewDiffCalls != 1 || g.reviewDiffBase != "base1" || g.reviewDiffUpstream != "up1" {
		t.Fatalf("ReviewDiff calls=%d range=%q→%q, want 1 call base1→up1",
			g.reviewDiffCalls, g.reviewDiffBase, g.reviewDiffUpstream)
	}
	if g.diffCalls != 0 {
		t.Fatalf("Diff called %d times, want 0", g.diffCalls)
	}
}
