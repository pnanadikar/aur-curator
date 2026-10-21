package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// LocalOnlyCommits returns the number of commits on local master that
// are not on upstream/master (workflow.md § Local Commits Not on
// Upstream). It returns 0 when master is absent or is Import's
// empty-tree placeholder, which is never a local commit. A failed check
// is an error, never 0: callers must not show "no local commits" for a
// check that did not run.
func (p *provider) LocalOnlyCommits(ctx context.Context, packageName domain.PackageName) (int, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return 0, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	local, err := p.localMasterStrict(ctx, repo)
	if err != nil {
		return 0, fmt.Errorf("resolve local master %q: %w", packageName, err)
	}
	if local == "" {
		return 0, nil
	}

	empty, err := p.isEmptyMaster(ctx, repo, local)
	if err != nil {
		return 0, fmt.Errorf("check master state %q: %w", packageName, err)
	}
	if empty {
		return 0, nil
	}

	out, err := p.runner.Run(ctx, repo, "rev-list", "--count", "upstream/master..master")
	if err != nil {
		return 0, fmt.Errorf("count local commits %q: %w", packageName, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count local commits %q: unexpected output %q: %w", packageName, out, err)
	}
	return n, nil
}
