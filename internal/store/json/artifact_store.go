package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// ArtifactStore persists artifacts in one JSON file per artifact ID.
type ArtifactStore struct {
	paths *store.StorePaths
}

// NewArtifactStore constructs a JSON-backed artifact store.
func NewArtifactStore(paths *store.StorePaths) *ArtifactStore {
	return &ArtifactStore{paths: paths}
}

// Get reads the stored artifact by its ID.
func (s *ArtifactStore) Get(id domain.ArtifactID) (*domain.Artifact, error) {
	if id == "" {
		return nil, fmt.Errorf("empty artifact ID")
	}

	path := s.paths.ArtifactPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("read artifact %s: %w", id, err)
	}

	var artifact domain.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("unmarshal artifact %s: %w", id, err)
	}
	return &artifact, nil
}

// Save persists the given artifact by its ID.
func (s *ArtifactStore) Save(id domain.ArtifactID, artifact *domain.Artifact) error {
	if id == "" {
		return fmt.Errorf("empty artifact ID")
	}
	if artifact == nil {
		return fmt.Errorf("save artifact %s: nil artifact", id)
	}

	dir := s.paths.ArtifactDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal artifact %s: %w", id, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.paths.ArtifactPath(id), data, 0o644); err != nil {
		return fmt.Errorf("write artifact %s: %w", id, err)
	}
	return nil
}

// Delete removes the stored artifact by ID, if present.
func (s *ArtifactStore) Delete(id domain.ArtifactID) error {
	if id == "" {
		return fmt.Errorf("empty artifact ID")
	}
	if err := os.Remove(s.paths.ArtifactPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete artifact %s: %w", id, err)
	}
	if err := os.Remove(s.paths.ArtifactDir(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete artifact %s directory: %w", id, err)
	}
	return nil
}

// ListAll returns all artifact records in the store. It may return a non-empty
// list of readable records along with a non-nil error. The error contains one
// entry per record that could not be read (file read failure, missing
// artifact.json, or JSON decode failure). A caller that cannot use partial
// data must treat any error as a failure.
func (s *ArtifactStore) ListAll() ([]domain.Artifact, error) {
	dir := s.paths.ArtifactsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read artifacts directory: %w", err)
	}

	var artifacts []domain.Artifact
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := domain.ArtifactID(entry.Name())
		artifactPath := s.paths.ArtifactPath(id)
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("artifact %s: record directory has no artifact.json", id))
			} else {
				errs = append(errs, fmt.Errorf("artifact %s: read: %w", id, err))
			}
			continue
		}
		var artifact domain.Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			errs = append(errs, fmt.Errorf("artifact %s: decode: %w", id, err))
			continue
		}
		artifacts = append(artifacts, artifact)
	}

	if len(errs) > 0 {
		return artifacts, errors.Join(errs...)
	}
	return artifacts, nil
}

// GetArtifactsByPackage returns all artifacts for a given package name. It may
// return a non-empty list of matching records along with a non-nil error. The
// error contains entries for any unreadable record across the entire store (not
// just the matching package), since unreadable records' packages are unknown. A
// caller that cannot use partial data must treat any error as a failure.
func (s *ArtifactStore) GetArtifactsByPackage(pkgName domain.PackageName) ([]domain.Artifact, error) {
	if pkgName == "" {
		return nil, fmt.Errorf("empty package name")
	}

	artifacts, err := s.ListAll()

	var result []domain.Artifact
	for _, a := range artifacts {
		if a.PackageName == pkgName {
			result = append(result, a)
		}
	}

	return result, err
}
