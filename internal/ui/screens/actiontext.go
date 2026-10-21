package screens

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
