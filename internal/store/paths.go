package store

import (
	"path/filepath"

	"github.com/pramod/aur-curator/internal/domain"
)

// StorePaths is the single owner of all .aur-curator/ path
// construction. No store or engine should construct these paths
// independently.
type StorePaths struct {
	workspace string
}

// NewStorePaths constructs a StorePaths rooted at workspace.
func NewStorePaths(workspace string) *StorePaths {
	return &StorePaths{workspace: workspace}
}

// RootPath returns the workspace root used to construct all paths.
func (p *StorePaths) RootPath() string {
	return p.workspace
}

// Per-package data: <workspace>/.aur-curator/packages/<pkg>/

// PackagesDir returns the workspace-level packages directory.
func (p *StorePaths) PackagesDir() string {
	return filepath.Join(p.workspace, ".aur-curator", "packages")
}

// PackageDir returns the per-package data directory.
func (p *StorePaths) PackageDir(pkg domain.PackageName) string {
	return filepath.Join(p.PackagesDir(), string(pkg))
}

// ReviewPath returns the path to a package's review.json file.
func (p *StorePaths) ReviewPath(pkg domain.PackageName) string {
	return filepath.Join(p.PackageDir(pkg), "review.json")
}

// BuildPath returns the path to a package's build.json file.
func (p *StorePaths) BuildPath(pkg domain.PackageName) string {
	return filepath.Join(p.PackageDir(pkg), "build.json")
}

// LastSuccessBuildPath returns the path to a package's last-success.json file.
func (p *StorePaths) LastSuccessBuildPath(pkg domain.PackageName) string {
	return filepath.Join(p.PackageDir(pkg), "last-success.json")
}

// Workspace-level data

// ArtifactsDir returns the workspace-level artifacts directory.
func (p *StorePaths) ArtifactsDir() string {
	return filepath.Join(p.workspace, ".aur-curator", "artifacts")
}

// ArtifactDir returns the directory for a specific artifact.
func (p *StorePaths) ArtifactDir(id domain.ArtifactID) string {
	return filepath.Join(p.ArtifactsDir(), string(id))
}

// ArtifactPath returns the path to an artifact's artifact.json file.
func (p *StorePaths) ArtifactPath(id domain.ArtifactID) string {
	return filepath.Join(p.ArtifactDir(id), "artifact.json")
}

// BuildsDir returns the workspace-level builds directory.
func (p *StorePaths) BuildsDir() string {
	return filepath.Join(p.workspace, ".aur-curator", "builds")
}

// BuildLogPath returns the path to a build's build.log file.
func (p *StorePaths) BuildLogPath(id domain.BuildID) string {
	return filepath.Join(p.BuildsDir(), string(id), "build.log")
}

// BuildArtifactsDir returns the directory for a build's artifact binaries.
func (p *StorePaths) BuildArtifactsDir(id domain.BuildID) string {
	return filepath.Join(p.BuildsDir(), string(id))
}

// BuildWorkDir returns a build's workspace: the directory the package is
// cloned into (under "src") and built in. It lives inside the build's own
// directory, so whatever removes that directory (a newer build's purge,
// remove, check's orphan sweep) removes the workspace with it (#307).
func (p *StorePaths) BuildWorkDir(id domain.BuildID) string {
	return filepath.Join(p.BuildArtifactsDir(id), "work")
}
