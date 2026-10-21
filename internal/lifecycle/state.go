package lifecycle

// State represents the lifecycle state of a package within aur-curator.
// It is a derived value computed from review status, merge status, and
// whether a review record exists — never persisted directly.
type State string

// String returns the underlying string value of the lifecycle state.
// It implements fmt.Stringer so State values format correctly with %s
// and %v verbs.
func (s State) String() string {
	return string(s)
}

const (
	// NotReviewed indicates the package has no review record.
	NotReviewed State = "Not Reviewed"

	// ReviewPending indicates the package has a review with
	// ReviewPending status.
	ReviewPending State = "Review Pending"

	// Approved indicates the package has an Approved review and
	// is not yet merged into master.
	Approved State = "Approved"

	// Merged indicates the package has an Approved review that
	// has been merged into master.
	Merged State = "Merged"

	// Rejected indicates the package has a Rejected review.
	Rejected State = "Rejected"

	// Built indicates the package has an Approved review that has been
	// merged into master and has a successful build record that has not
	// yet been confirmed by the user.
	Built State = "Built"

	// Confirmed indicates the package has a successful build that has been
	// confirmed by the user but not yet published to the package repository.
	Confirmed State = "Confirmed"

	// Published indicates the package build artifact has been
	// published to the local pacman repository. It is the final state:
	// installing is done with pacman (workflow.md § Installation, #351).
	Published State = "Published"
)
