package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// Status returns the current Git status for a package repository.
func (p *provider) Status(ctx context.Context, packageName domain.PackageName) (*RepositoryStatus, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	out, err := p.runner.Run(ctx, repo, "status", "--porcelain", "--branch")
	if err != nil {
		return nil, fmt.Errorf("status %q: %w", packageName, err)
	}

	return parseStatus(out), nil
}

// CommitsBehind returns the number of commits local master is behind upstream/master.
func (p *provider) CommitsBehind(ctx context.Context, packageName domain.PackageName) (int, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return 0, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	out, err := p.runner.Run(ctx, repo,
		"rev-list", "--count", reviewRange)
	if err != nil {
		return 0, fmt.Errorf("commits behind %q: %w", packageName, err)
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse commits behind: %w", err)
	}
	return n, nil
}

// UpstreamCommit returns the commit hash the local upstream-tracking ref
// currently points at. This is a purely local read — no network call.
func (p *provider) UpstreamCommit(ctx context.Context, packageName domain.PackageName) (string, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return "", fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	out, err := p.runner.Run(ctx, repo, "rev-parse", "upstream/master")
	if err != nil {
		return "", fmt.Errorf("upstream commit %q: %w", packageName, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// Metadata returns repository metadata.
func (p *provider) Metadata(ctx context.Context, packageName domain.PackageName) (*RepositoryMetadata, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	branch, err := p.runner.Run(ctx, repo, "branch", "--show-current")
	if err != nil {
		return nil, err
	}
	origin, err := p.runner.Run(ctx, repo, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	upstream, err := p.runner.Run(ctx, repo, "remote", "get-url", "upstream")
	if err != nil {
		return nil, err
	}

	return &RepositoryMetadata{
		CurrentBranch: strings.TrimSpace(string(branch)),
		OriginURL:     strings.TrimSpace(string(origin)),
		UpstreamURL:   strings.TrimSpace(string(upstream)),
	}, nil
}

func parseStatus(out string) *RepositoryStatus {
	rs := &RepositoryStatus{Clean: true}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			if strings.Contains(l, "(HEAD detached") {
				rs.Detached = true
			}
			continue
		}
		if l == "" {
			continue
		}
		rs.Clean = false
		if strings.HasPrefix(l, "??") {
			rs.HasUntracked = true
		} else {
			rs.HasModified = true
		}
	}
	return rs
}

// FileAtCommit returns the content of a file at a specific commit in
// packageName's repository, via `git show <commit>:<path>`. Returns
// the raw file content as a string.
func (p *provider) FileAtCommit(ctx context.Context, packageName domain.PackageName, commit, path string) (string, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return "", fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	out, err := p.runner.Run(ctx, repo, "show", commit+":"+path)
	if err != nil {
		return "", fmt.Errorf("file at commit %q %q %q: %w", packageName, commit, path, err)
	}

	return string(out), nil
}
