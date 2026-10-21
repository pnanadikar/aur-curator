package git

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func TestProviderFetchAndDiff(t *testing.T) {
	requireGit(t)

	workspace, pkgName, upstreamHead := createGitFixture(t)
	pkgDir := filepath.Join(workspace, pkgName)

	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: pkgDir},
		client:  http.DefaultClient,
	}

	ctx := context.Background()
	if err := p.Fetch(ctx, domain.PackageName(pkgName)); err != nil {
		t.Fatal(err)
	}

	diff, err := p.Diff(ctx, domain.PackageName(pkgName))
	if err != nil {
		t.Fatal(err)
	}

	if diff.UpstreamCommit != upstreamHead {
		t.Fatalf("expected UpstreamCommit %q, got %q", upstreamHead, diff.UpstreamCommit)
	}
	if diff.LocalCommit == "" {
		t.Fatal("expected a non-empty LocalCommit — this fixture starts with real content, not the Import case")
	}
	if len(diff.Commits) != 1 {
		t.Fatalf("expected 1 new upstream commit, got %d", len(diff.Commits))
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "PKGBUILD" {
		t.Fatalf("expected 1 changed file (PKGBUILD), got %#v", diff.Files)
	}
	if diff.Files[0].Kind != FileKindPKGBUILD {
		t.Fatalf("expected FileKindPKGBUILD, got %v", diff.Files[0].Kind)
	}
}

func TestProviderDiffEmptyWhenBranchesMatch(t *testing.T) {
	requireGit(t)

	workspace, pkgName, _ := createGitFixture(t)
	pkgDir := filepath.Join(workspace, pkgName)

	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: pkgDir},
		client:  http.DefaultClient,
	}

	ctx := context.Background()
	if err := p.Fetch(ctx, domain.PackageName(pkgName)); err != nil {
		t.Fatal(err)
	}
	// Simulate a completed Merge directly via git, not through p.Merge —
	// that method is out of scope for this TODO. This brings local
	// master and upstream/master to the same commit, which is the
	// condition this test's name describes.
	runGit(t, pkgDir, "merge", "--ff-only", "upstream/master")

	diff, err := p.Diff(ctx, domain.PackageName(pkgName))
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Commits) != 0 {
		t.Fatalf("expected no commits when branches match, got %d", len(diff.Commits))
	}
	if len(diff.Files) != 0 {
		t.Fatalf("expected no file changes when branches match, got %d", len(diff.Files))
	}
}

func createGitFixture(t *testing.T) (workspace string, pkgName string, upstreamHead string) {
	t.Helper()

	root := t.TempDir()
	workspace = filepath.Join(root, "workspace")
	upstreamBare := filepath.Join(root, "upstream.git")
	originBare := filepath.Join(root, "origin.git")
	upstreamSeed := filepath.Join(root, "upstream-seed")
	pkgName = "yay"

	mustMkdir(t, workspace)
	runGit(t, root, "init", "--bare", upstreamBare)

	mustMkdir(t, upstreamSeed)
	runGit(t, upstreamSeed, "init")
	runGit(t, upstreamSeed, "checkout", "-b", "master")
	writeFile(t, filepath.Join(upstreamSeed, "PKGBUILD"), "pkgver=1.0\n")
	runGit(t, upstreamSeed, "add", "PKGBUILD")
	runGit(t, upstreamSeed, "commit", "-m", "initial upstream")
	runGit(t, upstreamSeed, "remote", "add", "origin", upstreamBare)
	runGit(t, upstreamSeed, "push", "-u", "origin", "master")

	runGit(t, root, "clone", "--bare", upstreamBare, originBare)
	runGit(t, workspace, "clone", originBare, pkgName)
	pkgDir := filepath.Join(workspace, pkgName)
	runGit(t, pkgDir, "checkout", "-B", "master", "origin/master")
	runGit(t, pkgDir, "remote", "add", "upstream", upstreamBare)

	writeFile(t, filepath.Join(upstreamSeed, "PKGBUILD"), "pkgver=1.1\n")
	runGit(t, upstreamSeed, "add", "PKGBUILD")
	runGit(t, upstreamSeed, "commit", "-m", "upstream update")
	runGit(t, upstreamSeed, "push", "origin", "master")

	upstreamHead = strings.TrimSpace(runGit(t, upstreamSeed, "rev-parse", "HEAD"))
	return workspace, pkgName, upstreamHead
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=aur-curator",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=aur-curator",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
