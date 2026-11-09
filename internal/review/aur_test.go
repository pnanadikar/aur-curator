package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// TestCompareAUR covers every row of analyzer-specs.md § AUR Record.
func TestCompareAUR(t *testing.T) {
	day := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	rec := func(m string, co ...string) *domain.AURRecord {
		r := &domain.AURRecord{Maintainer: m, Orphaned: m == "", ReadAt: day}
		if len(co) > 0 {
			r.CoMaintainers = co
		}
		return r
	}
	const since = " (since the review of 2026-10-30)"

	tests := []struct {
		name  string
		prior *domain.AURRecord
		now   *domain.AURRecord
		want  string // "" = no finding
	}{
		{"no prior record", nil, rec("alice"), ""},
		{"same maintainer", rec("alice"), rec("alice"), ""},
		{"same maintainer and co-maintainers", rec("alice", "bob"), rec("alice", "bob"), ""},
		{"still orphaned", rec(""), rec(""), ""},
		{"maintainer changed", rec("alice"), rec("mallory"), "AUR maintainer changed: alice → mallory" + since},
		{"orphaned", rec("alice"), rec(""), "Package orphaned: was alice" + since},
		{"adopted", rec(""), rec("mallory"), "Package adopted by mallory" + since},
		{"co-maintainer added", rec("alice"), rec("alice", "eve"), "Co-maintainers changed: added eve" + since},
		{"co-maintainer removed", rec("alice", "bob"), rec("alice"), "Co-maintainers changed: removed bob" + since},
		{"co-maintainers swapped", rec("alice", "bob", "carol"), rec("alice", "carol", "eve"), "Co-maintainers changed: added eve; removed bob" + since},
		{"maintainer and co-maintainers changed: one finding", rec("alice", "bob"), rec("mallory", "eve"), "AUR maintainer changed: alice → mallory" + since},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareAUR(tt.prior, *tt.now)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("got %d findings (%q), want none", len(got), got[0].Summary)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d findings, want 1", len(got))
			}
			f := got[0]
			if f.Summary != tt.want {
				t.Errorf("Summary = %q, want %q", f.Summary, tt.want)
			}
			if f.Category != analyzer.CategoryMaintainerChanged || f.Analyzer != "aur" || f.Location != "AUR record" {
				t.Errorf("finding = %+v", f)
			}
			if !strings.Contains(string(f.Details), `"prior"`) || !strings.Contains(string(f.Details), `"current"`) {
				t.Errorf("Details = %s, want both records", f.Details)
			}
		})
	}
}

// fakeAURLookup returns rec or err; onLookup, if set, runs first.
type fakeAURLookup struct {
	rec      aurinfo.Record
	err      error
	onLookup func()
	calls    int
}

func (f *fakeAURLookup) Lookup(context.Context, domain.PackageName) (aurinfo.Record, error) {
	f.calls++
	if f.onLookup != nil {
		f.onLookup()
	}
	return f.rec, f.err
}

// newAUREngine is an engine over a Pending review with prior as its
// carried record, one analyzer finding, and lookup.
func newAUREngine(prior *domain.AURRecord, lookup AURLookup) (*Engine, *fakeReviewStore) {
	store := &fakeReviewStore{review: &domain.Review{
		ID: "yay@abc", PackageName: "yay", Status: domain.ReviewPending, PriorAUR: prior,
	}}
	e := NewEngine(
		&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}},
		store,
		&fakeAnalyzerManager{findings: []domain.Finding{{Category: "url_new", Summary: "new url"}}},
		&fakePolicyEngine{},
		WithAURLookup(lookup),
	)
	return e, store
}

// TestRunAnalyzersRecordsAURAndFlagsChange: a successful read is stored,
// its info too, and a changed maintainer adds a finding after the
// analyzers' own.
func TestRunAnalyzersRecordsAURAndFlagsChange(t *testing.T) {
	fixed := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	old := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = old })

	prior := &domain.AURRecord{Maintainer: "alice", ReadAt: fixed.AddDate(0, 0, -10), ReviewID: "yay@old"}
	lookup := &fakeAURLookup{rec: aurinfo.Record{
		Maintainer: "mallory", CoMaintainers: []string{"eve"},
		FirstSubmitted: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), NumVotes: 5, Popularity: 0.5,
	}}
	e, store := newAUREngine(prior, lookup)

	r, err := e.RunAnalyzers(context.Background(), "yay", "yay@abc")
	if err != nil {
		t.Fatal(err)
	}
	if lookup.calls != 1 {
		t.Errorf("Lookup calls = %d, want 1", lookup.calls)
	}
	if r.AUR == nil || r.AUR.Maintainer != "mallory" || r.AUR.ReviewID != "yay@abc" || !r.AUR.ReadAt.Equal(fixed) ||
		len(r.AUR.CoMaintainers) != 1 {
		t.Errorf("AUR = %+v", r.AUR)
	}
	if r.AURInfo == nil || r.AURInfo.NumVotes != 5 || r.AURInfo.Popularity != 0.5 {
		t.Errorf("AURInfo = %+v", r.AURInfo)
	}
	if r.AURError != "" {
		t.Errorf("AURError = %q, want empty", r.AURError)
	}
	if len(r.Findings) != 2 || r.Findings[1].Category != analyzer.CategoryMaintainerChanged {
		t.Fatalf("findings = %+v, want url_new then maintainer_changed", r.Findings)
	}
	if r.Findings[1].ID == "" || r.Findings[1].ReviewID != r.ID {
		t.Errorf("maintainer_changed finding has ID %q, ReviewID %q", r.Findings[1].ID, r.Findings[1].ReviewID)
	}
	if store.review.AUR == nil || len(store.review.Findings) != 2 {
		t.Error("record and findings not saved")
	}
}

// TestRunAnalyzersAURReadFailure: a failed read saves the analyzers'
// findings and says why on the review; no maintainer finding (#495).
func TestRunAnalyzersAURReadFailure(t *testing.T) {
	prior := &domain.AURRecord{Maintainer: "alice"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unreachable", fmt.Errorf("%w: HTTP 503", aurinfo.ErrAURClientUnreachable), "aur: client unreachable: HTTP 503"},
		{"not found", fmt.Errorf("%w: yay", aurinfo.ErrAURPackageNotFound), "the AUR has no package named yay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, store := newAUREngine(prior, &fakeAURLookup{err: tt.err})

			r, err := e.RunAnalyzers(context.Background(), "yay", "yay@abc")
			if err != nil {
				t.Fatalf("RunAnalyzers failed: %v; a failed AUR read must not fail the analysis", err)
			}
			if r.AURError != tt.want {
				t.Errorf("AURError = %q, want %q", r.AURError, tt.want)
			}
			if r.AUR != nil || r.AURInfo != nil {
				t.Errorf("AUR %+v, AURInfo %+v, want nil", r.AUR, r.AURInfo)
			}
			if r.FindingsStatus != domain.FindingsRun || len(r.Findings) != 1 {
				t.Errorf("FindingsStatus %q, %d findings; want run, 1", r.FindingsStatus, len(r.Findings))
			}
			if store.saveCalls != 1 || store.review.AURError != tt.want {
				t.Error("failure not saved")
			}
			if r.PriorAUR != prior {
				t.Error("PriorAUR changed by a failed read")
			}
		})
	}
}

// TestRunAnalyzersAURCancelSavesNothing: cancelling during the AUR read
// saves nothing, like any other cancelled analysis (#410).
func TestRunAnalyzersAURCancelSavesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lookup := &fakeAURLookup{err: context.Canceled, onLookup: cancel}
	e, store := newAUREngine(nil, lookup)

	_, err := e.RunAnalyzers(ctx, "yay", "yay@abc")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if store.saveCalls != 0 {
		t.Errorf("saveCalls = %d, want 0", store.saveCalls)
	}
}

// TestRunAnalyzersWithoutAURLookup: an engine built without the option
// runs no AUR check and sets no AUR field.
func TestRunAnalyzersWithoutAURLookup(t *testing.T) {
	store := &fakeReviewStore{review: &domain.Review{ID: "yay@abc", PackageName: "yay", Status: domain.ReviewPending}}
	e := NewEngine(&fakeGitProvider{diff: &git.GitDiff{}}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := e.RunAnalyzers(context.Background(), "yay", "yay@abc")
	if err != nil {
		t.Fatal(err)
	}
	if r.AUR != nil || r.AURInfo != nil || r.AURError != "" {
		t.Errorf("AUR fields set without a lookup: %+v %+v %q", r.AUR, r.AURInfo, r.AURError)
	}
}
