package screens

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
)

// gitProvider387 records which diff the Review screen asks for.
type gitProvider387 struct {
	fakeGitProvider

	diffCalls       int
	reviewDiffCalls int
	base, upstream  string
}

func (g *gitProvider387) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	g.diffCalls++
	return &git.GitDiff{}, nil
}

func (g *gitProvider387) ReviewDiff(_ context.Context, _ domain.PackageName, base, upstream string) (*git.GitDiff, error) {
	g.reviewDiffCalls++
	g.base, g.upstream = base, upstream
	return &git.GitDiff{
		BaseCommit:     base,
		UpstreamCommit: upstream,
		Files: []git.FileDiff{{
			Path:  "PKGBUILD",
			Kind:  git.FileKindPKGBUILD,
			Patch: "diff --git a/PKGBUILD b/PKGBUILD\n-pkgver=1\n+pkgver=2\n",
		}},
	}, nil
}

func newScreen387(t *testing.T, r *domain.Review) (ReviewScreen, *gitProvider387) {
	t.Helper()
	g := &gitProvider387{}
	store := &fakeReviewStore{review: r}
	engine := review.NewEngine(g, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{
		ID: "test", WorkspaceID: "test", Version: "test",
	}))
	return NewReviewScreen("yay", g, store, engine, domain.WorkspaceConfig{}), g
}

// TestLoadReviewUsesStoredRange387: the screen loads the review's stored
// range, never the live master...upstream/master — after Merge the live
// comparison is empty (#387).
func TestLoadReviewUsesStoredRange387(t *testing.T) {
	t.Parallel()

	screen, g := newScreen387(t, &domain.Review{
		ID: "yay@up76543210", PackageName: "yay",
		BaseCommit: "base1234567", UpstreamCommit: "up76543210",
		Status: domain.ReviewApproved, FindingsStatus: domain.FindingsNotRun,
	})

	msg := screen.loadReviewCmd()()
	if g.reviewDiffCalls != 1 || g.base != "base1234567" || g.upstream != "up76543210" {
		t.Fatalf("ReviewDiff calls=%d range=%q→%q, want 1 call base1234567→up76543210",
			g.reviewDiffCalls, g.base, g.upstream)
	}
	if g.diffCalls != 0 {
		t.Fatalf("Diff called %d times, want 0", g.diffCalls)
	}

	updated, _ := screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated, _ = updated.(ReviewScreen).Update(msg)
	view := updated.(ReviewScreen).View()
	if !strings.Contains(view, "Reviewing: base123 → up76543") {
		t.Fatalf("summary does not show the stored range; got:\n%s", view)
	}
	if !strings.Contains(view, "pkgver=2") {
		t.Fatalf("diff pane does not show the reviewed changes; got:\n%s", view)
	}
}

// TestReviewRangeLabel387: the comparison summary's range label.
func TestReviewRangeLabel387(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		diff git.GitDiff
		want string
	}{
		{"not loaded", git.GitDiff{}, "…"},
		{"first review", git.GitDiff{UpstreamCommit: "up76543210"}, "(new) → up76543"},
		{"range", git.GitDiff{BaseCommit: "base1234567", UpstreamCommit: "up76543210"}, "base123 → up76543"},
	} {
		if got := reviewRangeLabel(tc.diff); got != tc.want {
			t.Errorf("%s: reviewRangeLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestEmptyRangeText387: a range that changes no files says so, without
// claiming anything about master and upstream/master.
func TestEmptyRangeText387(t *testing.T) {
	t.Parallel()

	screen, _ := newScreen387(t, &domain.Review{PackageName: "yay", Status: domain.ReviewPending})
	updated, _ := screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated, _ = updated.(ReviewScreen).Update(reviewLoadedMsg{
		diff:   git.GitDiff{BaseCommit: "base1234567", UpstreamCommit: "base1234567"},
		review: &domain.Review{PackageName: "yay", Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun},
	})
	view := updated.(ReviewScreen).View()
	if !strings.Contains(view, "No changes in the reviewed range.") {
		t.Fatalf("empty range text missing; got:\n%s", view)
	}
	if strings.Contains(view, "identical") {
		t.Fatalf("old empty text still shown; got:\n%s", view)
	}
}
