package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

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
// Only git's own "not an ancestor" answer (exit code 1) is (false, nil).
// Any other failure — the repository path cannot be resolved, the commit
// or master is missing, git times out — is (false, err): the merge state
// is unknown, not "not merged". Callers treat an error as not merged
// (fail-closed) and report it (#462).
func (p *provider) IsMerged(ctx context.Context, packageName domain.PackageName, reviewedCommit string) (bool, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return false, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	// `git merge-base --is-ancestor A B` exits 0 if A is an ancestor of
	// B (a commit is its own ancestor), 1 if it is not, and something
	// else on an error.
	_, err = p.runner.Run(ctx, repo, "merge-base", "--is-ancestor", reviewedCommit, "master")
	if err == nil {
		return true, nil
	}
	if isExitCode(err, 1) {
		return false, nil
	}
	return false, fmt.Errorf("merge check %q: %w", packageName, err)
}

// isExitCode reports whether err comes from a command that ran to the end
// and exited with code. A command killed by a timeout or a cancel reports
// exit code -1, so it never matches.
func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
