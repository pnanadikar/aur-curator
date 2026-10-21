package git

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// lastArgs387 returns the arguments of runner call i.
func lastArgs387(t *testing.T, r *fakeRunner, i int) []string {
	t.Helper()
	if i >= len(r.calls) {
		t.Fatalf("expected at least %d runner calls, got %d: %#v", i+1, len(r.calls), r.calls)
	}
	return r.calls[i].Args
}

// TestDiffBaseCommit387: Diff reports the merge base of master and
// upstream/master as BaseCommit, computed from the two commits it read.
func TestDiffBaseCommit387(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"local123\n",    // for-each-ref refs/heads/master
			"upstream456\n", // rev-parse upstream/master
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fBump version\n", // log
			"deadbeef\n", // rev-parse local123^{tree} — not empty
			samplePatch,  // diff
			"base789\n",  // merge-base
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.Diff(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}
	if diff.BaseCommit != "base789" {
		t.Fatalf("BaseCommit = %q, want base789", diff.BaseCommit)
	}
	got := strings.Join(lastArgs387(t, r, 5), " ")
	if got != "merge-base local123 upstream456" {
		t.Fatalf("merge-base call = %q, want %q", got, "merge-base local123 upstream456")
	}
	if len(r.calls) != 6 {
		t.Fatalf("runner calls = %d, want 6 (isEmptyMaster must run once)", len(r.calls))
	}
}

// TestDiffBaseCommitImport387: with Import's placeholder master (empty
// tree) there is no base, and no merge-base call is made.
func TestDiffBaseCommitImport387(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"placeholder1\n", // for-each-ref
			"upstream456\n",  // rev-parse upstream/master
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fInitial\n", // log
			emptyTreeHash + "\n", // rev-parse placeholder1^{tree}
			samplePatch,          // diff against the empty tree
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.Diff(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}
	if diff.BaseCommit != "" {
		t.Fatalf("BaseCommit = %q, want empty for a first review", diff.BaseCommit)
	}
	if len(r.calls) != 5 {
		t.Fatalf("runner calls = %d, want 5 (no merge-base)", len(r.calls))
	}
}

// TestDiffEmptyMergeBase387: a merge-base that prints nothing is an
// error, never an empty base (which would read as a first review).
func TestDiffEmptyMergeBase387(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{"local123\n", "upstream456\n", "", "deadbeef\n", samplePatch, "\n"},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	if _, err := p.Diff(context.Background(), domain.PackageName("yay")); err == nil {
		t.Fatal("expected an error for an empty merge base")
	}
}

// TestReviewDiff387: the stored range is used as given: both commits are
// verified, then log base..upstream and diff base upstream. No ref
// (master, upstream/master) is read.
func TestReviewDiff387(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"upfull\n",   // rev-parse --verify up^{commit}
			"basefull\n", // rev-parse --verify base^{commit}
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fBump\n", // log
			samplePatch, // diff
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.ReviewDiff(context.Background(), "yay", "base", "up")
	if err != nil {
		t.Fatal(err)
	}
	if diff.BaseCommit != "basefull" || diff.UpstreamCommit != "upfull" {
		t.Fatalf("range = %q → %q, want basefull → upfull", diff.BaseCommit, diff.UpstreamCommit)
	}
	if diff.LocalCommit != "" {
		t.Fatalf("LocalCommit = %q, want empty (ReviewDiff does not read master)", diff.LocalCommit)
	}
	if len(diff.Commits) != 1 || len(diff.Files) != 3 {
		t.Fatalf("commits/files = %d/%d, want 1/3", len(diff.Commits), len(diff.Files))
	}
	logArgs := lastArgs387(t, r, 2)
	if logArgs[0] != "log" || logArgs[len(logArgs)-1] != "basefull..upfull" {
		t.Fatalf("log call = %#v, want range basefull..upfull", logArgs)
	}
	if got := strings.Join(lastArgs387(t, r, 3), " "); got != "diff basefull upfull" {
		t.Fatalf("diff call = %q, want %q", got, "diff basefull upfull")
	}
	for _, c := range r.calls {
		for _, a := range c.Args {
			if strings.Contains(a, "master") {
				t.Fatalf("ReviewDiff read a ref: %#v", c.Args)
			}
		}
	}
}

// TestReviewDiffNoBase387: an empty base is a first review: full history
// and a diff against the empty tree.
func TestReviewDiffNoBase387(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"upfull\n", // rev-parse --verify up^{commit}
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fInitial\n", // log
			samplePatch, // diff
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.ReviewDiff(context.Background(), "yay", "", "up")
	if err != nil {
		t.Fatal(err)
	}
	if diff.BaseCommit != "" {
		t.Fatalf("BaseCommit = %q, want empty", diff.BaseCommit)
	}
	logArgs := lastArgs387(t, r, 1)
	if logArgs[len(logArgs)-1] != "upfull" {
		t.Fatalf("log call = %#v, want the full history of upfull", logArgs)
	}
	if got := strings.Join(lastArgs387(t, r, 2), " "); got != "diff "+emptyTreeHash+" upfull" {
		t.Fatalf("diff call = %q, want a diff against the empty tree", got)
	}
}

// TestReviewDiffMissingCommit387: a stored commit that is gone from the
// repository is an error naming it — never a fallback to the live range.
func TestReviewDiffMissingCommit387(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs []string
		errs    []error
		missing string
	}{
		{"upstream", []string{""}, []error{errors.New("exit status 1")}, "up"},
		{"base", []string{"upfull\n", ""}, []error{nil, errors.New("exit status 1")}, "base"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{outputs: tc.outputs, errs: tc.errs}
			p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

			_, err := p.ReviewDiff(context.Background(), "yay", "base", "up")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("error %q does not name the missing commit %q", err, tc.missing)
			}
			if len(r.calls) > 2 {
				t.Fatalf("ReviewDiff went on after a missing commit: %#v", r.calls)
			}
		})
	}
}

// TestReviewDiffNoUpstream387: a review without an upstream commit has no
// range at all.
func TestReviewDiffNoUpstream387(t *testing.T) {
	r := &fakeRunner{}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	if _, err := p.ReviewDiff(context.Background(), "yay", "base", ""); err == nil {
		t.Fatal("expected an error for an empty upstream commit")
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no git calls, got %d", len(r.calls))
	}
}
