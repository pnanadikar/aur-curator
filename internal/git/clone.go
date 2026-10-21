package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// CloneAt creates a fresh, fully independent local clone of pkgName's
// repository at commit, into destDir. destDir must not already exist.
func (p *provider) CloneAt(ctx context.Context, pkgName domain.PackageName, commit string, destDir string) error {
	repo, err := p.repoPath(ctx, pkgName)
	if err != nil {
		return fmt.Errorf("resolve repository %q: %w", pkgName, err)
	}

	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("cloneAt: destDir %q already exists", destDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destDir %q: %w", destDir, err)
	}

	if _, err := p.runner.Run(ctx, repo, "clone", "--local", "--no-hardlinks", repo, destDir); err != nil {
		return fmt.Errorf("git clone --local %s %s: %w", repo, destDir, err)
	}

	if _, err := p.runner.Run(ctx, destDir, "checkout", "--detach", commit); err != nil {
		return fmt.Errorf("git checkout --detach %s: %w", commit, err)
	}

	return nil
}

// LocalHeadCommit returns the commit hash local master currently
// points at.
func (p *provider) LocalHeadCommit(ctx context.Context, pkgName domain.PackageName) (string, error) {
	repo, err := p.repoPath(ctx, pkgName)
	if err != nil {
		return "", fmt.Errorf("resolve repository %q: %w", pkgName, err)
	}

	commit, err := p.runner.Run(ctx, repo, "rev-parse", "master")
	if err != nil {
		return "", fmt.Errorf("git rev-parse master: %w", err)
	}

	return strings.TrimSpace(commit), nil
}
