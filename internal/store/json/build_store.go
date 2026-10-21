package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// BuildStore persists builds in one JSON file per package.
type BuildStore struct {
	paths *store.StorePaths
}

// NewBuildStore constructs a JSON-backed build store.
func NewBuildStore(paths *store.StorePaths) *BuildStore {
	return &BuildStore{paths: paths}
}

// Get reads the stored build for a package.
func (s *BuildStore) Get(pkgName domain.PackageName) (*domain.Build, error) {
	if pkgName == "" {
		return nil, fmt.Errorf("empty package name")
	}

	path := s.paths.BuildPath(pkgName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("read build %s: %w", path, err)
	}

	var build domain.Build
	if err := json.Unmarshal(data, &build); err != nil {
		return nil, fmt.Errorf("unmarshal build %s: %w", path, err)
	}
	return &build, nil
}

// GetLastSuccess reads the last-success build record for a package.
// If the file does not exist and the latest attempt is a success, returns
// the latest attempt (read-only; does not write a new file).
func (s *BuildStore) GetLastSuccess(pkgName domain.PackageName) (*domain.Build, error) {
	if pkgName == "" {
		return nil, fmt.Errorf("empty package name")
	}

	path := s.paths.LastSuccessBuildPath(pkgName)
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read last-success build %s: %w", path, err)
		}
		// Fallback: if latest attempt is a success, return it without writing.
		latest, err := s.Get(pkgName)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrNotFound
			}
			return nil, fmt.Errorf("fallback get last-success build %s: %w", pkgName, err)
		}
		if latest.Status == domain.BuildSuccess {
			return latest, nil
		}
		return nil, domain.ErrNotFound
	}

	var build domain.Build
	if err := json.Unmarshal(data, &build); err != nil {
		return nil, fmt.Errorf("unmarshal last-success build %s: %w", path, err)
	}
	return &build, nil
}

// SaveLastSuccess persists the build as the last-known-success record.
func (s *BuildStore) SaveLastSuccess(pkgName domain.PackageName, build *domain.Build) error {
	if pkgName == "" {
		return fmt.Errorf("empty package name")
	}
	if build == nil {
		return fmt.Errorf("save last-success build for %s: nil build", pkgName)
	}

	dir := s.paths.PackageDir(pkgName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create last-success build directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(build, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal last-success build for %s: %w", pkgName, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.paths.LastSuccessBuildPath(pkgName), data, 0o644); err != nil {
		return fmt.Errorf("write last-success build for %s: %w", pkgName, err)
	}
	return nil
}

// Save overwrites the stored build for a package.
func (s *BuildStore) Save(pkgName domain.PackageName, build *domain.Build) error {
	if pkgName == "" {
		return fmt.Errorf("empty package name")
	}
	if build == nil {
		return fmt.Errorf("save build for %s: nil build", pkgName)
	}

	dir := s.paths.PackageDir(pkgName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create build directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(build, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal build for %s: %w", pkgName, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.paths.BuildPath(pkgName), data, 0o644); err != nil {
		return fmt.Errorf("write build for %s: %w", pkgName, err)
	}
	return nil
}

// Delete removes the stored build for a package, if present.
func (s *BuildStore) Delete(pkgName domain.PackageName) error {
	if pkgName == "" {
		return fmt.Errorf("empty package name")
	}
	if err := os.Remove(s.paths.BuildPath(pkgName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete build for %s: %w", pkgName, err)
	}
	return nil
}

// ReadBuildLog reads the build log file for a build and returns its content.
func (s *BuildStore) ReadBuildLog(buildID domain.BuildID, logPath string) (string, error) {
	if buildID == "" && logPath == "" {
		return "", fmt.Errorf("empty build ID and log path")
	}
	if logPath == "" {
		return "", fmt.Errorf("empty log path for build %s", buildID)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("read build log %s: %w", logPath, err)
	}
	return string(data), nil
}
