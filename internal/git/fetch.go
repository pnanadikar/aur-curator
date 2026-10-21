package git

import (
	"context"
	"fmt"

	"github.com/pramod/aur-curator/internal/domain"
)

func (p *provider) Fetch(ctx context.Context, packageName domain.PackageName) error {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	_, err = p.runner.Run(
		ctx,
		repo,
		"fetch",
		"--prune",
		"upstream",
	)

	return err
}
