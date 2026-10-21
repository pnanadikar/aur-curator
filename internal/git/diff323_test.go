package git

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// #323 — Diff must not report a git failure while reading master as "no
// master": that made an already-merged package look newly imported (a
// "(new)" whole-file review). An absent master is empty output; a failure
// is an error.

func TestDiffMasterLookupFailureIsAnError(t *testing.T) {
	r := &fakeRunner{
		errs: []error{errors.New("fatal: not a git repository")},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.Diff(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatalf("expected an error, got a diff with LocalCommit %q", diff.LocalCommit)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected Diff to stop after the master lookup, got %#v", r.calls)
	}
}

func TestDiffUsesStrictMasterLookup(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"local123\n",    // for-each-ref refs/heads/master
			"upstream456\n", // rev-parse upstream/master
			"",              // log reviewRange
			"deadbeef\n",    // rev-parse local123^{tree}
			"",              // diff reviewDiffRange
			"base789\n",     // merge-base local123 upstream456 (#387)
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	if _, err := p.Diff(context.Background(), domain.PackageName("yay")); err != nil {
		t.Fatal(err)
	}
	got := r.calls[0].Args
	want := []string{"for-each-ref", "--format=%(objectname)", "refs/heads/master"}
	if len(got) != len(want) {
		t.Fatalf("master lookup: got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("master lookup: got %#v, want %#v", got, want)
		}
	}
}
