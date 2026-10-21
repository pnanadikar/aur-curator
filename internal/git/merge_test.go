package git

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// Merge's call sequence (see merge.go):
//
//	0 for-each-ref refs/heads/master    (localMasterStrict)
//	1 rev-parse --verify <commit>^{commit}
//	2 rev-parse <local>^{tree}          (isEmptyMaster; skipped when local == "")
//	first merge: reset --hard <target>, push -u origin master
//	otherwise:   merge-base master <target>, then merge --ff-only / push,
//	             or rev-list --count <target>..master on divergence

const (
	testLocal  = "abc123"
	testTarget = "fff999"
)

func newMergeProvider(r *fakeRunner) *provider {
	return &provider{runner: r, catalog: &fakeCatalog{}, forge: &fakeForge{}, client: http.DefaultClient}
}

func mergeArgsEqual(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// mergeAssertNoCall fails if any recorded call's first argument is cmd.
func mergeAssertNoCall(t *testing.T, calls []runnerCall, cmd string) {
	t.Helper()
	for _, c := range calls {
		if len(c.Args) > 0 && c.Args[0] == cmd {
			t.Fatalf("unexpected %q call: %#v", cmd, c.Args)
		}
	}
}

// mergeAssertNoUpstreamRef enforces workflow.md § Merge: Merge must use
// the reviewed commit, never the moving upstream/master ref.
func mergeAssertNoUpstreamRef(t *testing.T, calls []runnerCall) {
	t.Helper()
	for _, c := range calls {
		for _, a := range c.Args {
			if a == "upstream/master" {
				t.Fatalf("Merge used upstream/master: %#v", c.Args)
			}
		}
	}
}

func TestMergeFirstImport(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			testLocal + "\n",     // for-each-ref
			testTarget + "\n",    // rev-parse ^{commit}
			emptyTreeHash + "\n", // rev-parse ^{tree}
		},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %#v", len(r.calls), r.calls)
	}
	if !mergeArgsEqual(r.calls[3].Args, "reset", "--hard", testTarget) {
		t.Fatalf("unexpected reset call: %#v", r.calls[3].Args)
	}
	if !mergeArgsEqual(r.calls[4].Args, "push", "-u", "origin", "master") {
		t.Fatalf("unexpected push call: %#v", r.calls[4].Args)
	}
	mergeAssertNoUpstreamRef(t, r.calls)
}

func TestMergeFirstImportNoMasterRef(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"",                // for-each-ref: master absent
			testTarget + "\n", // rev-parse ^{commit}
		},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %d: %#v", len(r.calls), r.calls)
	}
	if !mergeArgsEqual(r.calls[2].Args, "reset", "--hard", testTarget) {
		t.Fatalf("unexpected reset call: %#v", r.calls[2].Args)
	}
	mergeAssertNoUpstreamRef(t, r.calls)
}

func TestMergeSubsequent(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			testLocal + "\n",  // for-each-ref
			testTarget + "\n", // rev-parse ^{commit}
			"deadbeef\n",      // rev-parse ^{tree} — not emptyTreeHash
			testLocal + "\n",  // merge-base == local: fast-forward possible
		},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 6 {
		t.Fatalf("expected 6 calls, got %d: %#v", len(r.calls), r.calls)
	}
	if !mergeArgsEqual(r.calls[3].Args, "merge-base", "master", testTarget) {
		t.Fatalf("unexpected merge-base call: %#v", r.calls[3].Args)
	}
	if !mergeArgsEqual(r.calls[4].Args, "merge", "--ff-only", testTarget) {
		t.Fatalf("unexpected merge call: %#v", r.calls[4].Args)
	}
	if !mergeArgsEqual(r.calls[5].Args, "push", "origin", "master") {
		t.Fatalf("unexpected push call: %#v", r.calls[5].Args)
	}
	mergeAssertNoUpstreamRef(t, r.calls)
}

func TestMergeUsesResolvedCommit(t *testing.T) {
	// A short reviewed commit is resolved once; every later call uses
	// the full SHA rev-parse returned, never the caller's string.
	r := &fakeRunner{
		outputs: []string{testLocal + "\n", testTarget + "\n", "deadbeef\n", testLocal + "\n"},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), "fff"); err != nil {
		t.Fatal(err)
	}
	if !mergeArgsEqual(r.calls[1].Args, "rev-parse", "--verify", "--quiet", "fff^{commit}") {
		t.Fatalf("unexpected rev-parse call: %#v", r.calls[1].Args)
	}
	if !mergeArgsEqual(r.calls[3].Args, "merge-base", "master", testTarget) {
		t.Fatalf("merge-base did not use the resolved SHA: %#v", r.calls[3].Args)
	}
	if !mergeArgsEqual(r.calls[4].Args, "merge", "--ff-only", testTarget) {
		t.Fatalf("merge did not use the resolved SHA: %#v", r.calls[4].Args)
	}
}

func TestMergeFirstImportPushFailure(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{testLocal + "\n", testTarget + "\n", emptyTreeHash + "\n"},
		errs:    []error{nil, nil, nil, nil, errors.New("push rejected")},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected reset to run before the failing push, got %d calls", len(r.calls))
	}
}

func TestMergeSubsequentMergeFailure(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{testLocal + "\n", testTarget + "\n", "deadbeef\n", testLocal + "\n"},
		errs:    []error{nil, nil, nil, nil, errors.New("not possible to fast-forward")},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected no push after a failed merge, got %d calls", len(r.calls))
	}
}

func TestMergeRepoPathFailure(t *testing.T) {
	r := &fakeRunner{}
	p := &provider{runner: r, catalog: &fakeCatalog{err: errors.New("boom")}, forge: &fakeForge{}, client: http.DefaultClient}
	if err := p.Merge(context.Background(), domain.PackageName("yay"), testTarget); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no git calls, got %#v", r.calls)
	}
}

func TestMergeEmptyCommit(t *testing.T) {
	r := &fakeRunner{}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), ""); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no git calls, got %#v", r.calls)
	}
}

func TestMergeStrictLookupFailureNeverResets(t *testing.T) {
	// A git failure while reading master must not be mistaken for
	// "no master" — that would take the reset --hard path.
	r := &fakeRunner{errs: []error{errors.New("git broke")}}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected only the master lookup, got %#v", r.calls)
	}
	mergeAssertNoCall(t, r.calls, "reset")
}

func TestMergeReviewedCommitNotFound(t *testing.T) {
	for name, r := range map[string]*fakeRunner{
		"rev-parse error": {
			outputs: []string{testLocal + "\n"},
			errs:    []error{nil, errors.New("unknown revision")},
		},
		"empty output": {
			outputs: []string{testLocal + "\n", ""},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err == nil {
				t.Fatal("expected error")
			}
			if len(r.calls) != 2 {
				t.Fatalf("expected 2 calls, got %#v", r.calls)
			}
			mergeAssertNoCall(t, r.calls, "reset")
			mergeAssertNoCall(t, r.calls, "merge")
			mergeAssertNoCall(t, r.calls, "push")
		})
	}
}

func TestMergeLocalCommitsBlockFastForward(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			testLocal + "\n",  // for-each-ref
			testTarget + "\n", // rev-parse ^{commit}
			"deadbeef\n",      // rev-parse ^{tree}
			"0ld000\n",        // merge-base: neither local nor target
			"2\n",             // rev-list --count target..master
		},
	}
	err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget)
	var nff *NotFastForwardError
	if !errors.As(err, &nff) {
		t.Fatalf("expected *NotFastForwardError, got %v", err)
	}
	if nff.LocalCommits != 2 || nff.Commit != testTarget || nff.Package != "yay" {
		t.Fatalf("unexpected error fields: %+v", nff)
	}
	if !mergeArgsEqual(r.calls[4].Args, "rev-list", "--count", testTarget+"..master") {
		t.Fatalf("unexpected count call: %#v", r.calls[4].Args)
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected 5 calls, got %#v", r.calls)
	}
	mergeAssertNoCall(t, r.calls, "merge")
	mergeAssertNoCall(t, r.calls, "push")
	mergeAssertNoCall(t, r.calls, "reset")
}

func TestMergeLocalCommitCountFailure(t *testing.T) {
	for name, r := range map[string]*fakeRunner{
		"rev-list error": {
			outputs: []string{testLocal + "\n", testTarget + "\n", "deadbeef\n", "0ld000\n"},
			errs:    []error{nil, nil, nil, nil, errors.New("boom")},
		},
		"non-numeric": {
			outputs: []string{testLocal + "\n", testTarget + "\n", "deadbeef\n", "0ld000\n", "lots\n"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget)
			if err == nil {
				t.Fatal("expected error")
			}
			var nff *NotFastForwardError
			if errors.As(err, &nff) {
				t.Fatalf("a failed count must not produce NotFastForwardError: %v", err)
			}
			mergeAssertNoCall(t, r.calls, "push")
		})
	}
}

func TestMergeAlreadyAtReviewedCommitRetriesPush(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			testTarget + "\n", // for-each-ref: master already at target
			testTarget + "\n", // rev-parse ^{commit}
			"deadbeef\n",      // rev-parse ^{tree}
			testTarget + "\n", // merge-base == target
		},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 5 {
		t.Fatalf("expected 5 calls, got %#v", r.calls)
	}
	if !mergeArgsEqual(r.calls[4].Args, "push", "origin", "master") {
		t.Fatalf("unexpected push call: %#v", r.calls[4].Args)
	}
	mergeAssertNoCall(t, r.calls, "merge")
}

func TestMergeAlreadyContainedWithLocalCommitsDoesNotPush(t *testing.T) {
	// Master contains the reviewed commit plus local commits on top.
	// Pushing would publish unreviewed commits as part of this review.
	r := &fakeRunner{
		outputs: []string{
			testLocal + "\n",  // for-each-ref: master ahead of target
			testTarget + "\n", // rev-parse ^{commit}
			"deadbeef\n",      // rev-parse ^{tree}
			testTarget + "\n", // merge-base == target
		},
	}
	if err := newMergeProvider(r).Merge(context.Background(), domain.PackageName("yay"), testTarget); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("expected 4 calls, got %#v", r.calls)
	}
	mergeAssertNoCall(t, r.calls, "push")
	mergeAssertNoCall(t, r.calls, "merge")
}

func TestNotFastForwardErrorMessage(t *testing.T) {
	e := &NotFastForwardError{Package: "yay", Commit: "0123456789abcdef", LocalCommits: 3}
	want := "merge yay: master has 3 local commit(s) not in reviewed commit 0123456; fast-forward impossible — resolve in git (rebase or reset) and retry"
	if e.Error() != want {
		t.Fatalf("got %q\nwant %q", e.Error(), want)
	}
}
