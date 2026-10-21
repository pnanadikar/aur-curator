package git

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// #322 — Review's file diff is master...upstream/master (three dots): only
// upstream's changes since the merge base. Two dots compared trees and
// showed local commits as if upstream would remove them. Import's
// placeholder master has no merge base with upstream, so it is diffed
// against the empty tree instead.

// TestProviderDiffIgnoresLocalCommits uses real git: a local commit on
// master must not appear in the review diff.
func TestProviderDiffIgnoresLocalCommits(t *testing.T) {
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
	writeFile(t, filepath.Join(pkgDir, "LOCAL.txt"), "local change\n")
	runGit(t, pkgDir, "add", "LOCAL.txt")
	runGit(t, pkgDir, "commit", "-m", "local change")

	diff, err := p.Diff(ctx, domain.PackageName(pkgName))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range diff.Files {
		paths = append(paths, f.Path)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "PKGBUILD" {
		t.Fatalf("review diff must show only upstream's change (PKGBUILD), got %v", paths)
	}
	if len(diff.Commits) != 1 {
		t.Fatalf("expected 1 upstream commit, got %d", len(diff.Commits))
	}
}

// TestProviderDiffImportPlaceholder uses real git: Import's placeholder
// master (an orphan commit with an empty tree) shares no history with
// upstream. Three dots would fail with "no merge base"; the review must
// still show every upstream file as added.
func TestProviderDiffImportPlaceholder(t *testing.T) {
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
	// Replace master with an orphan empty-tree commit, as Import does.
	runGit(t, pkgDir, "checkout", "--orphan", "placeholder")
	runGit(t, pkgDir, "rm", "-rqf", ".")
	runGit(t, pkgDir, "commit", "--allow-empty", "-m", "Initial managed branch")
	runGit(t, pkgDir, "branch", "-M", "master")

	diff, err := p.Diff(ctx, domain.PackageName(pkgName))
	if err != nil {
		t.Fatalf("import review diff failed: %v", err)
	}
	if diff.LocalCommit == "" {
		t.Fatal("expected the placeholder commit as LocalCommit")
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "PKGBUILD" {
		t.Fatalf("expected PKGBUILD as a full addition, got %#v", diff.Files)
	}
}

// TestDiffOrphanMasterUsesEmptyTree pins the call: an empty-tree master
// is diffed against the empty tree, not with three dots.
func TestDiffOrphanMasterUsesEmptyTree(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"local123\n",         // for-each-ref refs/heads/master
			"upstream456\n",      // rev-parse upstream/master
			"",                   // log reviewRange
			emptyTreeHash + "\n", // rev-parse local123^{tree} — empty
			samplePatch,          // diff
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	if _, err := p.Diff(context.Background(), domain.PackageName("yay")); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected 5 calls, got %#v", r.calls)
	}
	got := r.calls[4].Args
	if len(got) != 3 || got[0] != "diff" || got[1] != emptyTreeHash || got[2] != "upstream/master" {
		t.Fatalf("orphan master must diff against the empty tree, got %#v", got)
	}
}
