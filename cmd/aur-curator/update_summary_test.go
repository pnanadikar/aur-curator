package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/review"
)

// #402 — update ends with a summary of what happened and an exit status
// a wrapper can use (workflow.md § Summary and Exit Status).

func stalePlan(pkgs ...domain.PackageName) []updateCandidate {
	var cs []updateCandidate
	for _, p := range pkgs {
		cs = append(cs, updateCandidate{pkg: p, localVer: "1.0-1", aurVer: "1.1-1"})
	}
	return cs
}

var (
	started   = review.FetchResult{ReviewStarted: true, ReviewCommit: "0123456789abcdef"}
	noNew     = review.FetchResult{}
	emptyRead = review.FetchResult{EmptyResult: true}
)

func TestUpdateSummary(t *testing.T) {
	type fetchCall struct {
		pkg domain.PackageName
		res review.FetchResult
		err error
	}
	tests := []struct {
		name     string
		total    int
		plan     updatePlan
		fetches  []fetchCall
		readErr  error
		wantLine string
		wantExit int
	}{
		{
			name:     "nothing new",
			total:    3,
			plan:     updatePlan{upToDate: 3},
			wantLine: "All 3 curated packages match the AUR.",
			wantExit: exitUpdateNothingToReview,
		},
		{
			name:     "nothing new, but a review was already pending",
			total:    3,
			plan:     updatePlan{upToDate: 3, pending: []domain.PackageName{"yay"}},
			wantLine: "All 3 curated packages match the AUR.",
			wantExit: exitUpdateReviewsPending,
		},
		{
			name:  "fetched: a review started, another with no new commit",
			total: 5,
			plan:  updatePlan{upToDate: 3, batch: stalePlan("yay", "broot")},
			fetches: []fetchCall{
				{"yay", started, nil},
				{"broot", noNew, nil},
			},
			wantLine: "Fetched 2 (new reviews pending: yay); 3 up to date.",
			wantExit: exitUpdateReviewsPending,
		},
		{
			name:     "fetched with no new commit only: nothing to review",
			total:    2,
			plan:     updatePlan{upToDate: 1, batch: stalePlan("yay")},
			fetches:  []fetchCall{{"yay", noNew, nil}},
			wantLine: "Fetched 1; 1 up to date.",
			wantExit: exitUpdateNothingToReview,
		},
		{
			name:     "all skipped",
			total:    3,
			plan:     updatePlan{upToDate: 1, batch: stalePlan("yay", "broot")},
			wantLine: "2 skipped; 1 up to date.",
			wantExit: exitUpdateNothingToReview,
		},
		{
			name:  "skipped stale pending review: still to review",
			total: 1,
			plan: updatePlan{
				individual: []individualCandidate{{stalePlan("yay")[0], lossPendingReview}},
				pending:    []domain.PackageName{"yay"},
			},
			wantLine: "1 skipped.",
			wantExit: exitUpdateReviewsPending,
		},
		{
			name:     "fetch failed",
			total:    2,
			plan:     updatePlan{upToDate: 1, batch: stalePlan("pfetch")},
			fetches:  []fetchCall{{"pfetch", review.FetchResult{}, errors.New("timed out after 1m0s")}},
			wantLine: "1 failed; 1 up to date.",
			wantExit: exitUpdateFailed,
		},
		{
			name:     "fetch could not read the upstream commit",
			total:    1,
			plan:     updatePlan{batch: stalePlan("pfetch")},
			fetches:  []fetchCall{{"pfetch", emptyRead, nil}},
			wantLine: "1 failed.",
			wantExit: exitUpdateFailed,
		},
		{
			name:     "scan failure",
			total:    2,
			plan:     updatePlan{upToDate: 1, failed: []pkgReason{{"zed", "no review record"}}},
			wantLine: "1 failed; 1 up to date.",
			wantExit: exitUpdateFailed,
		},
		{
			name:     "not checked does not change the exit status",
			total:    3,
			plan:     updatePlan{upToDate: 2, notChecked: []pkgReason{{"foo-git", reasonNotOnAUR}}},
			wantLine: "1 not checked; 2 up to date.",
			wantExit: exitUpdateNothingToReview,
		},
		{
			name:     "read error is a failure",
			total:    2,
			plan:     updatePlan{upToDate: 1, batch: stalePlan("yay")},
			readErr:  errors.New("boom"),
			wantLine: "1 skipped; 1 up to date.",
			wantExit: exitUpdateFailed,
		},
		{
			name:  "mixed: failure beats pending reviews",
			total: 8,
			plan: updatePlan{
				upToDate:   2,
				batch:      stalePlan("yay", "broot", "pfetch", "skipme"),
				notChecked: []pkgReason{{"foo-git", reasonNotOnAUR}},
				failed:     []pkgReason{{"zed", "no review record"}},
			},
			fetches: []fetchCall{
				{"yay", started, nil},
				{"broot", started, nil},
				{"pfetch", review.FetchResult{}, errors.New("timed out")},
			},
			wantLine: "Fetched 2 (new reviews pending: yay, broot); 1 skipped; 2 failed; 1 not checked; 2 up to date.",
			wantExit: exitUpdateFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newUpdateSummary(tt.total, tt.plan)
			var out, errOut strings.Builder
			for _, f := range tt.fetches {
				s.recordFetch(&out, &errOut, f.pkg, f.res, f.err)
			}
			s.readErr = tt.readErr
			if got := s.line(); got != tt.wantLine {
				t.Errorf("line = %q, want %q", got, tt.wantLine)
			}
			if got := s.exitCode(); got != tt.wantExit {
				t.Errorf("exitCode = %d, want %d", got, tt.wantExit)
			}
		})
	}
}

// Exact wording of the per-fetch lines and the closing block, so a
// change is a deliberate one.
func TestUpdateSummaryOutput(t *testing.T) {
	s := newUpdateSummary(6, updatePlan{
		upToDate:   1,
		batch:      stalePlan("yay", "broot", "pfetch", "empty"),
		notChecked: []pkgReason{{"foo-git", reasonNotOnAUR}},
		failed:     []pkgReason{{"zed", "no review record"}},
	})
	var out, errOut strings.Builder
	s.recordFetch(&out, &errOut, "yay", started, nil)
	s.recordFetch(&out, &errOut, "broot", noNew, nil)
	s.recordFetch(&out, &errOut, "pfetch", review.FetchResult{}, errors.New("fetch pfetch: timed out after 1m0s"))
	s.recordFetch(&out, &errOut, "empty", emptyRead, nil)
	s.readErr = errors.New("boom")
	s.print(&out, &errOut)

	wantOut := "yay: fetch complete — review started for 0123456.\n" +
		"broot: fetch complete — no new upstream commit.\n" +
		"Not checked: foo-git: not on the AUR.\n" +
		"Fetched 2 (new reviews pending: yay); 3 failed; 1 not checked; 1 up to date.\n"
	if out.String() != wantOut {
		t.Errorf("stdout =\n%s\nwant\n%s", out.String(), wantOut)
	}
	wantErr := "pfetch: fetch failed: fetch pfetch: timed out after 1m0s\n" +
		"empty: fetch ran, but the upstream commit could not be read; no review step ran.\n" +
		"Failed: zed: no review record.\n" +
		"Failed: pfetch: fetch failed: fetch pfetch: timed out after 1m0s.\n" +
		"Failed: empty: fetch ran, but the upstream commit could not be read; no review step ran.\n" +
		"Failed: reading the answer: boom.\n"
	if errOut.String() != wantErr {
		t.Errorf("stderr =\n%s\nwant\n%s", errOut.String(), wantErr)
	}
}
