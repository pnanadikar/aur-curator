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

// ArtifactStore persists artifact records.
type ArtifactStore interface {
	Get(id ArtifactID) (*Artifact, error)
	Save(id ArtifactID, artifact *Artifact) error

	// GetArtifactsByPackage returns all artifacts for a given package name. It
	// may return a non-empty list of matching records along with a non-nil
	// error. The error contains entries for any unreadable record across the
	// entire store, since unreadable records' packages are unknown. A caller
	// that cannot use partial data must treat any error as a failure.
	GetArtifactsByPackage(pkgName PackageName) ([]Artifact, error)

	Delete(id ArtifactID) error

	// ListAll returns all artifact records in the store. It may return a
	// non-empty list of readable records along with a non-nil error. The error
	// contains one entry per record that could not be read (file read failure,
	// missing artifact.json, or JSON decode failure). A caller that cannot use
	// partial data must treat any error as a failure.
	ListAll() ([]Artifact, error)
}
