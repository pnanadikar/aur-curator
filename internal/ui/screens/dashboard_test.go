package screens

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

func TestRiskForNilReview(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	got := m.riskFor(nil, lifecycle.ReviewPending)
	want := "No review"

	if got != want {
		t.Errorf("riskFor(nil, ReviewPending) = %q, want %q", got, want)
	}
}

func TestRiskForPendingNotRun(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsNotRun,
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "Not Run"

	if got != want {
		t.Errorf("riskFor(pending, not_run) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunEmpty(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{},
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "NONE"

	if got != want {
		t.Errorf("riskFor(pending, run, empty) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunWithLowFindings(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "version_bump", Summary: "bump"},
		},
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "LOW"

	if got != want {
		t.Errorf("riskFor(pending, run, low) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunWithHighFindings(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "shell_exec", Summary: "exec"},
		},
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(pending, run, high) = %q, want %q", got, want)
	}
}

func TestRiskForApprovedWithRiskAssessmentHigh(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityHigh,
		},
	}

	got := m.riskFor(review, lifecycle.Approved)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(approved, risk=HIGH) = %q, want %q", got, want)
	}
}

func TestRiskForApprovedWithRiskAssessmentLow(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "version_bump", Summary: "bump"}},
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityLow,
		},
	}

	got := m.riskFor(review, lifecycle.Approved)
	want := "LOW"

	if got != want {
		t.Errorf("riskFor(approved, risk=LOW) = %q, want %q", got, want)
	}
}

func TestRiskForRejectedWithRiskAssessmentMedium(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewRejected,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "version_bump", Summary: "bump"}},
		// version_bump maps to LOW, but the review's frozen RA says MEDIUM.
		// Row 4 (decided + RA!=nil) takes priority over live finding severity.
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityMedium,
		},
	}

	got := m.riskFor(review, lifecycle.Rejected)
	want := "MEDIUM"

	if got != want {
		t.Errorf("riskFor(rejected, risk=MEDIUM) = %q, want %q", got, want)
	}
}

func TestRiskForApprovedNoRiskAssessmentFallback(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "url_new", Summary: "new url"},
		},
	}

	got := m.riskFor(review, lifecycle.Approved)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(approved, no RA, high findings) = %q, want %q", got, want)
	}
}

func TestRiskForNotReviewed(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	got := m.riskFor(nil, lifecycle.NotReviewed)
	want := "No review"

	if got != want {
		t.Errorf("riskFor(nil, NotReviewed) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunWithMixedFindings(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "version_bump", Summary: "bump"},
			{Category: "shell_exec", Summary: "exec"},
			{Category: "network_download", Summary: "download"},
		},
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(pending, run, mixed high) = %q, want %q", got, want)
	}
}

func TestRiskForTableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		review *domain.Review
		want   string
	}{
		{
			name:   "nil review",
			review: nil,
			want:   "No review",
		},
		{
			name: "pending, not run",
			review: &domain.Review{
				Status:         domain.ReviewPending,
				FindingsStatus: domain.FindingsNotRun,
			},
			want: "Not Run",
		},
		{
			name: "approved, not run, RiskAssessment nil",
			review: &domain.Review{
				Status:         domain.ReviewApproved,
				FindingsStatus: domain.FindingsNotRun,
				RiskAssessment: nil,
			},
			want: "Not Run",
		},
		{
			name: "pending, run, zero findings",
			review: &domain.Review{
				Status:         domain.ReviewPending,
				FindingsStatus: domain.FindingsRun,
				Findings:       []domain.Finding{},
			},
			want: "NONE",
		},
		{
			name: "approved, run, zero findings, RA{low}",
			review: &domain.Review{
				Status:         domain.ReviewApproved,
				FindingsStatus: domain.FindingsRun,
				Findings:       []domain.Finding{},
				RiskAssessment: &domain.RiskAssessment{
					HighestSeverity: domain.SeverityLow,
				},
			},
			want: "NONE",
		},
		{
			name: "rejected, run, zero findings, RA{low}",
			review: &domain.Review{
				Status:         domain.ReviewRejected,
				FindingsStatus: domain.FindingsRun,
				Findings:       []domain.Finding{},
				RiskAssessment: &domain.RiskAssessment{
					HighestSeverity: domain.SeverityLow,
				},
			},
			want: "NONE",
		},
		{
			name: "approved, findings present, RA{high}",
			review: &domain.Review{
				Status:         domain.ReviewApproved,
				FindingsStatus: domain.FindingsRun,
				Findings: []domain.Finding{
					{Category: "shell_exec", Summary: "exec"},
				},
				RiskAssessment: &domain.RiskAssessment{
					HighestSeverity: domain.SeverityHigh,
				},
			},
			want: "HIGH",
		},
		{
			name: "pending, run, findings with high",
			review: &domain.Review{
				Status:         domain.ReviewPending,
				FindingsStatus: domain.FindingsRun,
				Findings: []domain.Finding{
					{Category: "shell_exec", Summary: "exec"},
				},
			},
			want: "HIGH",
		},
		{
			name: "pending, run, findings only low",
			review: &domain.Review{
				Status:         domain.ReviewPending,
				FindingsStatus: domain.FindingsRun,
				Findings: []domain.Finding{
					{Category: "version_bump", Summary: "bump"},
				},
			},
			want: "LOW",
		},
	}

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.riskFor(tt.review, lifecycle.ReviewPending)
			if got != tt.want {
				t.Errorf("riskFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHighestSeverityForFindings(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	tests := []struct {
		name     string
		findings []domain.Finding
		want     string
	}{
		{
			name:     "empty findings",
			findings: []domain.Finding{},
			want:     "LOW",
		},
		{
			name:     "single low",
			findings: []domain.Finding{{Category: "version_bump", Summary: "bump"}},
			want:     "LOW",
		},
		{
			name:     "single high",
			findings: []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
			want:     "HIGH",
		},
		{
			name:     "mixed high and low",
			findings: []domain.Finding{{Category: "version_bump", Summary: "bump"}, {Category: "shell_exec", Summary: "exec"}},
			want:     "HIGH",
		},
		{
			name:     "multiple high",
			findings: []domain.Finding{{Category: "shell_exec", Summary: "exec"}, {Category: "url_new", Summary: "new url"}},
			want:     "HIGH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := m.highestSeverityForFindings(tt.findings)
			if got != tt.want {
				t.Errorf("highestSeverityForFindings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHighestSeverityForFindingsUnknownCategory(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// Unknown categories fall back to SeverityLow
	got := m.highestSeverityForFindings([]domain.Finding{
		{Category: "unknown_cat", Summary: "unknown"},
	})

	want := "LOW"
	if got != want {
		t.Errorf("highestSeverityForFindings(unknown) = %q, want %q", got, want)
	}
}

func TestDashboardUpdateRiskAfterReview(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// Simulate a terminal review arriving
	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityHigh,
		},
	}

	msg := ReviewDataMsg{
		Pkg:      "test-pkg",
		Status:   review.Status,
		Has:      true,
		Review:   review,
		Updates:  0,
		IsMerged: false,
	}

	m.lifecycle["test-pkg"] = lifecycle.Approved
	m.reviewInputs["test-pkg"] = reviewInputs{
		Status:    review.Status,
		HasReview: true,
	}
	m.reviewsByPkg["test-pkg"] = review
	m.updates["test-pkg"] = 0

	got := m.riskFor(msg.Review, m.lifecycle[msg.Pkg])
	want := "HIGH"
	if got != want {
		t.Errorf("riskFor after ReviewDataMsg = %q, want %q", got, want)
	}
}

func TestRiskForMerged(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityHigh,
		},
	}

	got := m.riskFor(review, lifecycle.Merged)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(merged, risk=HIGH) = %q, want %q", got, want)
	}
}

func TestRiskForBuilt(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "version_bump", Summary: "bump"}},
		// version_bump maps to LOW, but the review's frozen RA says MEDIUM.
		// Row 4 (decided + RA!=nil) takes priority over live finding severity.
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityMedium,
		},
	}

	got := m.riskFor(review, lifecycle.Built)
	want := "MEDIUM"

	if got != want {
		t.Errorf("riskFor(built, risk=MEDIUM) = %q, want %q", got, want)
	}
}

func TestDashboardUpdateFromReviewStoresReview(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityMedium,
		},
	}

	m, _ = m.UpdateFromReview(context.TODO(), "test-pkg", review)

	stored, ok := m.reviewsByPkg["test-pkg"]
	if !ok {
		t.Fatal("review not stored in reviewsByPkg")
	}
	if stored == nil {
		t.Fatal("stored review is nil")
	}
	if stored.RiskAssessment == nil {
		t.Fatal("stored RiskAssessment is nil")
	}
	if stored.RiskAssessment.HighestSeverity != domain.SeverityMedium {
		t.Errorf("HighestSeverity = %s, want %s", stored.RiskAssessment.HighestSeverity, domain.SeverityMedium)
	}
}

func TestDashboardReviewDataMsgStoresReview(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityHigh,
		},
	}

	msg := ReviewDataMsg{
		Pkg:      "test-pkg",
		Status:   review.Status,
		Has:      true,
		Review:   review,
		Updates:  0,
		IsMerged: false,
	}

	m, _ = m.Update(msg)

	stored, ok := m.reviewsByPkg["test-pkg"]
	if !ok {
		t.Fatal("review not stored in reviewsByPkg")
	}
	if stored == nil {
		t.Fatal("stored review is nil")
	}
}

func TestDashboardReviewDataMsgClearsReviewOnNil(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// First, store a review
	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityHigh,
		},
	}

	msgWithReview := ReviewDataMsg{
		Pkg:    "test-pkg",
		Status: review.Status,
		Has:    true,
		Review: review,
	}

	m.Update(msgWithReview)

	if _, ok := m.reviewsByPkg["test-pkg"]; !ok {
		t.Fatal("review not stored initially")
	}

	// Now, simulate a nil review (error case)
	msgNoReview := ReviewDataMsg{
		Pkg:    "test-pkg",
		Has:    false,
		Review: nil,
	}

	m.Update(msgNoReview)

	if _, ok := m.reviewsByPkg["test-pkg"]; ok {
		t.Fatal("review should have been cleared from reviewsByPkg")
	}
}

func TestRiskForTerminalStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    domain.ReviewStatus
		lifecycle lifecycle.State
	}{
		{
			name:      "approved",
			status:    domain.ReviewApproved,
			lifecycle: lifecycle.Approved,
		},
		{
			name:      "rejected",
			status:    domain.ReviewRejected,
			lifecycle: lifecycle.Rejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPolicyEngine(t)
			m := DashboardModel{
				deps: deps.Dependencies{
					Policy: p,
				},
			}

			review := &domain.Review{
				Status:         tt.status,
				FindingsStatus: domain.FindingsRun,
				Findings:       []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
				RiskAssessment: &domain.RiskAssessment{
					HighestSeverity: domain.SeverityHigh,
				},
			}

			got := m.riskFor(review, tt.lifecycle)
			want := "HIGH"

			if got != want {
				t.Errorf("riskFor(%s) = %q, want %q", tt.name, got, want)
			}
		})
	}
}

func TestHighestSeverityForFindingsMixedHighMediumLow(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// shell_exec = HIGH, url_new = HIGH, version_bump = LOW
	got := m.highestSeverityForFindings([]domain.Finding{
		{Category: "version_bump", Summary: "bump"},
		{Category: "shell_exec", Summary: "exec"},
		{Category: "url_new", Summary: "new url"},
	})

	want := "HIGH"
	if got != want {
		t.Errorf("highestSeverityForFindings(mixed) = %q, want %q", got, want)
	}
}

func TestHighestSeverityForFindingsMediumOnly(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// shell_exec = HIGH, checksum_refresh = LOW
	got := m.highestSeverityForFindings([]domain.Finding{
		{Category: "checksum_refresh", Summary: "refresh"},
	})

	want := "LOW"
	if got != want {
		t.Errorf("highestSeverityForFindings(checksum_refresh) = %q, want %q", got, want)
	}
}

func TestRiskForNilPolicyEngine(t *testing.T) {
	t.Parallel()

	m := DashboardModel{
		deps: deps.Dependencies{},
	}

	// nil review + nil policy should not panic
	got := m.riskFor(nil, lifecycle.NotReviewed)
	want := "No review"

	if got != want {
		t.Errorf("riskFor(nil, nil policy) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunNilFindingsSlice(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings:       nil, // nil slice, not empty
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "NONE"

	if got != want {
		t.Errorf("riskFor(pending, run, nil findings) = %q, want %q", got, want)
	}
}

func TestHighestSeverityForFindingsNilSlice(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	got := m.highestSeverityForFindings(nil)
	want := "LOW"

	if got != want {
		t.Errorf("highestSeverityForFindings(nil) = %q, want %q", got, want)
	}
}

func TestRiskForPendingRunMixedHighMediumLow(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "version_bump", Summary: "bump"},
			{Category: "url_new", Summary: "new url"},
			{Category: "checksum_refresh", Summary: "refresh"},
		},
	}

	got := m.riskFor(review, lifecycle.ReviewPending)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(pending, run, mixed) = %q, want %q", got, want)
	}
}

func TestDashboardReviewDataMsgTerminalReviewStored(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewRejected,
		FindingsStatus: domain.FindingsRun,
		RiskAssessment: &domain.RiskAssessment{
			HighestSeverity: domain.SeverityLow,
		},
	}

	msg := ReviewDataMsg{
		Pkg:      "test-pkg",
		Status:   review.Status,
		Has:      true,
		Review:   review,
		Updates:  0,
		IsMerged: false,
	}

	m, _ = m.Update(msg)

	stored, ok := m.reviewsByPkg["test-pkg"]
	if !ok {
		t.Fatal("terminal review not stored in reviewsByPkg")
	}
	if stored.Status != domain.ReviewRejected {
		t.Errorf("stored Status = %s, want %s", stored.Status, domain.ReviewRejected)
	}
	if stored.RiskAssessment.HighestSeverity != domain.SeverityLow {
		t.Errorf("stored HighestSeverity = %s, want %s", stored.RiskAssessment.HighestSeverity, domain.SeverityLow)
	}
}

func TestRiskForApprovedWithNilRiskAssessment(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{{Category: "shell_exec", Summary: "exec"}},
		RiskAssessment: nil, // explicitly nil
	}

	// Should fall back to findings
	got := m.riskFor(review, lifecycle.Approved)
	want := "HIGH"

	if got != want {
		t.Errorf("riskFor(approved, nil RA, shell_exec finding) = %q, want %q", got, want)
	}
}

func TestHighestSeverityForFindingsSingleLow(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	m := DashboardModel{
		deps: deps.Dependencies{
			Policy: p,
		},
	}

	// version_bump maps to LOW
	got := m.highestSeverityForFindings([]domain.Finding{
		{Category: "version_bump", Summary: "bump"},
	})

	want := "LOW"
	if got != want {
		t.Errorf("highestSeverityForFindings(version_bump) = %q, want %q", got, want)
	}
}

func TestRefreshRetriesWhenPackagesEmpty(t *testing.T) {
	t.Parallel()

	m := DashboardModel{keys: newDashboardKeys()}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if cmd == nil {
		t.Fatal("cmd = nil, want the reload cmd — R should retry even when the package list is empty")
	}
	if !updated.loading {
		t.Fatal("loading = false, want true — an empty-list refresh should show the loading state")
	}
}

func TestRefreshDoesNotShowLoadingWhenPackagesNonEmpty(t *testing.T) {
	t.Parallel()

	m := DashboardModel{
		keys:     newDashboardKeys(),
		packages: []domain.PackageName{"pkg-a"},
		loading:  false,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if cmd == nil {
		t.Fatal("cmd = nil, want the reload cmd — refresh of a non-empty list must still work")
	}
	if updated.loading {
		t.Fatal("loading = true, want false — refreshing an already-populated dashboard must keep its existing no-flicker, update-in-place behavior")
	}
}

// pageSizeTestModel returns a fully-loaded DashboardModel with n packages.
// withCardFilter selects which buildEntries branch reconstructs pkgList:
// the card-predicate path (NewDashboardModel's default, two NewLifecycleList
// calls) or the toolbar string-filter path (one).
func pageSizeTestModel(n int, withCardFilter bool) DashboardModel {
	m := NewDashboardModel(deps.Dependencies{})
	m.loading = false
	m.allData = true
	m.lifecycle = make(map[domain.PackageName]lifecycle.State)
	for i := 0; i < n; i++ {
		pkg := domain.PackageName(fmt.Sprintf("pkg-%02d", i))
		m.packages = append(m.packages, pkg)
		m.lifecycle[pkg] = lifecycle.ReviewPending
	}
	if !withCardFilter {
		m.cardFilter = nil
	}
	m.buildSummary()
	m.buildCards()
	return m
}

// TestBuildEntriesPreservesPageSizeAfterResize is the regression test for
// #290: buildEntries reconstructs pkgList via NewLifecycleList, which resets
// page size to the constructor default. A data reload after a resize must
// keep the height-derived page size rather than waiting for the next
// WindowSizeMsg.
func TestBuildEntriesPreservesPageSizeAfterResize(t *testing.T) {
	t.Parallel()

	const height = 60

	for _, tc := range []struct {
		name           string
		withCardFilter bool
	}{
		{"card filter path", true},
		{"string filter path", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := pageSizeTestModel(10, tc.withCardFilter)
			want := m.listPageSize(height)

			// Guard against a vacuous pass: if the height-derived size
			// equals the constructor default, the bug is invisible.
			// Read the default from the constructor rather than hardcoding it,
			// so this guard can't go stale if the default changes.
			if def := widgets.NewLifecycleList(nil).PageSize(); want == def {
				t.Fatalf("listPageSize(%d) = %d, same as the constructor default; "+
					"pick a height that yields a different page size", height, def)
			}

			m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: height})
			m.buildEntries(context.Background(), "test")

			if got := m.pkgList.PageSize(); got != want {
				t.Errorf("PageSize() after resize then reload = %d, want %d (listPageSize(%d))",
					got, want, height)
			}
		})
	}
}

// TestBuildEntriesBeforeFirstResize covers a data load completing before
// any WindowSizeMsg has arrived (lastHeight == 0): the height is unknown,
// not small, so the page size is FlexHeight's floor (#450), never zero,
// negative or the short-terminal floor of one row.
func TestBuildEntriesBeforeFirstResize(t *testing.T) {
	t.Parallel()

	m := pageSizeTestModel(10, true)
	if m.lastHeight != 0 {
		t.Fatalf("precondition: lastHeight = %d, want 0", m.lastHeight)
	}

	m.buildEntries(context.Background(), "test")

	if got := m.pkgList.PageSize(); got != widgets.MinFlexHeight {
		t.Errorf("PageSize() before first resize = %d, want widgets.MinFlexHeight (%d)",
			got, widgets.MinFlexHeight)
	}
	if m.TooSmall() {
		t.Error("TooSmall() before the first resize: the height is not known yet")
	}
}

// TestDashboardQuitKeyRequestsQuit verifies that the 'q' key on the
// Dashboard emits msgs.QuitRequestedMsg (not tea.Quit) so that the
// App-level active-operation guard can intercept it (#300).
func TestDashboardQuitKeyRequestsQuit(t *testing.T) {
	t.Parallel()

	m := pageSizeTestModel(10, true)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected Cmd from 'q' key")
	}
	msg := cmd()
	if _, ok := msg.(msgs.QuitRequestedMsg); !ok {
		t.Fatalf("expected msgs.QuitRequestedMsg, got %T", msg)
	}
}

func TestDashboardReviewDataMsgHeadCommitMismatchCausesMerged(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)
	store := &mockBuildStore{
		lastSuccess: map[domain.PackageName]*domain.Build{
			"test-pkg": {
				ID:             "ls-1",
				Status:         domain.BuildSuccess,
				Confirmed:      true,
				Published:      true,
				ReviewedCommit: "A",
			},
		},
	}
	engine := build.NewEngine(&mockBuilder{}, store, &mockArtStore{}, &mockGitProvider{}, nil)

	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Policy:      p,
			BuildEngine: engine,
		},
	}

	review := &domain.Review{
		Status:         domain.ReviewApproved,
		FindingsStatus: domain.FindingsRun,
		UpstreamCommit: "a",
	}

	// HeadCommit mismatch ("B" vs stored "A") → Merged
	msg := ReviewDataMsg{
		Pkg:        "test-pkg",
		Status:     review.Status,
		Has:        true,
		IsMerged:   true,
		Review:     review,
		HeadCommit: "B",
	}
	m, _ = m.Update(msg)

	if st := m.lifecycle["test-pkg"]; st != lifecycle.Merged {
		t.Errorf("HeadCommit mismatch → state = %q, want %q", st, lifecycle.Merged)
	}

	// HeadCommit match ("A" vs stored "A") → Published
	msg.HeadCommit = "A"
	m, _ = m.Update(msg)

	if st := m.lifecycle["test-pkg"]; st != lifecycle.Published {
		t.Errorf("HeadCommit match → state = %q, want %q", st, lifecycle.Published)
	}
}
