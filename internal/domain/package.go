package domain

// Package represents one curated AUR package managed by aur-curator.
type Package struct {
	Name PackageName

	GitRepositoryID GitRepositoryID

	CurrentReviewID *ReviewID

	LatestBuildID *BuildID
}
