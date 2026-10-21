package lifecycle

import "github.com/pramod/aur-curator/internal/domain"

// Inputs holds the arguments for DeriveState.
type Inputs struct {
	ReviewStatus domain.ReviewStatus
	HasReview    bool
	IsMerged     bool
	HeadCommit   string
	LastSuccess  *domain.Build
}

// DeriveState computes a human-readable lifecycle state string from
// the review status, merge status, last-success build record, and local HEAD commit.
// It is a pure function with no I/O and is reused by Package Details
// and Dashboard screens.
//
// When the review is Approved and merged, the last-success build determines
// whether the state advances from Merged to Built (on BuildSuccess, unconfirmed),
// Confirmed (on BuildSuccess, confirmed, unconfirmed), or Published (on
// BuildSuccess, confirmed, published).
//
// A build only counts toward a built/confirmed/published state when its
// ReviewedCommit exactly matches HeadCommit — this prevents stale states from
// persisting after a new merge has moved HEAD forward. An empty HeadCommit
// also means the build does not count (the HEAD is unknown).
func DeriveState(in Inputs) State {
	if !in.HasReview {
		return NotReviewed
	}

	// Treat last-success as nil unless its ReviewedCommit matches the local
	// HEAD commit. A mismatch or unknown HEAD (empty HeadCommit) means the
	// last-success was created from a different commit and no longer reflects
	// the current state of the package.
	lastSuccess := in.LastSuccess
	if lastSuccess != nil && (in.HeadCommit == "" || lastSuccess.ReviewedCommit != in.HeadCommit) {
		lastSuccess = nil
	}

	switch in.ReviewStatus {
	case domain.ReviewPending:
		return ReviewPending
	case domain.ReviewApproved:
		if in.IsMerged {
			if lastSuccess != nil && lastSuccess.Status == domain.BuildSuccess {
				if lastSuccess.Confirmed && lastSuccess.Published {
					return Published
				}
				if lastSuccess.Confirmed {
					return Confirmed
				}
				return Built
			}
			return Merged
		}
		return Approved
	case domain.ReviewRejected:
		return Rejected
	default:
		return NotReviewed
	}
}
