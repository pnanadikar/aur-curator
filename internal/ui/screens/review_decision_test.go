package screens

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// TestDecisionComment checks that the activity log's comment comes from the
// field each decision actually writes: reviewer notes for an approval, the
// reason (Rationale) for a rejection.
func TestDecisionComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		review domain.Review
		want   string
	}{
		{
			name:   "approve logs reviewer notes",
			review: domain.Review{Status: domain.ReviewApproved, ReviewerNotes: "looks fine", Rationale: "ignored"},
			want:   "looks fine",
		},
		{
			name:   "approve with no notes logs empty",
			review: domain.Review{Status: domain.ReviewApproved},
			want:   "",
		},
		{
			name:   "reject logs rationale",
			review: domain.Review{Status: domain.ReviewRejected, ReviewerNotes: "", Rationale: "new download URL"},
			want:   "new download URL",
		},
		{
			name:   "reject with skipped reason logs empty",
			review: domain.Review{Status: domain.ReviewRejected},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decisionComment(&tc.review); got != tc.want {
				t.Errorf("decisionComment() = %q, want %q", got, tc.want)
			}
		})
	}
}
