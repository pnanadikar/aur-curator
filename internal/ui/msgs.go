package ui

import (
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// Diff is an alias for the underlying diff type, keeping message
// signatures decoupled from the specific git implementation.
type Diff = git.GitDiff

// ---------------------------------------------------------------------------
// 1. Data loaded — results of tea.Cmds that called into injected interfaces
// ---------------------------------------------------------------------------

// ReviewLoadedMsg carries the result of a review-engine or store lookup.
type ReviewLoadedMsg struct {
	Pkg    domain.PackageName
	Review domain.Review
}

// DiffLoadedMsg carries a freshly fetched GitDiff.
type DiffLoadedMsg struct {
	Pkg  domain.PackageName
	Kind string // "PKGBUILD" | ".SRCINFO" | ".install" | "patch"
	Diff Diff
}

// FindingsLoadedMsg carries analyzer findings for a package.
type FindingsLoadedMsg struct {
	Pkg      domain.PackageName
	Findings []domain.Finding
}

// BuildLoadedMsg carries the latest build record for a package.
type BuildLoadedMsg struct {
	Pkg   domain.PackageName
	Build domain.Build
}

// RepoPackageEntry is a single published package (name + version).
type RepoPackageEntry struct {
	Name    string
	Version string
}

// RepoStatusLoadedMsg carries the current repository status.
type RepoStatusLoadedMsg struct {
	Initialized    bool
	LastRepoAdd    time.Time
	DiskUsageBytes int64
	Packages       []RepoPackageEntry
}

// ---------------------------------------------------------------------------
// 3. User-triggered actions
// ---------------------------------------------------------------------------

// DecisionRequestedMsg captures an Approve/Reject action. There is no
// domain.DecisionOutcome type — Decision is not a separate entity.
// Outcome reuses domain.ReviewStatus, restricted to its two terminal values.
type DecisionRequestedMsg struct {
	Pkg     domain.PackageName
	Outcome domain.ReviewStatus // domain.ReviewApproved | domain.ReviewRejected
	Notes   string
}

// ---------------------------------------------------------------------------
// 4. Operation lifecycle
// ---------------------------------------------------------------------------

// OperationProgressMsg reports partial progress on an active operation.
type OperationProgressMsg struct {
	OpID string
	Pct  int
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

// ErrorMsg wraps a synchronous (inline) failure for modal display.
type ErrorMsg struct{ Err error }

// clearToastMsg is privately emitted by setToast's own dismiss timer.
// It is intercepted by App.Update to clear a.toast when the timer's
// ID still matches — preventing the stale-dismiss race where an old
// timer wipes a new toast that was set while the previous one was
// visible.
type clearToastMsg struct {
	id int
}
