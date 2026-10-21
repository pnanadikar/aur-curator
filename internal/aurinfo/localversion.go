package aurinfo

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/lifecycle"
)

// LocalVersionResolver resolves a package's local version from its
// PKGBUILD content. Implementations must read git-committed content —
// never a working-tree disk read — per workflow.md § Local Version —
// Git-Committed, Not Working Tree, since Build (internal/build/engine.go)
// clones and builds only from a resolved commit, never the working tree.
type LocalVersionResolver func(ctx context.Context, pkg domain.PackageName) (version string, unknown bool)

// NewGitLocalVersionResolver returns a LocalVersionResolver that reads
// the PKGBUILD relevant to a package's current lifecycle state:
//   - ReviewPending: content at the pending review's UpstreamCommit
//     (matches update's bucket-2 behavior).
//   - Approved/Merged/Built/Confirmed/Rejected/Published: content at
//     local HEAD (matches update's bucket-3 / parseLocalMasterVersion).
//   - Anything else (no review yet, review lookup error): unknown.
func NewGitLocalVersionResolver(provider git.Provider, reviewDB domain.ReviewStore) LocalVersionResolver {
	return func(ctx context.Context, pkg domain.PackageName) (string, bool) {
		review, err := reviewDB.Get(pkg)
		if err != nil {
			return "", true
		}

		var isMerged bool
		if review.UpstreamCommit != "" {
			isMerged, _ = provider.IsMerged(ctx, pkg, review.UpstreamCommit)
		}
		state := lifecycle.DeriveState(lifecycle.Inputs{
			ReviewStatus: review.Status,
			HasReview:    true,
			IsMerged:     isMerged,
		})

		var commit string
		switch state {
		case lifecycle.ReviewPending:
			commit = review.UpstreamCommit
		case lifecycle.Approved, lifecycle.Merged, lifecycle.Built, lifecycle.Confirmed, lifecycle.Rejected, lifecycle.Published:
			head, err := provider.LocalHeadCommit(ctx, pkg)
			if err != nil {
				return "", true
			}
			commit = head
		default:
			return "", true
		}

		content, err := provider.FileAtCommit(ctx, pkg, commit, "PKGBUILD")
		if err != nil {
			return "", true
		}
		return ExtractFullVersion(content)
	}
}
