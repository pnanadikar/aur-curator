package build

import "context"

// BuildResult reports where a completed build's log and artifacts can
// be found. Populated by the Builder implementation and consumed by
// Engine — Engine does not assume any particular file layout beyond
// what this struct reports.
type BuildResult struct {
	LogLocation   string
	ArtifactPaths []string // absolute paths to produced package files
}

// Builder executes a build using the source already checked out into
// workspaceDir (by Engine, before this is called). On success, the
// build's log and artifacts are available at the paths reported in the
// returned BuildResult. Builder implementations own everything about
// *how* the build runs (plain makepkg, clean chroot, sandboxed,
// remote) — callers only rely on this input/output contract.
type Builder interface {
	Build(ctx context.Context, workspaceDir string, logPath string) (BuildResult, error)
}
