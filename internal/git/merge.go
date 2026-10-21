package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/trace"
)

// NotFastForwardError reports that local master has commits that are not
// in the reviewed commit, so Merge cannot fast-forward to it. Resolving
// this is the user's job, in git, outside the application
// (workflow.md § Merge, § Local Commits Not on Upstream).
type NotFastForwardError struct {
	Package      domain.PackageName
	Commit       string // reviewed commit, full SHA
	LocalCommits int    // commits on master that are not in Commit
}

func (e *NotFastForwardError) Error() string {
	short := e.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fmt.Sprintf("merge %s: master has %d local commit(s) not in reviewed commit %s; fast-forward impossible — resolve in git (rebase or reset) and retry",
		e.Package, e.LocalCommits, short)
}

// Merge moves local master to commit — the reviewed upstream commit
// (Review.UpstreamCommit) — and pushes the result to origin. It never
// merges the moving upstream/master ref, so only a reviewed commit can
// reach the trusted history (workflow.md § Merge).
//
// For a freshly-imported package (master absent, or Import's empty-tree
// placeholder — see isEmptyMaster), master is reset to commit and pushed
// with upstream tracking. Otherwise master is fast-forwarded to commit.
// If master has commits of its own, the fast-forward is impossible and
// Merge returns *NotFastForwardError without changing anything. If
// master already contains commit, nothing is merged; the push is
// retried only when master is exactly at commit, so Merge never pushes
// local commits that are not part of the review.
func (p *provider) Merge(ctx context.Context, packageName domain.PackageName, commit string) error {
	if commit == "" {
		return fmt.Errorf("merge %q: no reviewed commit given", packageName)
	}

	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	local, err := p.localMasterStrict(ctx, repo)
	if err != nil {
		return fmt.Errorf("resolve local master %q: %w", packageName, err)
	}

	out, err := p.runner.Run(ctx, repo, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return fmt.Errorf("merge %q: reviewed commit %s not found: %w", packageName, commit, err)
	}
	target := strings.TrimSpace(out)
	if target == "" {
		return fmt.Errorf("merge %q: reviewed commit %s not found", packageName, commit)
	}

	firstMerge, err := p.isEmptyMaster(ctx, repo, local)
	if err != nil {
		return fmt.Errorf("check master state %q: %w", packageName, err)
	}

	if firstMerge {
		trace.Log(ctx, "handoff", "to", "subprocess", "from", "merge", "cmd", "reset", "pkg", packageName)
		if _, err := p.runner.Run(ctx, repo, "reset", "--hard", target); err != nil {
			return fmt.Errorf("create master %q: %w", packageName, err)
		}
		trace.Log(ctx, "handoff", "to", "subprocess", "from", "merge", "cmd", "push", "pkg", packageName)
		if _, err := p.runner.Run(ctx, repo, "push", "-u", "origin", "master"); err != nil {
			return fmt.Errorf("local master updated for %q, but push to origin failed: %w", packageName, err)
		}
		return nil
	}

	mbOut, err := p.runner.Run(ctx, repo, "merge-base", "master", target)
	if err != nil {
		return fmt.Errorf("merge %q: find merge base with %s: %w", packageName, target, err)
	}
	mergeBase := strings.TrimSpace(mbOut)

	switch {
	case mergeBase == target && local != target:
		// The reviewed commit is already in master, and master has
		// commits of its own on top. Nothing to merge; pushing would
		// publish those local commits as part of this review, so don't.
		trace.Log(ctx, "result", "op", "merge", "outcome", "already-contained", "pkg", packageName)
		return nil
	case mergeBase == target:
		// master is exactly at the reviewed commit — an earlier merge
		// succeeded locally. Retry the push below.
	case mergeBase == local:
		trace.Log(ctx, "handoff", "to", "subprocess", "from", "merge", "cmd", "merge", "pkg", packageName)
		if _, err := p.runner.Run(ctx, repo, "merge", "--ff-only", target); err != nil {
			return fmt.Errorf("merge %q: %w", packageName, err)
		}
	default:
		countOut, err := p.runner.Run(ctx, repo, "rev-list", "--count", target+"..master")
		if err != nil {
			return fmt.Errorf("merge %q: count local commits: %w", packageName, err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(countOut))
		if err != nil {
			return fmt.Errorf("merge %q: count local commits: unexpected output %q: %w", packageName, countOut, err)
		}
		return &NotFastForwardError{Package: packageName, Commit: target, LocalCommits: n}
	}

	trace.Log(ctx, "handoff", "to", "subprocess", "from", "merge", "cmd", "push", "pkg", packageName)
	if _, err := p.runner.Run(ctx, repo, "push", "origin", "master"); err != nil {
		return fmt.Errorf("local master updated for %q, but push to origin failed: %w", packageName, err)
	}
	return nil
}

// localMasterStrict returns master's commit, "" if the master ref does
// not exist, or an error if git itself failed — never reporting a git
// failure as "no master" (#323). Merge relies on that distinction before
// it will run reset --hard; Diff relies on it so a failure is never shown
// as a new package's review.
func (p *provider) localMasterStrict(ctx context.Context, repo string) (string, error) {
	out, err := p.runner.Run(ctx, repo, "for-each-ref", "--format=%(objectname)", "refs/heads/master")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
