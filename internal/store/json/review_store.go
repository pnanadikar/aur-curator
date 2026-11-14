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
	path, err := s.paths.ReviewPath(pkgName)
	if err != nil {
		return nil, err
	}
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
	if review == nil {
		return fmt.Errorf("save review for %s: nil review", pkgName)
	}

	dir, err := s.paths.PackageDir(pkgName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create review directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review for %s: %w", pkgName, err)
	}
	data = append(data, '\n')

	file, err := s.paths.ReviewPath(pkgName)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return fmt.Errorf("write review for %s: %w", pkgName, err)
	}
	return nil
}

// Delete removes the stored review for a package, if present.
func (s *ReviewStore) Delete(pkgName domain.PackageName) error {
	file, err := s.paths.ReviewPath(pkgName)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete review for %s: %w", pkgName, err)
	}
	return nil
}
