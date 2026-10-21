package lifecycle

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestDeriveState(t *testing.T) {
	tests := []struct {
		name        string
		status      domain.ReviewStatus
		hasReview   bool
		isMerged    bool
		headCommit  string
		lastSuccess *domain.Build
		wantState   State
	}{
		{
			name:        "pending review",
			status:      domain.ReviewPending,
			hasReview:   true,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   ReviewPending,
		},
		{
			name:        "approved review not merged",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   Approved,
		},
		{
			name:        "approved review merged no last-success",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			lastSuccess: nil,
			wantState:   Merged,
		},
		{
			name:        "approved review merged last-success success",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
			wantState:   Built,
		},
		{
			name:        "approved review merged last-success failed",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			lastSuccess: &domain.Build{Status: domain.BuildFailed},
			wantState:   Merged,
		},
		{
			name:        "approved review merged last-success pending",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			lastSuccess: &domain.Build{Status: domain.BuildPending},
			wantState:   Merged,
		},
		{
			name:        "approved review merged last-success running",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			lastSuccess: &domain.Build{Status: domain.BuildRunning},
			wantState:   Merged,
		},
		{
			name:        "rejected review",
			status:      domain.ReviewRejected,
			hasReview:   true,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   Rejected,
		},
		{
			name:        "no review",
			status:      domain.ReviewPending,
			hasReview:   false,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   NotReviewed,
		},
		{
			name:        "no review with approved status",
			status:      domain.ReviewApproved,
			hasReview:   false,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   NotReviewed,
		},
		{
			name:        "unknown status without review",
			status:      "unknown",
			hasReview:   false,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   NotReviewed,
		},
		{
			name:        "unknown status with review",
			status:      "unknown",
			hasReview:   true,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   NotReviewed,
		},
		{
			name:        "approved but merged flag false",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    false,
			lastSuccess: nil,
			wantState:   Approved,
		},
		{
			name:        "no review with merged flag true",
			status:      domain.ReviewApproved,
			hasReview:   false,
			isMerged:    true,
			lastSuccess: nil,
			wantState:   NotReviewed,
		},
		{
			name:        "rejected with merged flag true",
			status:      domain.ReviewRejected,
			hasReview:   true,
			isMerged:    true,
			lastSuccess: nil,
			wantState:   Rejected,
		},
		{
			name:        "Confirmed from Success+Confirmed (no Published)",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, Confirmed: true, Published: false, ReviewedCommit: "abc123"},
			wantState:   Confirmed,
		},
		{
			name:        "Published from Success+Confirmed+Published",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, Confirmed: true, Published: true, ReviewedCommit: "abc123"},
			wantState:   Published,
		},
		{
			name:        "build ReviewedCommit matches HeadCommit",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
			wantState:   Built,
		},
		{
			name:        "build ReviewedCommit mismatch with HeadCommit",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "def456",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
			wantState:   Merged,
		},
		{
			name:        "Confirmed build ReviewedCommit mismatch with HeadCommit",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "def456",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, Confirmed: true, ReviewedCommit: "abc123"},
			wantState:   Merged,
		},
		{
			name:        "Published build ReviewedCommit mismatch with HeadCommit",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "def456",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, Confirmed: true, Published: true, ReviewedCommit: "abc123"},
			wantState:   Merged,
		},
		{
			name:        "empty HeadCommit — last-success ignored",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "abc123"},
			wantState:   Merged,
		},
		{
			name:        "nil lastSuccess with any HeadCommit",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: nil,
			wantState:   Merged,
		},
		{
			name:        "Published but not Confirmed falls back to Built",
			status:      domain.ReviewApproved,
			hasReview:   true,
			isMerged:    true,
			headCommit:  "abc123",
			lastSuccess: &domain.Build{Status: domain.BuildSuccess, Confirmed: false, Published: true, ReviewedCommit: "abc123"},
			wantState:   Built,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveState(Inputs{
				ReviewStatus: tt.status,
				HasReview:    tt.hasReview,
				IsMerged:     tt.isMerged,
				HeadCommit:   tt.headCommit,
				LastSuccess:  tt.lastSuccess,
			})
			if got != tt.wantState {
				t.Errorf("DeriveState(%q, %v, %v, %q, %v) = %q, want %q", tt.status, tt.hasReview, tt.isMerged, tt.headCommit, tt.lastSuccess, got, tt.wantState)
			}
		})
	}
}
