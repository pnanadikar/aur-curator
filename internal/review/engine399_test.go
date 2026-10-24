package review

import (
	"context"
	"errors"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// guardCase is one row of the review-ID guard tables (#399): the review in
// the store, and the ID the caller says it is acting on.
type guardCase struct {
	name         string
	storedID     domain.ReviewID
	storedStatus domain.ReviewStatus
	expected     domain.ReviewID
	wantChanged  bool   // want a *ReviewChangedError
	wantErr      string // otherwise, the exact error text ("" = no error)
}

func guardStore(tc guardCase) *fakeReviewStore {
	return &fakeReviewStore{review: &domain.Review{
		ID:             tc.storedID,
		PackageName:    "yay",
		Status:         tc.storedStatus,
		FindingsStatus: domain.FindingsNotRun,
	}}
}

// checkChanged asserts err is the *ReviewChangedError for tc.
func checkChanged(t *testing.T, tc guardCase, err error) {
	t.Helper()
	var rce *ReviewChangedError
	if !errors.As(err, &rce) {
		t.Fatalf("err = %v, want *ReviewChangedError", err)
	}
	if rce.Expected != tc.expected || rce.Current.ID != tc.storedID {
		t.Fatalf("Expected/Current.ID = %q/%q, want %q/%q", rce.Expected, rce.Current.ID, tc.expected, tc.storedID)
	}
	want := "review for yay changed while it was being viewed: expected " + string(tc.expected) + ", stored " + string(tc.storedID)
	if err.Error() != want {
		t.Fatalf("err.Error() = %q\nwant          %q", err.Error(), want)
	}
}

func TestReviewChangedErrorText399(t *testing.T) {
	cases := []struct {
		expected, stored domain.ReviewID
		want             string
	}{
		{"yay@A", "yay@B", "review for yay changed while it was being viewed: expected yay@A, stored yay@B"},
		{"", "yay@A", "review for yay changed while it was being viewed: expected , stored yay@A"},
	}
	for _, c := range cases {
		err := &ReviewChangedError{Package: "yay", Expected: c.expected, Current: domain.Review{ID: c.stored}}
		if got := err.Error(); got != c.want {
			t.Errorf("Error() = %q\nwant      %q", got, c.want)
		}
	}
}

func TestRunAnalyzersReviewIDGuard399(t *testing.T) {
	cases := []guardCase{
		{name: "matching pending", storedID: "yay@A", storedStatus: domain.ReviewPending, expected: "yay@A"},
		{name: "replaced pending", storedID: "yay@B", storedStatus: domain.ReviewPending, expected: "yay@A", wantChanged: true},
		{name: "replaced decided", storedID: "yay@B", storedStatus: domain.ReviewApproved, expected: "yay@A", wantChanged: true},
		{name: "matching decided", storedID: "yay@A", storedStatus: domain.ReviewApproved, expected: "yay@A",
			wantErr: "cannot run analyzers on a review that is already decided"},
		{name: "empty expected", storedID: "yay@A", storedStatus: domain.ReviewPending, expected: "", wantChanged: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := guardStore(tc)
			g := &fakeGitProvider{diff: &git.GitDiff{}}
			e := NewEngine(g, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

			r, err := e.RunAnalyzers(context.Background(), "yay", tc.expected)

			switch {
			case tc.wantChanged:
				checkChanged(t, tc, err)
			case tc.wantErr != "":
				var rce *ReviewChangedError
				if err == nil || errors.As(err, &rce) || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want exactly %q", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if r.FindingsStatus != domain.FindingsRun {
					t.Fatalf("FindingsStatus = %q, want %q", r.FindingsStatus, domain.FindingsRun)
				}
				if store.saveCalls != 1 || g.reviewDiffCalls != 1 {
					t.Fatalf("saves/ReviewDiff calls = %d/%d, want 1/1", store.saveCalls, g.reviewDiffCalls)
				}
				return
			}
			// Refused: nothing read, run or saved.
			if store.saveCalls != 0 || g.reviewDiffCalls != 0 {
				t.Fatalf("saves/ReviewDiff calls = %d/%d, want 0/0", store.saveCalls, g.reviewDiffCalls)
			}
			if store.review.FindingsStatus != domain.FindingsNotRun {
				t.Fatalf("stored FindingsStatus = %q, want unchanged %q", store.review.FindingsStatus, domain.FindingsNotRun)
			}
		})
	}
}

func TestRecordDecisionReviewIDGuard399(t *testing.T) {
	cases := []guardCase{
		{name: "matching pending", storedID: "yay@A", storedStatus: domain.ReviewPending, expected: "yay@A"},
		{name: "replaced pending", storedID: "yay@B", storedStatus: domain.ReviewPending, expected: "yay@A", wantChanged: true},
		{name: "replaced decided", storedID: "yay@B", storedStatus: domain.ReviewApproved, expected: "yay@A", wantChanged: true},
		{name: "matching decided", storedID: "yay@A", storedStatus: domain.ReviewApproved, expected: "yay@A",
			wantErr: "review already decided"},
		{name: "empty expected", storedID: "yay@A", storedStatus: domain.ReviewPending, expected: "", wantChanged: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := guardStore(tc)
			e := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

			r, err := e.RecordDecision(context.Background(), "yay", domain.ReviewApproved, "", "n", tc.expected)

			switch {
			case tc.wantChanged:
				checkChanged(t, tc, err)
			case tc.wantErr != "":
				var rce *ReviewChangedError
				if err == nil || errors.As(err, &rce) || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want exactly %q", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if r.Status != domain.ReviewApproved || store.saveCalls != 1 {
					t.Fatalf("status/saves = %q/%d, want %q/1", r.Status, store.saveCalls, domain.ReviewApproved)
				}
				return
			}
			// Refused: the stored review is untouched.
			got := store.review
			if store.saveCalls != 0 || got.Status != tc.storedStatus || got.ReviewerNotes != "" || !got.DecisionTime.IsZero() {
				t.Fatalf("after refusal: saves=%d status=%q notes=%q decided=%v; want 0, %q, \"\", zero",
					store.saveCalls, got.Status, got.ReviewerNotes, got.DecisionTime, tc.storedStatus)
			}
		})
	}
}
