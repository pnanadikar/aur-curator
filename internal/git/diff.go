package git

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// reviewRange is the comparison this application always makes: local
// master against upstream/master. Never HEAD — HEAD may not be master
// (mid-operation, detached, etc.), and the domain model
// (workflow.md, tui-review.md) is explicit that review always compares
// against master by name, never "whatever's currently checked out".
// Two dots: used for listing and counting upstream's new commits.
const reviewRange = "master..upstream/master"

// reviewDiffRange is the file diff Review shows: upstream's changes since
// the merge base of master and upstream/master (three dots). A tree-to-tree
// diff (two dots) also shows commits on master that are not on upstream, as
// if upstream would remove them (#322). Three dots need a merge base, so
// Import's placeholder master (empty tree, no shared history) is diffed
// against the empty tree instead.
const reviewDiffRange = "master...upstream/master"

// emptyTreeHash is Git's well-known empty tree object, present in every
// repository. Diffing against it produces a fully-additive diff — used
// for the Import case (workflow.md § Import) where no local master
// exists yet, so every upstream file is new by definition.
const emptyTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

var diffHeaderRe = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

// Diff returns the live comparison between local master and
// upstream/master for packageName. If the package has no local master yet
// (its first review — see workflow.md § Import), GitDiff.LocalCommit is
// empty, GitDiff.Commits is the full upstream/master history, and every
// file in GitDiff.Files is a full addition.
//
// GitDiff.BaseCommit is the merge base of the two commits read, or empty
// when master is missing or Import's placeholder. A new Review stores it as
// the start of its reviewed range (domain.md § Reviewed Range, #387). The
// Review screen and the analyzers never use Diff: they use ReviewDiff on
// the stored range, which stays meaningful after Merge moves master.
func (p *provider) Diff(ctx context.Context, packageName domain.PackageName) (*GitDiff, error) {
	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	localCommit, err := p.localMasterStrict(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve local master %q: %w", packageName, err)
	}

	upstreamCommit, err := p.runner.Run(ctx, repo, "rev-parse", "upstream/master")
	if err != nil {
		return nil, fmt.Errorf("resolve upstream/master %q: %w", packageName, err)
	}

	commits, err := p.commitsInRange(ctx, repo, localCommit)
	if err != nil {
		return nil, fmt.Errorf("commits %q: %w", packageName, err)
	}

	empty, err := p.isEmptyMaster(ctx, repo, localCommit)
	if err != nil {
		return nil, fmt.Errorf("files %q: %w", packageName, err)
	}

	files, err := p.filesInRange(ctx, repo, empty)
	if err != nil {
		return nil, fmt.Errorf("files %q: %w", packageName, err)
	}

	upstream := strings.TrimSpace(upstreamCommit)
	var base string
	if !empty {
		base, err = p.mergeBase(ctx, repo, localCommit, upstream)
		if err != nil {
			return nil, fmt.Errorf("merge base %q: %w", packageName, err)
		}
	}

	return &GitDiff{
		LocalCommit:    localCommit,
		BaseCommit:     base,
		UpstreamCommit: upstream,
		Commits:        commits,
		Files:          files,
	}, nil
}

// mergeBase returns the merge base of two commits. Output that names no
// commit is an error: an empty base would read as a first review.
func (p *provider) mergeBase(ctx context.Context, repo, a, b string) (string, error) {
	out, err := p.runner.Run(ctx, repo, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(out)
	if base == "" {
		return "", fmt.Errorf("no merge base for %s and %s", a, b)
	}
	return base, nil
}

// ReviewDiff returns the diff of a stored review range: upstream's commits
// and file changes from base to upstream (domain.md § Reviewed Range,
// #387). It reads no refs, so the result does not depend on where master
// or upstream/master are now: a review shows the same changes before and
// after Merge. An empty base is a first review (Import): the full history
// of upstream, every file diffed against the empty tree.
//
// Both commits are verified first. A commit missing from the repository is
// an error naming it; ReviewDiff never falls back to the live comparison,
// which would show something other than what was reviewed. The returned
// GitDiff has LocalCommit empty; BaseCommit and UpstreamCommit are the
// verified full hashes.
func (p *provider) ReviewDiff(ctx context.Context, packageName domain.PackageName, base, upstream string) (*GitDiff, error) {
	if upstream == "" {
		return nil, fmt.Errorf("review %q: no reviewed upstream commit", packageName)
	}

	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", packageName, err)
	}

	upFull, err := p.verifyCommit(ctx, repo, upstream)
	if err != nil {
		return nil, fmt.Errorf("review %q: reviewed upstream commit %s not found in the repository: %w", packageName, upstream, err)
	}

	var baseFull string
	if base != "" {
		baseFull, err = p.verifyCommit(ctx, repo, base)
		if err != nil {
			return nil, fmt.Errorf("review %q: base commit %s of the reviewed range not found in the repository: %w", packageName, base, err)
		}
	}

	logRange, diffFrom := upFull, emptyTreeHash
	if baseFull != "" {
		logRange, diffFrom = baseFull+".."+upFull, baseFull
	}

	commits, err := p.logCommits(ctx, repo, logRange)
	if err != nil {
		return nil, fmt.Errorf("commits %q: %w", packageName, err)
	}

	patch, err := p.runner.Run(ctx, repo, "diff", diffFrom, upFull)
	if err != nil {
		return nil, fmt.Errorf("files %q: %w", packageName, err)
	}

	return &GitDiff{
		BaseCommit:     baseFull,
		UpstreamCommit: upFull,
		Commits:        commits,
		Files:          parseFileDiffs(patch),
	}, nil
}

// verifyCommit resolves rev to a full commit hash, or fails if the
// repository has no such commit.
func (p *provider) verifyCommit(ctx context.Context, repo, rev string) (string, error) {
	out, err := p.runner.Run(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	full := strings.TrimSpace(out)
	if full == "" {
		return "", fmt.Errorf("no such commit")
	}
	return full, nil
}

// isEmptyMaster reports whether localCommit points to a tree with no
// content — i.e. Import's placeholder orphan commit (see NOTE in
// import.go), not yet superseded by a real merge. localCommit == ""
// (no master ref at all) is treated the same way: nothing reviewed has
// been merged yet, regardless of whether that's because Import hasn't
// run or because Import's orphan ref is somehow missing.
func (p *provider) isEmptyMaster(ctx context.Context, repo, localCommit string) (bool, error) {
	if localCommit == "" {
		return true, nil
	}
	tree, err := p.runner.Run(ctx, repo, "rev-parse", localCommit+"^{tree}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(tree) == emptyTreeHash, nil
}

func (p *provider) commitsInRange(ctx context.Context, repo, localCommit string) ([]Commit, error) {
	rangeArg := reviewRange
	if localCommit == "" {
		rangeArg = "upstream/master"
	}
	return p.logCommits(ctx, repo, rangeArg)
}

// logCommits lists the commits of a git log range, newest first.
func (p *provider) logCommits(ctx context.Context, repo, rangeArg string) ([]Commit, error) {
	format := "%H%x1f%an%x1f%ad%x1f%s"
	out, err := p.runner.Run(ctx, repo,
		"log", "--date=iso-strict", fmt.Sprintf("--pretty=format:%s", format), rangeArg)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// filesInRange diffs upstream/master against the empty tree when master
// is empty (Import's placeholder or absent), otherwise against its merge
// base with master (three dots, #322).
func (p *provider) filesInRange(ctx context.Context, repo string, empty bool) ([]FileDiff, error) {
	var patch string
	var err error

	if empty {
		patch, err = p.runner.Run(ctx, repo, "diff", emptyTreeHash, "upstream/master")
	} else {
		patch, err = p.runner.Run(ctx, repo, "diff", reviewDiffRange)
	}
	if err != nil {
		return nil, err
	}
	return parseFileDiffs(patch), nil
}

func parseCommits(out string) []Commit {
	text := strings.TrimSpace(out)
	if text == "" {
		return nil
	}

	var commits []Commit
	for _, line := range strings.Split(text, "\n") {
		fields := strings.SplitN(line, "\x1f", 4)
		if len(fields) != 4 {
			continue
		}
		commits = append(commits, Commit{
			Hash:    fields[0],
			Author:  fields[1],
			Date:    fields[2],
			Subject: fields[3],
		})
	}
	return commits
}

// parseFileDiffs splits a unified diff produced by `git diff` into one
// FileDiff per changed file. Callers never receive a concatenated
// multi-file patch — the Diff tab (tui-review.md) is built by filtering
// the returned slice for Kind == FileKindPKGBUILD or FileKindOther and
// rendering each FileDiff independently.
func parseFileDiffs(patch string) []FileDiff {
	if strings.TrimSpace(patch) == "" {
		return nil
	}

	var files []FileDiff
	var current *FileDiff
	var body []string

	flush := func() {
		if current != nil {
			current.Patch = strings.Join(body, "\n")
			files = append(files, *current)
		}
	}

	for _, line := range strings.Split(patch, "\n") {
		if m := diffHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			path := m[2]
			current = &FileDiff{Path: path, Kind: classifyFileKind(path)}
			body = []string{line}
			continue
		}
		if current != nil {
			body = append(body, line)
		}
	}
	flush()

	return files
}

func classifyFileKind(path string) FileKind {
	base := filepath.Base(path)
	switch {
	case base == "PKGBUILD":
		return FileKindPKGBUILD
	case base == ".SRCINFO":
		return FileKindSRCINFO
	case strings.HasSuffix(base, ".install"):
		return FileKindInstall
	case strings.HasSuffix(base, ".patch"), strings.HasSuffix(base, ".diff"):
		return FileKindPatch
	default:
		return FileKindOther
	}
}
