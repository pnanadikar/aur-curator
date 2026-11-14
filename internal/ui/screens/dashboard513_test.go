package screens

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
)

// metadataGit513 is a fakeGitProvider whose Metadata answers per package:
// an error from errs, else metas[pkg], else both remotes set.
type metadataGit513 struct {
	fakeGitProvider
	errs  map[domain.PackageName]error
	metas map[domain.PackageName]*git.RepositoryMetadata
	calls *int
}

func (g metadataGit513) Metadata(_ context.Context, pkg domain.PackageName) (*git.RepositoryMetadata, error) {
	if g.calls != nil {
		*g.calls++
	}
	if err, ok := g.errs[pkg]; ok {
		return nil, err
	}
	if meta, ok := g.metas[pkg]; ok {
		return meta, nil
	}
	return &git.RepositoryMetadata{OriginURL: "o", UpstreamURL: "u"}, nil
}

// errGitNotFound513 is the error ExecRunner returns when git is not on PATH.
var errGitNotFound513 = fmt.Errorf("git [branch --show-current]: %w", &exec.Error{Name: "git", Err: exec.ErrNotFound})

// load513 runs loadPackagesCmd over pkgs and returns its message and the
// error log's contents.
func load513(t *testing.T, ctx context.Context, g metadataGit513, pkgs ...domain.PackageName) (PackagesLoadedMsg, string) {
	t.Helper()
	root := t.TempDir()
	store := &fakeReviewStore{review: &domain.Review{ID: "r1", Status: domain.ReviewPending}}
	m := NewDashboardModel(deps.Dependencies{
		Git:     g,
		Catalog: &mockCatalog{pkgs: pkgs},
		Reviews: review.NewEngine(g, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{})),
		Config:  domain.WorkspaceConfig{RootPath: root},
	})
	msg, ok := m.loadPackagesCmd(ctx)().(PackagesLoadedMsg)
	if !ok {
		t.Fatal("loadPackagesCmd did not return a PackagesLoadedMsg")
	}
	logged, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read error log: %v", err)
	}
	return msg, string(logged)
}

// TestLoadPackagesGitCannotStart513: git not starting ends the load with
// its own error, checked on the first package only, and logged once —
// not "no origin/upstream remotes found" per package (#513).
func TestLoadPackagesGitCannotStart513(t *testing.T) {
	t.Parallel()
	calls := 0
	all := map[domain.PackageName]error{"a": errGitNotFound513, "b": errGitNotFound513}
	msg, logged := load513(t, context.Background(), metadataGit513{errs: all, calls: &calls}, "a", "b")

	if !errors.Is(msg.Err, exec.ErrNotFound) {
		t.Fatalf("Err = %v, want exec.ErrNotFound", msg.Err)
	}
	if len(msg.Packages) != 0 || len(msg.Skipped) != 0 {
		t.Errorf("Packages = %v, Skipped = %v, want both empty", msg.Packages, msg.Skipped)
	}
	if calls != 1 {
		t.Errorf("Metadata called %d times, want 1", calls)
	}
	if !strings.Contains(logged, "package list not loaded: "+errGitNotFound513.Error()) {
		t.Errorf("error log lacks the real error:\n%s", logged)
	}
	if n := strings.Count(logged, "\n"); n != 1 {
		t.Errorf("error log has %d lines, want 1:\n%s", n, logged)
	}
	if strings.Contains(logged, "no origin/upstream") {
		t.Errorf("error log blames the remotes:\n%s", logged)
	}
}

// TestLoadPackagesContextEnded513: a cancelled (or timed-out) load ends
// with that error instead of skipping every remaining package (#513).
func TestLoadPackagesContextEnded513(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	interrupted := git.Interrupted(ctx, errors.New("signal: killed"))
	msg, logged := load513(t, ctx, metadataGit513{errs: map[domain.PackageName]error{"a": interrupted}}, "a", "b")

	if !errors.Is(msg.Err, context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", msg.Err)
	}
	if len(msg.Skipped) != 0 {
		t.Errorf("Skipped = %v, want empty", msg.Skipped)
	}
	if !strings.Contains(logged, "package list not loaded") {
		t.Errorf("error log lacks the failed load:\n%s", logged)
	}
}

// TestLoadPackagesCatalogErrorLogged513: a catalog failure ends the load
// as before, and now also reaches the error log.
func TestLoadPackagesCatalogErrorLogged513(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := NewDashboardModel(deps.Dependencies{
		Catalog: failingCatalog513{},
		Config:  domain.WorkspaceConfig{RootPath: root},
	})
	msg := m.loadPackagesCmd(context.Background())().(PackagesLoadedMsg)
	if msg.Err == nil {
		t.Fatal("Err = nil, want the catalog's error")
	}
	logged, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil || !strings.Contains(string(logged), "package list not loaded: catalog: unreadable") {
		t.Errorf("error log = %q, %v; want the catalog's error", logged, err)
	}
}

type failingCatalog513 struct{}

func (failingCatalog513) Packages(context.Context) ([]domain.PackageName, []catalog.Rejected, error) {
	return nil, nil, errors.New("catalog: unreadable")
}

func (failingCatalog513) RepositoryPath(domain.PackageName) (string, error) { return "", nil }

// TestLoadPackagesSkipReasons513: a package git fails on, or one with a
// missing remote, is skipped with its real reason; the rest load.
func TestLoadPackagesSkipReasons513(t *testing.T) {
	t.Parallel()
	noRemote := errors.New("git [remote get-url upstream]: exit status 2: error: No such remote 'upstream'")
	g := metadataGit513{
		errs:  map[domain.PackageName]error{"broken": noRemote},
		metas: map[domain.PackageName]*git.RepositoryMetadata{"emptyurl": {OriginURL: "o"}, "nilmeta": nil},
	}
	msg, logged := load513(t, context.Background(), g, "good", "broken", "emptyurl", "nilmeta")

	if msg.Err != nil {
		t.Fatalf("Err = %v, want nil", msg.Err)
	}
	if fmt.Sprint(msg.Packages) != "[good]" {
		t.Errorf("Packages = %v, want [good]", msg.Packages)
	}
	if fmt.Sprint(msg.Skipped) != "[broken emptyurl nilmeta]" {
		t.Errorf("Skipped = %v, want [broken emptyurl nilmeta]", msg.Skipped)
	}
	for _, want := range []string{
		`skipped "broken": ` + noRemote.Error(),
		`skipped "emptyurl": no origin/upstream remotes found`,
		`skipped "nilmeta": no origin/upstream remotes found`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("error log lacks %q:\n%s", want, logged)
		}
	}
}

// TestDashboardSkippedState513: Skipped is kept from the last load, the
// empty body says packages were left out, and a clean load clears it.
func TestDashboardSkippedState513(t *testing.T) {
	t.Parallel()
	m := NewDashboardModel(deps.Dependencies{})

	m, _ = m.Update(PackagesLoadedMsg{Skipped: []domain.PackageName{"a", "b"}, TraceCtx: context.Background()})
	if fmt.Sprint(m.Skipped()) != "[a b]" {
		t.Errorf("Skipped() = %v, want [a b]", m.Skipped())
	}
	if got, want := m.View(), "No packages loaded: 2 left out (see the error log).\n"; got != want {
		t.Errorf("View() = %q, want %q", got, want)
	}

	m, _ = m.Update(PackagesLoadedMsg{TraceCtx: context.Background()})
	if len(m.Skipped()) != 0 {
		t.Errorf("Skipped() after a clean load = %v, want empty", m.Skipped())
	}
	if got, want := m.View(), "No curated packages found.\n"; got != want {
		t.Errorf("View() = %q, want %q", got, want)
	}

	m, _ = m.Update(PackagesLoadedMsg{Err: errGitNotFound513, TraceCtx: context.Background()})
	if got := m.View(); !strings.Contains(got, "Error loading packages: "+errGitNotFound513.Error()) {
		t.Errorf("View() = %q, want the load error", got)
	}
}
