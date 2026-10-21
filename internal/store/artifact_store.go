package store

import "github.com/pramod/aur-curator/internal/domain"

// ArtifactStore persists artifact records.
type ArtifactStore interface {
	Get(id domain.ArtifactID) (*domain.Artifact, error)
	Save(id domain.ArtifactID, artifact *domain.Artifact) error

	// GetArtifactsByPackage returns all artifacts for a given package name. It
	// may return a non-empty list of matching records along with a non-nil
	// error. The error contains entries for any unreadable record across the
	// entire store, since unreadable records' packages are unknown. A caller
	// that cannot use partial data must treat any error as a failure.
	GetArtifactsByPackage(pkgName domain.PackageName) ([]domain.Artifact, error)

	Delete(id domain.ArtifactID) error

	// ListAll returns all artifact records in the store. It may return a
	// non-empty list of readable records along with a non-nil error. The error
	// contains one entry per record that could not be read (file read failure,
	// missing artifact.json, or JSON decode failure). A caller that cannot use
	// partial data must treat any error as a failure.
	ListAll() ([]domain.Artifact, error)
}
