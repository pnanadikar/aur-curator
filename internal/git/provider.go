package git

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
)

// Provider exposes Git operations required by aur-curator.
type Provider interface {
	Import(ctx context.Context, packageName domain.PackageName) error
	Fetch(ctx context.Context, packageName domain.PackageName) error
	Status(ctx context.Context, packageName domain.PackageName) (*RepositoryStatus, error)
	// CommitsBehind returns the number of commits local master is behind
	// upstream/master.
	//
	// It exists as a cheap count-only query for listing views such as the
	// Dashboard. Callers that already have a GitDiff (for example Package
	// Details or the Review screen) should use len(diff.Commits) instead of
	// making a second Git call.
	CommitsBehind(ctx context.Context, packageName domain.PackageName) (int, error)

	// UpstreamCommit returns the commit hash the local upstream-tracking ref
	// currently points at. This is a purely local read — no network call,
	// reflecting whatever the most recent Fetch last recorded.
	UpstreamCommit(ctx context.Context, packageName domain.PackageName) (string, error)

	// IsMerged reports whether the reviewed commit (reviewedCommit) has
	// been incorporated into local master using `git merge-base
	// --is-ancestor`. The reviewedCommit is Review.UpstreamCommit — the
	// specific commit that was reviewed and approved. Only git's "not an
	// ancestor" answer is (false, nil); any other failure is (false, err),
	// which callers treat as not merged (fail-closed) and report (#462).
	IsMerged(ctx context.Context, packageName domain.PackageName, reviewedCommit string) (bool, error)

	// Diff returns the live comparison of local master with
	// upstream/master, including their merge base (GitDiff.BaseCommit),
	// which a new Review stores as the start of its range.
	Diff(ctx context.Context, packageName domain.PackageName) (*GitDiff, error)

	// ReviewDiff returns the diff of a stored review range, base to
	// upstream, independent of where master or upstream/master are now
	// (domain.md § Reviewed Range, #387). An empty base is a first review.
	// A commit missing from the repository is an error naming it.
	ReviewDiff(ctx context.Context, packageName domain.PackageName, base, upstream string) (*GitDiff, error)

	Merge(ctx context.Context, packageName domain.PackageName, commit string) error
	LocalOnlyCommits(ctx context.Context, packageName domain.PackageName) (int, error)
	Metadata(ctx context.Context, packageName domain.PackageName) (*RepositoryMetadata, error)

	// CloneAt creates a fresh, fully independent local clone of pkgName's
	// repository at commit, into destDir. destDir must not already exist.
	// Unlike a git worktree, the resulting clone shares no state (object
	// database, refs) with the source repository — any git operation
	// performed inside destDir, however destructive, cannot affect the
	// managed repository this was cloned from. Callers own removing
	// destDir when finished; CloneAt does no cleanup of its own.
	CloneAt(ctx context.Context, pkgName domain.PackageName, commit string, destDir string) error

	// LocalHeadCommit returns the commit hash local master currently
	// points at. This is a purely local read — no network call.
	LocalHeadCommit(ctx context.Context, pkgName domain.PackageName) (string, error)

	// FileAtCommit returns the content of a file at a specific commit
	// in pkgName's repository, via `git show <commit>:<path>`. Returns
	// the raw file content as a string. Use for reading a file's
	// historical content (e.g., a PKGBuild at a review's UpstreamCommit)
	// without checking out that commit.
	FileAtCommit(ctx context.Context, packageName domain.PackageName, commit, path string) (string, error)
}
