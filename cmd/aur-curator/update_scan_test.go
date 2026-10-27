package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
)

// #426 — update compares the AUR with the reviewed commit and asks by
// what a fetch would discard.

func TestFetchLossFor(t *testing.T) {
	tests := []struct {
		status   domain.ReviewStatus
		merged   bool
		wantLoss fetchLoss
		wantOK   bool
	}{
		{domain.ReviewPending, false, lossPendingReview, true},
		{domain.ReviewApproved, false, lossUnmergedApproval, true},
		{domain.ReviewApproved, true, lossNone, true},
		{domain.ReviewRejected, false, lossNone, true},
		{domain.ReviewStatus("bogus"), false, 0, false},
	}
	for _, tt := range tests {
		loss, ok := fetchLossFor(tt.status, tt.merged)
		if loss != tt.wantLoss || ok != tt.wantOK {
			t.Errorf("fetchLossFor(%q, merged=%v) = (%v, %v), want (%v, %v)",
				tt.status, tt.merged, loss, ok, tt.wantLoss, tt.wantOK)
		}
	}
}

func TestPlanUpdate(t *testing.T) {
	stale := func(pkg, local, aur string) aurinfo.VersionPair {
		return aurinfo.VersionPair{Pkg: domain.PackageName(pkg), LocalVersion: local, AURVersion: aur, AURStale: true}
	}
	current := func(pkg, ver string) aurinfo.VersionPair {
		return aurinfo.VersionPair{Pkg: domain.PackageName(pkg), LocalVersion: ver, AURVersion: ver}
	}
	pairs := []aurinfo.VersionPair{
		stale("pending-stale", "1.0-1", "1.1-1"),
		current("pending-current", "2.0-1"),
		stale("approved-unmerged", "1.11.0-1", "1.12.0-1"),
		current("approved-unmerged-current", "1.11.0-1"), // pfetch: approved, AUR = reviewed
		stale("approved-merged", "3.0-1", "3.1-1"),
		stale("rejected", "4.0-1", "4.1-1"),
		current("rejected-current", "4.0-1"),
		{Pkg: "no-review", AURVersion: "5.1-1", LocalUnknown: true, LocalErr: aurinfo.ErrNoReview},
		stale("store-error", "6.0-1", "6.1-1"),
		{Pkg: "pending-unknown", AURVersion: "7.0-1", LocalUnknown: true, LocalErr: aurinfo.ErrVersionNotReadable},
		{Pkg: "pending-not-on-aur", LocalVersion: "8.0-1", AURUnknown: true},
		stale("merge-error", "9.0-1", "9.1-1"),
		current("merge-error-current", "9.5-1"),
		stale("bogus-status", "10.0-1", "10.1-1"),
		{Pkg: "read-timeout", AURVersion: "11.1-1", LocalUnknown: true, LocalErr: errors.New("read PKGBUILD at abc1234: timed out after 1m0s")},
	}
	reviews := map[domain.PackageName]*domain.Review{
		"pending-stale":             {Status: domain.ReviewPending, UpstreamCommit: "p1"},
		"pending-current":           {Status: domain.ReviewPending, UpstreamCommit: "p2"},
		"approved-unmerged":         {Status: domain.ReviewApproved, UpstreamCommit: "a1"},
		"approved-unmerged-current": {Status: domain.ReviewApproved, UpstreamCommit: "a2"},
		"approved-merged":           {Status: domain.ReviewApproved, UpstreamCommit: "m1"},
		"rejected":                  {Status: domain.ReviewRejected, UpstreamCommit: "r1"},
		"rejected-current":          {Status: domain.ReviewRejected, UpstreamCommit: "r2"},
		"pending-unknown":           {Status: domain.ReviewPending, UpstreamCommit: "p3"},
		"pending-not-on-aur":        {Status: domain.ReviewPending, UpstreamCommit: "p4"},
		"merge-error":               {Status: domain.ReviewApproved, UpstreamCommit: "e1"},
		"merge-error-current":       {Status: domain.ReviewApproved, UpstreamCommit: "e2"},
		"bogus-status":              {Status: domain.ReviewStatus("bogus"), UpstreamCommit: "b1"},
		"read-timeout":              {Status: domain.ReviewPending, UpstreamCommit: "t1"},
	}
	getReview := func(pkg domain.PackageName) (*domain.Review, error) {
		if pkg == "store-error" {
			return nil, errors.New("store error")
		}
		if r, ok := reviews[pkg]; ok {
			return r, nil
		}
		return nil, domain.ErrNotFound
	}
	var mergedAsked []string
	merged := func(pkg domain.PackageName, commit string) (bool, error) {
		mergedAsked = append(mergedAsked, commit)
		if commit == "e1" || commit == "e2" {
			return true, errors.New("git broke") // the result is ignored on error
		}
		return commit == "m1", nil
	}

	p := planUpdate(pairs, getReview, merged)

	if want := []domain.PackageName{"pending-current"}; !reflect.DeepEqual(p.pendingUpToDate, want) {
		t.Errorf("pendingUpToDate = %v, want %v", p.pendingUpToDate, want)
	}
	wantIndividual := []individualCandidate{
		{updateCandidate{pkg: "pending-stale", localVer: "1.0-1", aurVer: "1.1-1"}, lossPendingReview},
		{updateCandidate{pkg: "approved-unmerged", localVer: "1.11.0-1", aurVer: "1.12.0-1"}, lossUnmergedApproval},
		// A failed merge check is not merged (fail-closed): asked on its own.
		{updateCandidate{pkg: "merge-error", localVer: "9.0-1", aurVer: "9.1-1"}, lossUnmergedApproval},
	}
	if !reflect.DeepEqual(p.individual, wantIndividual) {
		t.Errorf("individual = %+v, want %+v", p.individual, wantIndividual)
	}
	wantBatch := []updateCandidate{
		{pkg: "approved-merged", localVer: "3.0-1", aurVer: "3.1-1"},
		{pkg: "rejected", localVer: "4.0-1", aurVer: "4.1-1"},
	}
	if !reflect.DeepEqual(p.batch, wantBatch) {
		t.Errorf("batch = %+v, want %+v", p.batch, wantBatch)
	}
	// IsMerged is asked only for approved reviews, with the reviewed commit.
	if want := []string{"a1", "a2", "m1", "e1", "e2"}; !reflect.DeepEqual(mergedAsked, want) {
		t.Errorf("merged asked for %v, want %v", mergedAsked, want)
	}

	// #402 — every pair lands somewhere, with its reason.
	if want := 4; p.upToDate != want { // pending-current, approved-unmerged-current, rejected-current, merge-error-current
		t.Errorf("upToDate = %d, want %d", p.upToDate, want)
	}
	wantNotChecked := []pkgReason{
		{"pending-unknown", aurinfo.ErrVersionNotReadable.Error()},
		{"pending-not-on-aur", reasonNotOnAUR},
	}
	if !reflect.DeepEqual(p.notChecked, wantNotChecked) {
		t.Errorf("notChecked = %+v, want %+v", p.notChecked, wantNotChecked)
	}
	wantFailed := []pkgReason{
		{"no-review", "no review record"},
		{"store-error", "read review: store error"},
		{"merge-error", "check whether the approved commit is merged: git broke"},
		{"merge-error-current", "check whether the approved commit is merged: git broke"},
		{"bogus-status", `unknown review status "bogus"`},
		{"read-timeout", "read PKGBUILD at abc1234: timed out after 1m0s"},
	}
	if !reflect.DeepEqual(p.failed, wantFailed) {
		t.Errorf("failed = %+v, want %+v", p.failed, wantFailed)
	}
	wantPending := []domain.PackageName{"pending-stale", "pending-current", "pending-unknown", "pending-not-on-aur"}
	if !reflect.DeepEqual(p.pending, wantPending) {
		t.Errorf("pending = %v, want %v", p.pending, wantPending)
	}
}

// Exact wording, so a change is a deliberate one.
func TestIndividualPrompt(t *testing.T) {
	tests := []struct {
		loss fetchLoss
		want string
	}{
		{lossPendingReview, "pfetch: pending review is stale — AUR has moved to 1.12.0-1 since this review was created at 1.11.0-1. Confirming will discard the current review's notes/findings. Proceed? [y/n/q] "},
		{lossUnmergedApproval, "pfetch: approved review (1.11.0-1) is not merged yet — AUR has moved to 1.12.0-1. Fetching replaces the approval with a new pending review. Proceed? [y/n/q] "},
	}
	for _, tt := range tests {
		c := individualCandidate{updateCandidate{pkg: "pfetch", localVer: "1.11.0-1", aurVer: "1.12.0-1"}, tt.loss}
		if got := individualPrompt(c); got != tt.want {
			t.Errorf("individualPrompt(loss=%v) =\n%q\nwant\n%q", tt.loss, got, tt.want)
		}
	}
}
