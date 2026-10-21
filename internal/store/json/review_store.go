package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// ReviewStore persists reviews in one JSON file per package.
type ReviewStore struct {
	paths *store.StorePaths
}

// NewReviewStore constructs a JSON-backed review store.
func NewReviewStore(paths *store.StorePaths) *ReviewStore {
	return &ReviewStore{paths: paths}
}

// Get reads the stored review for a package.
func (s *ReviewStore) Get(pkgName domain.PackageName) (*domain.Review, error) {
	if pkgName == "" {
		return nil, fmt.Errorf("empty package name")
	}

	path := s.paths.ReviewPath(pkgName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("read review %s: %w", path, err)
	}

	var review domain.Review
	if err := json.Unmarshal(data, &review); err != nil {
		return nil, fmt.Errorf("unmarshal review %s: %w", path, err)
	}
	return &review, nil
}

// Save overwrites the stored review for a package.
func (s *ReviewStore) Save(pkgName domain.PackageName, review *domain.Review) error {
	if pkgName == "" {
		return fmt.Errorf("empty package name")
	}
	if review == nil {
		return fmt.Errorf("save review for %s: nil review", pkgName)
	}

	dir := s.paths.PackageDir(pkgName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create review directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review for %s: %w", pkgName, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.paths.ReviewPath(pkgName), data, 0o644); err != nil {
		return fmt.Errorf("write review for %s: %w", pkgName, err)
	}
	return nil
}

// Delete removes the stored review for a package, if present.
func (s *ReviewStore) Delete(pkgName domain.PackageName) error {
	if pkgName == "" {
		return fmt.Errorf("empty package name")
	}
	if err := os.Remove(s.paths.ReviewPath(pkgName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete review for %s: %w", pkgName, err)
	}
	return nil
}
