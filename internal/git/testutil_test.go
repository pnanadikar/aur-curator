package git

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
)

type runnerCall struct {
	Dir  string
	Args []string
}

type fakeRunner struct {
	calls  []runnerCall
	output string
	err    error

	// outputs/errs, when set, override output/err per call, indexed by
	// call order (0-based). Shorter than the number of calls made falls
	// back to output/err for the remaining calls. Existing tests that
	// only set output/err are unaffected.
	outputs []string
	errs    []error
}

func (f *fakeRunner) Run(
	_ context.Context,
	dir string,
	args ...string,
) (string, error) {
	idx := len(f.calls)
	f.calls = append(f.calls, runnerCall{
		Dir:  dir,
		Args: append([]string(nil), args...),
	})

	out, err := f.output, f.err
	if idx < len(f.outputs) {
		out = f.outputs[idx]
	}
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return out, err
}

type fakeCatalog struct {
	repoPath string
	err      error
	pkgs     []domain.PackageName
}

func (f *fakeCatalog) Packages(_ context.Context) ([]domain.PackageName, error) {
	if f.pkgs == nil {
		return []domain.PackageName{"placeholder"}, nil
	}
	return f.pkgs, f.err
}

func (f *fakeCatalog) RepositoryPath(domain.PackageName) (string, error) {
	if f.repoPath == "" {
		f.repoPath = "/tmp/repo"
	}
	return f.repoPath, f.err
}

type fakeForge struct {
	created  []string
	deleted  []string
	err      error
	cloneURL string
}

func (f *fakeForge) CreateRepository(_ context.Context, name string) (string, error) {
	f.created = append(f.created, name)
	url := f.cloneURL
	if url == "" {
		url = "https://gitea.example.com/aur/" + name + ".git"
	}
	return url, f.err
}

func (f *fakeForge) DeleteRepository(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return f.err
}
