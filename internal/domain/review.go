package domain

import (
	"errors"
	"time"
)

// ReviewStatus represents the current review decision.
type ReviewStatus string

const (
	// ReviewPending indicates that no decision has been recorded yet.
	ReviewPending ReviewStatus = "pending"
	// ReviewApproved indicates that the upstream changes were approved.
	ReviewApproved ReviewStatus = "approved"
	// ReviewRejected indicates that the upstream changes were rejected.
	ReviewRejected ReviewStatus = "rejected"
)

// FindingsStatus distinguishes reviews that never ran analyzers from
// reviews whose analyzers completed.
type FindingsStatus string

const (
	// FindingsNotRun indicates that no analyzers have been executed.
	FindingsNotRun FindingsStatus = "not_run"
	// FindingsRun indicates that analyzers completed, even if they found nothing.
	FindingsRun FindingsStatus = "run"
)

// Review captures the persisted decision for one package's current review.
type Review struct {
	ID             ReviewID        `json:"id"`
	PackageName    PackageName     `json:"packageName"`
	UpstreamCommit string          `json:"upstreamCommit"`
	BaseCommit     string          `json:"baseCommit"` // start of the reviewed range; "" on a first review (Import) — domain.md § Reviewed Range
	Status         ReviewStatus    `json:"status"`     // Pending, Approved, Rejected
	DecisionTime   time.Time       `json:"decisionTime"`
	ReviewerNotes  string          `json:"reviewerNotes"`
	Rationale      string          `json:"rationale"` // optional, captured on Approve-without-analysis or Reject
	Findings       []Finding       `json:"findings"`  // may be nil/empty — see FindingsStatus
	FindingsStatus FindingsStatus  `json:"findingsStatus"`
	RiskAssessment *RiskAssessment `json:"riskAssessment,omitempty"`

	// AUR is the AUR's maintainer record as this review's analysis read
	// it; nil until the analyzers run and the read succeeds. AURInfo is
	// the package's standing from the same read. AURError says why the
	// read failed. PriorAUR is the last recorded maintainer record from
	// an earlier review, set when this review is created; this review's
	// analysis compares against it. domain.md § AUR Record (#495).
	AUR      *AURRecord `json:"aur,omitempty"`
	AURInfo  *AURInfo   `json:"aurInfo,omitempty"`
	AURError string     `json:"aurError,omitempty"`
	PriorAUR *AURRecord `json:"priorAur,omitempty"`
}

// AURRecord is the AUR's maintainer record as one review's analysis read
// it (domain.md § AUR Record, #495).
type AURRecord struct {
	Maintainer    string    `json:"maintainer"` // "" when Orphaned
	Orphaned      bool      `json:"orphaned"`
	CoMaintainers []string  `json:"coMaintainers,omitempty"` // sorted
	ReadAt        time.Time `json:"readAt"`
	ReviewID      ReviewID  `json:"reviewId"` // the review whose analysis read it
}

// AURInfo is the package's standing on the AUR, from the same read as an
// AURRecord. Shown on the Findings tab; never a finding (#495).
type AURInfo struct {
	FirstSubmitted time.Time `json:"firstSubmitted"`
	NumVotes       int       `json:"numVotes"`
	Popularity     float64   `json:"popularity"`
}

// ReviewStore persists review records.
type ReviewStore interface {
	// Get returns the stored review for a package or ErrNotFound if none exists.
	Get(name PackageName) (*Review, error)
	// Save overwrites the stored review for a package with the provided record.
	Save(name PackageName, review *Review) error
	// Delete removes any stored review for a package.
	Delete(name PackageName) error
}

// ErrNotFound indicates that no stored review exists for the package.
var ErrNotFound = errors.New("review: not found")
