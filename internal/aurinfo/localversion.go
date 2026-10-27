package aurinfo

import (
	"context"
	"errors"
	"fmt"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// Why a local version is unknown. A resolver returns one of these, or the
// error from reading the review or the PKGBUILD, wrapped.
var (
	// ErrNoReview: the package has no review, so there is no reviewed
	// commit to read the version from.
	ErrNoReview = errors.New("no review record")
	// ErrNoUpstreamCommit: the review does not name the commit it covers.
	ErrNoUpstreamCommit = errors.New("review has no upstream commit")
	// ErrVersionNotReadable: the PKGBUILD was read, but its version cannot
	// be taken from it — a pkgver() function, a missing pkgver or pkgrel,
	// or a computed epoch, pkgver or pkgrel (ExtractFullVersion). It is a
	// property of the package, true on every check, not a failure of this
	// one (workflow.md § Update).
	ErrVersionNotReadable = errors.New("version cannot be read from the PKGBUILD (pkgver() function, or pkgver/pkgrel missing, or epoch/pkgver/pkgrel computed)")
)

// LocalVersionResolver resolves a package's local version from its
// PKGBUILD content. Implementations must read git-committed content —
// never a working-tree disk read — per workflow.md § Local Version —
// Git-Committed, Not Working Tree, since Build (internal/build/engine.go)
// clones and builds only from a resolved commit, never the working tree.
//
// A non-nil error means the version is unknown and says why; version is
// then "".
type LocalVersionResolver func(ctx context.Context, pkg domain.PackageName) (version string, err error)

// NewGitLocalVersionResolver returns a LocalVersionResolver that reads
// the PKGBUILD at the commit the package's current review covers
// (Review.UpstreamCommit), whatever the review's state — Pending,
// Approved, Rejected, or Approved and merged (workflow.md § Local
// Version, #426). Local master is never read: what is left to do with the
// reviewed commit is the lifecycle state's business, not the version
// comparison's.
//
// The version is unknown, with the reason as the error (#402), when the
// package has no review (ErrNoReview), the review cannot be read, the
// review has no UpstreamCommit (ErrNoUpstreamCommit), the PKGBUILD read
// fails, or the version cannot be taken from the PKGBUILD
// (ErrVersionNotReadable). The git read runs under its own
// git.GitTimeout, so a caller with no deadline of its own (update, #417)
// cannot hang on it.
func NewGitLocalVersionResolver(provider git.Provider, reviewDB domain.ReviewStore) LocalVersionResolver {
	return func(ctx context.Context, pkg domain.PackageName) (string, error) {
		review, err := reviewDB.Get(pkg)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			return "", ErrNoReview
		case err != nil:
			return "", fmt.Errorf("read review: %w", err)
		case review.UpstreamCommit == "":
			return "", ErrNoUpstreamCommit
		}

		gitCtx, cancel := git.GitTimeoutCtx(ctx)
		defer cancel()
		content, err := provider.FileAtCommit(gitCtx, pkg, review.UpstreamCommit, "PKGBUILD")
		if err != nil {
			return "", fmt.Errorf("read PKGBUILD at %s: %w", shortCommit(review.UpstreamCommit), err)
		}
		ver, unknown := ExtractFullVersion(content)
		if unknown {
			return "", ErrVersionNotReadable
		}
		return ver, nil
	}
}

// shortCommit abbreviates a commit hash for messages.
func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
