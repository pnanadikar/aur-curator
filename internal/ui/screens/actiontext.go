package screens

import "github.com/pramod/aur-curator/internal/lifecycle"

// Prerequisite texts for unavailable actions, shown in the full help
// (tui-package-details.md § Action Bar Convention). Each is written once
// and shared by every screen; the lifecycle ones state the rules in
// workflow.md § Action Availability.
const (
	prereqApproved      = "requires Approved"
	prereqMergedOrLater = "requires Merged or later"
	prereqBuilt         = "requires Built"
	prereqConfirmed     = "requires Confirmed"
	prereqPendingReview = "requires a Pending review"

	// Reasons the lifecycle does not express.
	reasonNoPackage      = "no package selected"
	reasonNothingToMerge = "nothing to merge"
	reasonNoReview       = "no review"
	reasonNoGit          = "git not configured"
	reasonPackagesCard   = "Packages card only"
)

// isMergeAvailable: Merge (T9) is available exactly when the package is
// Approved — the review is approved and its reviewed commit is not yet in
// master. Where upstream's tip is plays no part (#406).
func isMergeAvailable(lc lifecycle.State) bool {
	return lc == lifecycle.Approved
}

// isBuildAvailable reports whether Build (T10) or rebuild (T15) is
// available in state lc (workflow.md § Dashboard / Package Details). Local
// commits never disable it: the Build confirmation flow asks instead. Both
// screens use it, so they cannot disagree.
func isBuildAvailable(lc lifecycle.State) bool {
	switch lc {
	case lifecycle.Merged, lifecycle.Built, lifecycle.Confirmed, lifecycle.Published:
		return true
	}
	return false
}

// mergeReason is why Merge is unavailable in lc: an approved review whose
// commit is already merged has nothing to merge; anything else is not
// approved yet.
func mergeReason(lc lifecycle.State) string {
	switch lc {
	case lifecycle.Merged, lifecycle.Built, lifecycle.Confirmed, lifecycle.Published:
		return reasonNothingToMerge
	default:
		return prereqApproved
	}
}
