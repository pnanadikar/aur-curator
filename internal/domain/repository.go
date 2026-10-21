package domain

// PackageRepository represents the local package repository.
type PackageRepository struct {
	Location      string
	SigningStatus string // reserved: package signing not yet scoped, see roadmap.md Phase 3
}
