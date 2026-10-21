package domain

// PackageName is a package's natural key — its actual name (e.g.
// "yay"). There is no separate surrogate ID; PackageName is the sole
// identifier used everywhere a package is referenced.
type PackageName string

// ReviewID identifies a Review.
type ReviewID string

// FindingID identifies a Finding.
type FindingID string

// RiskAssessmentID identifies a RiskAssessment.
type RiskAssessmentID string

// BuildID identifies a Build.
type BuildID string

// ArtifactID identifies an Artifact.
type ArtifactID string

// WorkspaceID identifies a Workspace.
type WorkspaceID string

// GitRepositoryID identifies a GitRepository.
type GitRepositoryID string

// PackageRepositoryID identifies a PackageRepository.
type PackageRepositoryID string

// PolicyConfigID identifies a PolicyConfig.
type PolicyConfigID string
