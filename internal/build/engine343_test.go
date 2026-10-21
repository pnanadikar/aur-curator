package build

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// #343 step 1 — the build record carries LogLocation from its first save,
// so the Build Log tab can show a running (or interrupted) build.
//
// The fakes embed the interfaces they stand in for: only the methods
// RunBuild actually calls are implemented, and a call to any other
// method panics, which would itself flag an unexpected dependency.

type lp343Store struct {
	domain.BuildStore
	current *domain.Build
	saves   []domain.Build
}

func (s *lp343Store) Get(domain.PackageName) (*domain.Build, error) {
	if s.current == nil {
		return nil, nil
	}
	cp := *s.current
	return &cp, nil
}

func (s *lp343Store) Save(_ domain.PackageName, b *domain.Build) error {
	cp := *b
	s.current = &cp
	s.saves = append(s.saves, cp)
	return nil
}

func (s *lp343Store) Delete(domain.PackageName) error { s.current = nil; return nil }

func (s *lp343Store) SaveLastSuccess(domain.PackageName, *domain.Build) error { return nil }

type lp343Git struct{ git.Provider }

func (lp343Git) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return "abc", nil
}

func (lp343Git) CloneAt(_ context.Context, _ domain.PackageName, _ string, dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// lp343Builder records what the store held while the build was running
// and the log path it was given.
type lp343Builder struct {
	st        *lp343Store
	result    BuildResult
	err       error
	seen      *domain.Build
	gotLogArg string
}

func (b *lp343Builder) Build(_ context.Context, _ string, logPath string) (BuildResult, error) {
	b.gotLogArg = logPath
	b.seen, _ = b.st.Get("yay")
	return b.result, b.err
}

func lp343Paths(t *testing.T) *store.StorePaths {
	t.Helper()
	// NewStorePaths may return a value or a pointer; accept either.
	var p any = store.NewStorePaths(t.TempDir())
	switch v := p.(type) {
	case *store.StorePaths:
		return v
	case store.StorePaths:
		return &v
	}
	t.Fatalf("unexpected NewStorePaths type %T", p)
	return nil
}

// TestRunningBuildRecordHasLogLocation: every save before the builder
// runs, and the record the UI reads mid-build, carry the log path.
func TestRunningBuildRecordHasLogLocation(t *testing.T) {
	st := &lp343Store{}
	b := &lp343Builder{st: st}
	p := lp343Paths(t)
	e := NewEngine(b, st, jsonstore.NewArtifactStore(p), lp343Git{}, p)

	if _, err := e.RunBuild(context.Background(), "yay"); err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	if b.gotLogArg == "" {
		t.Fatal("builder got no log path")
	}
	if b.seen == nil || b.seen.Status != domain.BuildRunning {
		t.Fatalf("record during build = %+v, want Running", b.seen)
	}
	if b.seen.LogLocation != b.gotLogArg {
		t.Fatalf("running record LogLocation = %q, want %q", b.seen.LogLocation, b.gotLogArg)
	}
	for i, s := range st.saves {
		if s.Status == domain.BuildPending || s.Status == domain.BuildRunning {
			if s.LogLocation != b.gotLogArg {
				t.Fatalf("save %d (%s) LogLocation = %q, want %q", i, s.Status, s.LogLocation, b.gotLogArg)
			}
		}
	}
}

// TestFinishedBuildKeepsLogLocation: a builder that reports no log
// location must not wipe the start-time path, on success or failure.
func TestFinishedBuildKeepsLogLocation(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus domain.BuildStatus
	}{
		{"success", nil, domain.BuildSuccess},
		{"failure", errors.New("makepkg exploded"), domain.BuildFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &lp343Store{}
			b := &lp343Builder{st: st, err: tc.err} // result.LogLocation == ""
			p := lp343Paths(t)
			e := NewEngine(b, st, jsonstore.NewArtifactStore(p), lp343Git{}, p)

			_, _ = e.RunBuild(context.Background(), "yay")
			if st.current == nil || st.current.Status != tc.wantStatus {
				t.Fatalf("final record = %+v, want %s", st.current, tc.wantStatus)
			}
			if st.current.LogLocation != b.gotLogArg {
				t.Fatalf("final LogLocation = %q, want %q", st.current.LogLocation, b.gotLogArg)
			}
		})
	}
}
