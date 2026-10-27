package git

import (
	"context"
	"net/http"
	"time"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/domain"
)

// GitTimeout is the hard cap for git-provider subprocesses.
// Build operations (makepkg) are not subject to this timeout — they
// use explicit cancel only and should never be wrapped here.
const GitTimeout = 60 * time.Second

// ImportTimeout is the cap for the Import sequence — a full upstream
// clone plus HTTP round-trips and sequential git setup steps.  This
// is intentionally longer than GitTimeout to avoid false timeouts
// during normal operation; see #215.
const ImportTimeout = 2 * time.Minute

// GitTimeoutCtx derives a context with a GitTimeout deadline from parent.
// If parent is non-nil, it becomes the parent of the timeout context so
// that cancellation of parent propagates through (preserving X-key
// cancellation cascade). If parent is nil, falls back to
// context.Background() to avoid a nil-parent panic. The timeout is
// recorded (WithTimeout), so a git call it stops says "timed out after
// 1m0s" (#417).
func GitTimeoutCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return WithTimeout(parent, GitTimeout)
}

// ImportTimeoutCtx derives a context with an ImportTimeout deadline
// from parent.  Same derive-from-parent-or-fall-back-to-background
// shape as GitTimeoutCtx, parameterized on the longer duration.
func ImportTimeoutCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return WithTimeout(parent, ImportTimeout)
}

// httpDoer abstracts the HTTP client for testability.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// provider is the concrete Provider implementation.
type provider struct {
	catalog  catalog.PackageCatalog
	runner   Runner
	forge    Forge
	client   httpDoer
	rootPath string
}

func New(catalog catalog.PackageCatalog, runner Runner, forge Forge, rootPath string) Provider {
	return &provider{
		catalog:  catalog,
		runner:   runner,
		forge:    forge,
		client:   http.DefaultClient,
		rootPath: rootPath,
	}
}

func (p *provider) repoPath(ctx context.Context, packageName domain.PackageName) (string, error) {
	return p.catalog.RepositoryPath(packageName)
}
