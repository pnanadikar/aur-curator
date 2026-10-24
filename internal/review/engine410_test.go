package review

import (
	"context"
	"errors"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// TestRunAnalyzersFailureSavesNothing410: when the analysis is incomplete —
// an analyzer failed, or ctx was cancelled while the analyzers ran —
// RunAnalyzers returns an error and the stored review keeps its previous
// Findings and FindingsStatus (#410).
func TestRunAnalyzersFailureSavesNothing410(t *testing.T) {
	boom := errors.New(`analyzer "shell" failed: boom`)
	prior := []domain.Finding{{ID: "f-old", Category: "url_new"}}

	cases := []struct {
		name        string
		priorStatus domain.FindingsStatus
		priorFinds  []domain.Finding
		analyzerErr error
		cancel      bool // cancel ctx while the analyzers run
		wantIs      error
		wantErr     string
	}{
		{name: "analyzer fails, never run", priorStatus: domain.FindingsNotRun,
			analyzerErr: boom, wantIs: boom,
			wantErr: `analysis of yay not saved: analyzer "shell" failed: boom`},
		{name: "analyzer fails, re-run", priorStatus: domain.FindingsRun, priorFinds: prior,
			analyzerErr: boom, wantIs: boom,
			wantErr: `analysis of yay not saved: analyzer "shell" failed: boom`},
		{name: "cancelled during run", priorStatus: domain.FindingsNotRun,
			cancel: true, wantIs: context.Canceled,
			wantErr: "analysis of yay not saved: context canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeReviewStore{review: &domain.Review{
				ID:             "yay@abc",
				PackageName:    "yay",
				Status:         domain.ReviewPending,
				FindingsStatus: tc.priorStatus,
				Findings:       tc.priorFinds,
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			analyzers := &fakeAnalyzerManager{
				// Partial findings from the analyzers that did finish.
				findings: []domain.Finding{{Category: "checksum_refresh"}},
				err:      tc.analyzerErr,
			}
			if tc.cancel {
				analyzers.onRun = cancel
			}
			e := NewEngine(&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}}, store, analyzers, &fakePolicyEngine{})

			r, err := e.RunAnalyzers(ctx, "yay", "yay@abc")

			if r != nil {
				t.Fatalf("review = %+v, want nil", r)
			}
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("err = %v, want it to wrap %v", err, tc.wantIs)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("err.Error() = %q\nwant          %q", err.Error(), tc.wantErr)
			}
			if store.saveCalls != 0 {
				t.Fatalf("saveCalls = %d, want 0", store.saveCalls)
			}
			if store.review.FindingsStatus != tc.priorStatus {
				t.Fatalf("FindingsStatus = %q, want %q", store.review.FindingsStatus, tc.priorStatus)
			}
			if len(store.review.Findings) != len(tc.priorFinds) {
				t.Fatalf("stored findings = %+v, want %+v", store.review.Findings, tc.priorFinds)
			}
		})
	}
}
