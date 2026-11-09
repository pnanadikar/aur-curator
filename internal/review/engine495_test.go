package review

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// TestPriorAUR covers which record a new review carries forward
// (domain.md § AUR Record, #495).
func TestPriorAUR(t *testing.T) {
	own := &domain.AURRecord{Maintainer: "own", ReadAt: time.Unix(200, 0).UTC(), ReviewID: "p@2"}
	carried := &domain.AURRecord{Maintainer: "carried", ReadAt: time.Unix(100, 0).UTC(), ReviewID: "p@1"}

	tests := []struct {
		name     string
		replaced *domain.Review
		want     *domain.AURRecord
	}{
		{"no earlier review", nil, nil},
		{"approved with a record", &domain.Review{Status: domain.ReviewApproved, AUR: own, PriorAUR: carried}, own},
		{"rejected with a record", &domain.Review{Status: domain.ReviewRejected, AUR: own, PriorAUR: carried}, own},
		{"approved without analysis", &domain.Review{Status: domain.ReviewApproved, PriorAUR: carried}, carried},
		{"pending, analysed, replaced", &domain.Review{Status: domain.ReviewPending, AUR: own, PriorAUR: carried}, carried},
		{"pending, not analysed", &domain.Review{Status: domain.ReviewPending, PriorAUR: carried}, carried},
		{"decided, nothing recorded anywhere", &domain.Review{Status: domain.ReviewApproved}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := priorAUR(tt.replaced); got != tt.want {
				t.Errorf("priorAUR = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestInitiateReviewCarriesAURRecord: a new review replacing a decided
// one carries that review's record; the replaced review's own fields
// (record, error, info) are not copied (#495).
func TestInitiateReviewCarriesAURRecord(t *testing.T) {
	rec := &domain.AURRecord{Maintainer: "alice", ReadAt: time.Unix(100, 0).UTC(), ReviewID: "yay@old"}
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@old",
			PackageName:    "yay",
			UpstreamCommit: "old",
			Status:         domain.ReviewApproved,
			AUR:            rec,
			AURInfo:        &domain.AURInfo{NumVotes: 3},
			AURError:       "stale",
		},
	}
	engine := NewEngine(&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "new"}},
		store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := engine.InitiateReview(context.Background(), "yay")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.PriorAUR, rec) {
		t.Errorf("PriorAUR = %+v, want %+v", r.PriorAUR, rec)
	}
	if r.AUR != nil || r.AURInfo != nil || r.AURError != "" {
		t.Errorf("new review has AUR %+v, AURInfo %+v, AURError %q; want none", r.AUR, r.AURInfo, r.AURError)
	}
}

// TestEnsureReviewHasNoPriorAUR: a first review (Import) has nothing to
// compare against.
func TestEnsureReviewHasNoPriorAUR(t *testing.T) {
	engine := NewEngine(&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "c1"}},
		&fakeReviewStore{}, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := engine.EnsureReview(context.Background(), "yay")
	if err != nil {
		t.Fatal(err)
	}
	if r.PriorAUR != nil {
		t.Errorf("PriorAUR = %+v, want nil", r.PriorAUR)
	}
}
