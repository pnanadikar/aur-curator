package build

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
)

// BuildResult reports where a completed build's log and artifacts can
// be found. Populated by the Builder implementation and consumed by
// Engine — Engine does not assume any particular file layout beyond
// what this struct reports.
type BuildResult struct {
	LogLocation   string
	ArtifactPaths []string // absolute paths to produced package files
}

// BuildOptions is what a build is told about its package besides the
// checked-out source.
type BuildOptions struct {
	// Package is the package being built, for messages.
	Package domain.PackageName
	// Network gives every run of the build the network, not only the
	// download of its sources: the package is in the config's
	// buildNetwork (#522).
	Network bool
}

// Builder executes a build using the source already checked out into
// workspaceDir (by Engine, before this is called). On success, the
// build's log and artifacts are available at the paths reported in the
// returned BuildResult. Builder implementations own everything about
// *how* the build runs (plain makepkg, clean chroot, sandboxed,
// remote) — callers only rely on this input/output contract.
type Builder interface {
	Build(ctx context.Context, workspaceDir string, logPath string, opts BuildOptions) (BuildResult, error)
}
