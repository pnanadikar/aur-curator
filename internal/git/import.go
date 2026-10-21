package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/trace"
)

// importStep is one git subcommand run during Import. Grouping name,
// dir, args, and failure sentinel together (rather than parallel slices
// indexed by position) removes the risk of the two drifting out of sync.
type importStep struct {
	name string
	dir  string
	args []string
	err  error
}

// Import creates and initializes a managed repository for an AUR package.
func (p *provider) Import(ctx context.Context, packageName domain.PackageName) error {
	// Wrap all operations with a timeout to prevent indefinite hangs
	// on network issues (#215).  Import covers a heavier sequence
	// (full upstream clone, HTTP round-trips, sequential git setup),
	// so it uses ImportTimeout (2m) rather than the shared GitTimeout
	// (60s) to avoid false timeouts during normal operation.
	importStart := time.Now()
	ctx, cancel := ImportTimeoutCtx(ctx)
	defer cancel()

	ctx = trace.Start(ctx, "import")

	var completed []string

	if err := p.checkAURPackageExists(ctx, packageName); err != nil {
		if isDeadlineExceeded(err) {
			_, _ = fmt.Fprintf(os.Stdout, "verify %q exists on AUR ... timed out\n", packageName)
			return importTimedOut(string(packageName), completed, time.Since(importStart))
		}
		p.logFailure(ctx, packageName, completed, err)
		_, _ = fmt.Fprintf(os.Stdout, "verify %q exists on AUR ... failed\n", packageName)
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "verify %q exists on AUR ... done\n", packageName)
	trace.Log(ctx, "handoff", "to", "forge", "from", "aur", "via", "checkAURPackageExists", "pkg", packageName)

	cloneURL, err := p.forge.CreateRepository(ctx, string(packageName))
	if err != nil {
		if isDeadlineExceeded(err) {
			_, _ = fmt.Fprintf(os.Stdout, "create Gitea repository ... timed out\n")
			return importTimedOut(string(packageName), completed, time.Since(importStart))
		}
		p.logFailure(ctx, packageName, completed, err)
		_, _ = fmt.Fprintf(os.Stdout, "create Gitea repository ... failed\n")
		return importFailure(string(packageName), ErrCreateRepository, err, completed)
	}
	_, _ = fmt.Fprintf(os.Stdout, "create Gitea repository ... done\n")
	trace.Log(ctx, "handoff", "to", "clone", "from", "forge", "via", "CreateRepository", "pkg", packageName)
	completed = append(completed, "create Gitea repository")

	repo, err := p.repoPath(ctx, packageName)
	if err != nil {
		if isDeadlineExceeded(err) {
			_, _ = fmt.Fprintf(os.Stdout, "resolve repository path ... timed out\n")
			return importTimedOut(string(packageName), completed, time.Since(importStart))
		}
		p.logFailure(ctx, packageName, completed, err)
		_, _ = fmt.Fprintf(os.Stdout, "resolve repository path ... failed\n")
		return importFailure(string(packageName), ErrResolveRepository, err, completed)
	}
	_, _ = fmt.Fprintf(os.Stdout, "resolve repository path ... done\n")
	trace.Log(ctx, "handoff", "to", "clone", "from", "catalog", "via", "RepositoryPath", "pkg", packageName, "path", repo)
	completed = append(completed, "resolve repository path")

	preSteps := []importStep{
		{
			name: "clone AUR repository",
			dir:  filepath.Dir(repo),
			args: []string{"clone", fmt.Sprintf("https://aur.archlinux.org/%s.git", packageName), repo},
			err:  ErrCloneRepository,
		},
		{
			name: "rename origin to upstream",
			dir:  repo,
			args: []string{"remote", "rename", "origin", "upstream"},
			err:  ErrConfigureRemotes,
		},
		{
			name: "add origin remote",
			dir:  repo,
			args: []string{"remote", "add", "origin", cloneURL},
			err:  ErrConfigureRemotes,
		},
	}

	for i, s := range preSteps {
		if _, err := p.runner.Run(ctx, s.dir, s.args...); err != nil {
			if isDeadlineExceeded(err) {
				_, _ = fmt.Fprintf(os.Stdout, "%s ... timed out\n", s.name)
				return importTimedOut(string(packageName), completed, time.Since(importStart))
			}
			p.logFailure(ctx, packageName, completed, err)
			_, _ = fmt.Fprintf(os.Stdout, "%s ... failed\n", s.name)
			return importFailure(string(packageName), s.err, err, completed)
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s ... done\n", s.name)
		completed = append(completed, s.name)
		if i < len(preSteps)-1 {
			trace.Log(ctx, "handoff", "to", preSteps[i+1].name, "from", s.name, "via", "git", "pkg", packageName)
		}
	}

	// AUR repositories conventionally check out onto "master"
	// (roadmap.md), which collides with the orphan "master" this
	// import creates next. Detect the actual checked-out branch rather
	// than assuming "master" — not every upstream is guaranteed to
	// follow that convention — then detach from it and delete it
	// before creating the orphan branch.
	branchOut, err := p.runner.Run(ctx, repo, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		if isDeadlineExceeded(err) {
			_, _ = fmt.Fprintf(os.Stdout, "detect cloned branch ... timed out\n")
			return importTimedOut(string(packageName), completed, time.Since(importStart))
		}
		_, _ = fmt.Fprintf(os.Stdout, "detect cloned branch ... failed\n")
		return importFailure(string(packageName), ErrCreateMaster, err, completed)
	}
	_, _ = fmt.Fprintf(os.Stdout, "detect cloned branch ... done\n")
	clonedBranch := strings.TrimSpace(branchOut)

	postSteps := []importStep{
		{
			name: "detach HEAD from cloned branch",
			dir:  repo,
			args: []string{"checkout", "--detach"},
			err:  ErrCreateMaster,
		},
		{
			name: "delete cloned branch",
			dir:  repo,
			args: []string{"branch", "-D", clonedBranch},
			err:  ErrCreateMaster,
		},
		{
			name: "switch to orphan master",
			dir:  repo,
			args: []string{"switch", "--orphan", "master"},
			err:  ErrCreateMaster,
		},
		{
			name: "clear working tree",
			dir:  repo,
			args: []string{"rm", "-rf", "--ignore-unmatch", "."},
			err:  ErrCreateMaster,
		},
		{
			name: "remove untracked files",
			dir:  repo,
			args: []string{"clean", "-fdx"},
			err:  ErrCreateMaster,
		},
		{
			name: "commit empty master",
			dir:  repo,
			args: []string{"commit", "--allow-empty", "-m", "Initial managed branch"},
			err:  ErrCreateMaster,
		},
	}

	for i, s := range postSteps {
		if _, err := p.runner.Run(ctx, s.dir, s.args...); err != nil {
			if isDeadlineExceeded(err) {
				_, _ = fmt.Fprintf(os.Stdout, "%s ... timed out\n", s.name)
				return importTimedOut(string(packageName), completed, time.Since(importStart))
			}
			p.logFailure(ctx, packageName, completed, err)
			_, _ = fmt.Fprintf(os.Stdout, "%s ... failed\n", s.name)
			return importFailure(string(packageName), s.err, err, completed)
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s ... done\n", s.name)
		completed = append(completed, s.name)
		if i < len(postSteps)-1 {
			trace.Log(ctx, "handoff", "to", postSteps[i+1].name, "from", s.name, "via", "git", "pkg", packageName)
		}
	}

	trace.Log(ctx, "handoff", "to", "complete", "from", "commit", "via", "Import", "pkg", packageName)
	return nil
}

// isDeadlineExceeded reports whether err is a context deadline exceeded error.
func isDeadlineExceeded(err error) bool {
	return err != nil && errors.Is(err, context.DeadlineExceeded)
}

// importTimedOut returns a timeout-distinguishing error that clearly
// identifies the failure as a deadline exceeded rather than a generic
// git error. The completed-steps line is included so the user knows
// which steps ran before the timeout.
func importTimedOut(packageName string, completed []string, elapsed time.Duration) error {
	return fmt.Errorf(
		"import %q: timed out after %v (completed: %s)",
		packageName, elapsed.Round(time.Millisecond), strings.Join(completed, ", "),
	)
}

// importFailure builds a consistent Import failure message: the failing
// package, the sentinel and underlying cause (both wrapped so
// errors.Is still works against either), which steps completed before
// the failure, and manual-cleanup guidance whenever at least one step
// has completed — i.e. whenever a Gitea repository may already exist.
// When nothing has completed yet (the Forge call itself failed),
// neither the completed-steps line nor the cleanup guidance is
// included, since there's nothing to clean up.
func importFailure(packageName string, sentinel, cause error, completed []string) error {
	if len(completed) == 0 {
		return fmt.Errorf("import %q: %w", packageName, cause)
	}
	return fmt.Errorf(
		"import %q: %w: %w\n\nCompleted steps: %s.\n\nThe managed "+
			"repository has already been created. Delete the remote "+
			"repository manually before retrying",
		packageName,
		sentinel,
		cause,
		strings.Join(completed, ", "),
	)
}

// logFailure appends a human-readable error entry to the workspace error log
// whenever an Import step fails. It logs the package name, which step
// completed before failure, the failed step, and whether manual cleanup
// is needed — matching the information already in the wrapped error.
func (p *provider) logFailure(ctx context.Context, packageName domain.PackageName, completed []string, cause error) {
	msg := fmt.Sprintf("import %q failed: %v", packageName, cause)
	if len(completed) > 0 {
		msg += fmt.Sprintf("; completed before failure: %s", strings.Join(completed, ", "))
	}
	_ = errorlog.Log(ctx, p.rootPath, "import", msg)
}

// aurPackageResponse is the AUR RPC v5 "info" response shape for a single
// package name lookup (the names query parameter).
type aurPackageResponse struct {
	ResultCount int    `json:"resultcount"`
	Type        string `json:"type"`
}

// checkAURPackageExists queries the AUR RPC API to confirm that the given
// package name corresponds to an actual package on the AUR.  It returns
// ErrPackageNotFound when the package is not present and a distinct wrapped
// error when the RPC call itself fails (network, malformed response, etc.).
func (p *provider) checkAURPackageExists(ctx context.Context, packageName domain.PackageName) error {
	url := fmt.Sprintf("https://aur.archlinux.org/rpc/?v=5&type=info&arg=%s", packageName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("import %q: could not verify package exists on AUR: %w", packageName, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("import %q: could not verify package exists on AUR: %w", packageName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("import %q: could not verify package exists on AUR: %w", packageName, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("import %q: could not verify package exists on AUR: HTTP %d", packageName, resp.StatusCode)
	}

	var result aurPackageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("import %q: could not verify package exists on AUR: %w", packageName, err)
	}
	if result.ResultCount == 0 {
		return fmt.Errorf("import %q: package not found on AUR — check the package name and try again: %w", packageName, ErrPackageNotFound)
	}
	return nil
}
