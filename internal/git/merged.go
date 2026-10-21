package git

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
)

// IsMerged reports whether the reviewed commit (reviewedCommit) has
// been incorporated into local master using `git merge-base
// --is-ancestor`. Returns true if the commit is an ancestor of (or
// equal to) master HEAD.
//
// This answers "was this specific reviewed change merged" — invariant
// to upstream advancing after the merge, unlike CommitsBehind which
// only answers "how far behind is master from upstream's current tip."
//
// On any failure (missing commit, repository errors, no master
// branch, or inability to resolve the repository path) returns
// (false, nil) — fail-closed. This is a deliberate deviation from
// the wrapped-error convention used at all other provider call
// sites; IsMerged treats any uncertainty as "not merged."
func (p *provider) IsMerged(ctx context.Context, packageName domain.PackageName, reviewedCommit string) (bool, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return false, nil // fail-closed: can't resolve repo → not confirmed merged
	}

	// Check if reviewedCommit is an ancestor of (or equal to) master.
	// `git merge-base --is-ancestor A B` returns 0 if A is an ancestor of B.
	// This handles the equality case too (a commit is its own ancestor).
	_, err = p.runner.Run(ctx, repo, "merge-base", "--is-ancestor", reviewedCommit, "master")
	if err != nil {
		return false, nil // fail-closed: git error → not confirmed merged
	}

	return true, nil
}
