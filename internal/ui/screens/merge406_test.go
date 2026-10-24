package screens

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
)

// #406 — Merge is available exactly in Approved; where upstream's tip is
// (commits behind) plays no part.

func TestMergeAvailabilityByState406(t *testing.T) {
	tests := []struct {
		lc         lifecycle.State
		wantEnable bool
		wantReason string
	}{
		{lifecycle.NotReviewed, false, prereqApproved},
		{lifecycle.ReviewPending, false, prereqApproved},
		{lifecycle.Rejected, false, prereqApproved},
		{lifecycle.Approved, true, ""},
		{lifecycle.Merged, false, reasonNothingToMerge},
		{lifecycle.Built, false, reasonNothingToMerge},
		{lifecycle.Confirmed, false, reasonNothingToMerge},
		{lifecycle.Published, false, reasonNothingToMerge},
	}
	for _, tt := range tests {
		if got := isMergeAvailable(tt.lc); got != tt.wantEnable {
			t.Errorf("isMergeAvailable(%s) = %v, want %v", tt.lc, got, tt.wantEnable)
		}
		if !tt.wantEnable {
			if got := mergeReason(tt.lc); got != tt.wantReason {
				t.Errorf("mergeReason(%s) = %q, want %q", tt.lc, got, tt.wantReason)
			}
		}
	}
}

func TestDashboardMergeIgnoresCommitsBehind406(t *testing.T) {
	for _, behind := range []int{0, 3} {
		m := dashboardWith(lifecycle.Approved)
		m.updates["yay"] = behind
		if !m.isMergeEnabled("yay") {
			t.Errorf("Approved, %d behind: Merge disabled, want enabled", behind)
		}
		m = dashboardWith(lifecycle.Merged)
		m.updates["yay"] = behind
		if m.isMergeEnabled("yay") {
			t.Errorf("Merged, %d behind: Merge enabled, want disabled", behind)
		}
	}
}

func TestDetailMergeFollowsReviewedCommit406(t *testing.T) {
	tests := []struct {
		name       string
		status     domain.ReviewStatus
		isMerged   bool
		behind     int
		wantEnable bool
		wantReason string
	}{
		{"approved, not merged, nothing behind", domain.ReviewApproved, false, 0, true, ""},
		{"approved, not merged, behind", domain.ReviewApproved, false, 2, true, ""},
		{"approved, merged, upstream ahead", domain.ReviewApproved, true, 3, false, reasonNothingToMerge},
		{"approved, merged, level", domain.ReviewApproved, true, 0, false, reasonNothingToMerge},
		{"pending, behind", domain.ReviewPending, false, 2, false, prereqApproved},
	}
	for _, tt := range tests {
		m := detailAt(nil, "A")
		m.review = &domain.Review{Status: tt.status}
		m.isMerged = tt.isMerged
		m.commitsBehind = tt.behind
		a := actionByKey(t, m.actions(), "M")
		if a.Enabled != tt.wantEnable || (!tt.wantEnable && a.Reason != tt.wantReason) {
			t.Errorf("%s: Merge %+v, want enabled=%v reason %q", tt.name, a, tt.wantEnable, tt.wantReason)
		}
	}
}

// TestBuildAvailabilitySameOnBothScreens406: Build follows the derived state
// on Package Details exactly as on the Dashboard.
func TestBuildAvailabilitySameOnBothScreens406(t *testing.T) {
	built := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}
	tests := []struct {
		name     string
		status   domain.ReviewStatus
		isMerged bool
		ls       *domain.Build
		want     bool
	}{
		{"pending", domain.ReviewPending, false, nil, false},
		{"approved, not merged", domain.ReviewApproved, false, nil, false},
		{"approved, merged", domain.ReviewApproved, true, nil, true},
		{"approved, merged, built at HEAD", domain.ReviewApproved, true, built, true},
		{"rejected", domain.ReviewRejected, false, nil, false},
	}
	for _, tt := range tests {
		m := detailAt(tt.ls, "A")
		m.review = &domain.Review{Status: tt.status}
		m.isMerged = tt.isMerged
		lc := m.lifecycleState()
		if got := m.isBuildEnabled(); got != tt.want || got != isBuildAvailable(lc) {
			t.Errorf("%s (%s): Package Details Build = %v, isBuildAvailable = %v, want %v",
				tt.name, lc, got, isBuildAvailable(lc), tt.want)
		}
	}
}
