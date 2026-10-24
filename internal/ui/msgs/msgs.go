package msgs

import (
	"context"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
)

// Screen identifies one of the top-level application views.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenPackageDetail
	ScreenRepositoryStatus
	ScreenSettings
	ScreenReview
	ScreenHelp
)

// NavigateMsg instructs App to switch to a different screen.
type NavigateMsg struct {
	To       Screen
	Package  domain.PackageName // set when navigating to ScreenPackageDetail or ScreenReview
	FromMenu bool               // true when triggered by a Nav Menu digit key; clears history entirely
}

// BackMsg pops the top of the navigation stack, returning to the
// previous screen. Set Changed to true when the back navigation is
// triggered by a successful save that may have modified state on
// the destination screen, so the destination can selectively
// re-initialize rather than blindly reloading.
type BackMsg struct{ Changed bool }

// ReviewDecisionBackMsg carries the result of an Approve/Reject decision
// back to App for navigation. Unlike BackMsg, this carries the full
// *domain.Review so the destination screen can update state from
// in-memory data instead of re-fetching from disk. Only constructed at
// the review screen's reviewSavedMsg handler.
type ReviewDecisionBackMsg struct {
	Changed  bool
	Review   *domain.Review
	TraceCtx context.Context
}

// FetchRequestedMsg requests a package fetch.
type FetchRequestedMsg struct {
	Pkg      domain.PackageName
	TraceCtx context.Context
}

// AnalyzeRequestedMsg requests an analyzer run on the review with ReviewID,
// the review the screen shows (#399).
type AnalyzeRequestedMsg struct {
	Pkg      domain.PackageName
	ReviewID domain.ReviewID
	TraceCtx context.Context
}

// BuildRequestedMsg requests a package build.
type BuildRequestedMsg struct {
	Pkg      domain.PackageName
	TraceCtx context.Context
}

// ConfirmRequestedMsg requests a package build confirmation.
type ConfirmRequestedMsg struct {
	Pkg      domain.PackageName
	TraceCtx context.Context
}

// PublishRequestedMsg requests a package publish.
type PublishRequestedMsg struct {
	Pkg      domain.PackageName
	TraceCtx context.Context
}

// MergeRequestedMsg requests a package merge.
type MergeRequestedMsg struct {
	Pkg      domain.PackageName
	TraceCtx context.Context
}

// OperationLogMsg carries a textual log-append event for the Operations
// Panel lanes. It is distinct from OperationProgressMsg (numeric percent)
// so that the two message semantics remain separate types feeding the
// same per-lane append-only Log.
type OperationLogMsg struct {
	OpID    string
	Message string
}

// OperationCompletedMsg carries the successful result of an operation.
type OperationCompletedMsg struct {
	OpID     string
	Result   any // narrowed by OperationKind at the call site (e.g. formatted status string)
	Review   *domain.Review
	Build    *domain.Build
	TraceCtx context.Context
}

// OperationFailedMsg carries the error result of an operation.
type OperationFailedMsg struct {
	OpID     string
	Kind     string
	Err      error
	TraceCtx context.Context
}

// OperationCancelledMsg carries the result of a user-requested
// cancellation. Unlike OperationFailedMsg, this is not an error — the
// operation was intentionally aborted by the user and should be logged
// as status=cancelled reason=user_abort in the activity log, not as a
// failure in errors.log.
type OperationCancelledMsg struct {
	OpID     string
	TraceCtx context.Context
}

// ToastMsg carries a transient notification string dispatched to App.
type ToastMsg struct {
	Text string
}

// AURUpdateMsg carries the result of a bulk AUR update check for all
// curated packages. Populated by a parallel tea.Cmd and handled by
// the Dashboard model to refresh its version-aware summary cards.
type AURUpdateMsg struct {
	Pairs []aurinfo.VersionPair
	// Err is non-nil when the whole check failed; every package is then
	// unknown, never "up to date" (#331).
	Err      error
	TraceCtx context.Context
}

// QuitRequestedMsg asks App to quit. Screens emit it instead of
// returning tea.Quit directly, so every quit goes through App's
// active-operation guard (#179, #300).
type QuitRequestedMsg struct{}
