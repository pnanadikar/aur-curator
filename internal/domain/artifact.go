package domain

// Artifact represents a single package file produced by a build.
type Artifact struct {
	ID           ArtifactID
	BuildID      BuildID
	PackageName  PackageName
	Filename     string
	Version      string
	Architecture string
	Checksum     string
	Size         int64
}
