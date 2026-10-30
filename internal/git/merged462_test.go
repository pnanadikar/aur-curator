package git

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// #462 — IsMerged tells "not merged" (git's exit code 1) apart from "could
// not check" (any other failure). It used to return (false, nil) for both,
// so no caller could report a failed merge check.

// TestIsMerged uses real git. The fixture's master holds the initial
// upstream commit; after the fetch, upstream/master holds one more.
func TestIsMerged(t *testing.T) {
	requireGit(t)

	workspace, pkgName, upstreamHead := createGitFixture(t)
	pkgDir := filepath.Join(workspace, pkgName)
	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: pkgDir},
		client:  http.DefaultClient,
	}
	ctx := context.Background()
	pkg := domain.PackageName(pkgName)
	if err := p.Fetch(ctx, pkg); err != nil {
		t.Fatal(err)
	}
	masterHead := strings.TrimSpace(runGit(t, pkgDir, "rev-parse", "master"))

	tests := []struct {
		name       string
		commit     string
		wantMerged bool
		wantErr    bool
	}{
		{name: "master head is merged", commit: masterHead, wantMerged: true},
		{name: "upstream commit not in master is not merged", commit: upstreamHead},
		{name: "unknown commit is an error", commit: "0000000000000000000000000000000000000001", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, err := p.IsMerged(ctx, pkg, tt.commit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsMerged err = %v, want error: %v", err, tt.wantErr)
			}
			if merged != tt.wantMerged {
				t.Fatalf("IsMerged = %v, want %v", merged, tt.wantMerged)
			}
		})
	}
}

// TestIsMergedNoMaster: a clone without a master branch (a partial
// import) cannot be checked; that is an error, not "not merged".
func TestIsMergedNoMaster(t *testing.T) {
	requireGit(t)

	workspace, pkgName, upstreamHead := createGitFixture(t)
	pkgDir := filepath.Join(workspace, pkgName)
	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: pkgDir},
		client:  http.DefaultClient,
	}
	ctx := context.Background()
	pkg := domain.PackageName(pkgName)
	if err := p.Fetch(ctx, pkg); err != nil {
		t.Fatal(err)
	}
	runGit(t, pkgDir, "checkout", "--detach")
	runGit(t, pkgDir, "branch", "-D", "master")

	merged, err := p.IsMerged(ctx, pkg, upstreamHead)
	if err == nil {
		t.Fatal("expected an error when master does not exist")
	}
	if merged {
		t.Fatal("a failed check must report not merged")
	}
}

// TestIsMergedRepoPathError: a package whose repository cannot be resolved
// is an error, wrapping the catalog's.
func TestIsMergedRepoPathError(t *testing.T) {
	catalogErr := errors.New("not in catalog")
	p := &provider{
		runner:  &fakeRunner{},
		catalog: &fakeCatalog{err: catalogErr},
	}

	merged, err := p.IsMerged(context.Background(), "yay", "abc123")
	if !errors.Is(err, catalogErr) {
		t.Fatalf("IsMerged err = %v, want it to wrap %v", err, catalogErr)
	}
	if merged {
		t.Fatal("a failed check must report not merged")
	}
}

// TestIsMergedInterrupted: a cancelled check is an error. The killed git
// process reports exit code -1, never 1, so it cannot read as "not merged".
func TestIsMergedInterrupted(t *testing.T) {
	requireGit(t)

	workspace, pkgName, upstreamHead := createGitFixture(t)
	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: filepath.Join(workspace, pkgName)},
		client:  http.DefaultClient,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	merged, err := p.IsMerged(ctx, domain.PackageName(pkgName), upstreamHead)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("IsMerged err = %v, want context.Canceled", err)
	}
	if merged {
		t.Fatal("a failed check must report not merged")
	}
}
