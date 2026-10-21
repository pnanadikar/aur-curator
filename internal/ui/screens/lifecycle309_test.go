package screens

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
)

// Tests for #309: lifecycle derives from the last success, never from the
// latest attempt; Confirmed is a distinct state; the Published column and
// next actions follow from it (workflow.md § State Transitions and Action
// Availability).

// headGit is a git provider whose local HEAD is fixed.
type headGit struct {
	mockGitProvider
	head string
}

func (g *headGit) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return g.head, nil
}

// publishedAtA is a last success at commit A that is confirmed and published.
func publishedAtA() *domain.Build {
	return &domain.Build{ID: "b1", Status: domain.BuildSuccess, Confirmed: true, Published: true, ReviewedCommit: "A"}
}

// deriveViaDashboard delivers a ReviewDataMsg for an approved, merged
// package and returns the resulting lifecycle state.
func deriveViaDashboard(t *testing.T, store *mockBuildStore, head string) lifecycle.State {
	t.Helper()
	engine := build.NewEngine(&mockBuilder{}, store, &mockArtStore{}, &mockGitProvider{}, nil)
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps:         deps.Dependencies{Policy: testPolicyEngine(t), BuildEngine: engine},
	}
	review := &domain.Review{Status: domain.ReviewApproved, FindingsStatus: domain.FindingsRun}
	m, _ = m.Update(ReviewDataMsg{
		Pkg: "test-pkg", Status: review.Status, Has: true, IsMerged: true,
		Review: review, HeadCommit: head,
	})
	return m.lifecycle["test-pkg"]
}

// TestRebuildDoesNotDemotePublished is the #309 core case: a same-commit
// rebuild that failed, or is still running, leaves the package Published.
func TestRebuildDoesNotDemotePublished(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.BuildStatus{domain.BuildFailed, domain.BuildRunning} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			store := &mockBuildStore{
				records:     map[domain.PackageName]*domain.Build{"test-pkg": {ID: "b2", Status: status, ReviewedCommit: "A"}},
				lastSuccess: map[domain.PackageName]*domain.Build{"test-pkg": publishedAtA()},
			}
			if got := deriveViaDashboard(t, store, "A"); got != lifecycle.Published {
				t.Errorf("latest attempt %s → state %q, want %q", status, got, lifecycle.Published)
			}
		})
	}
}

// TestNewCommitWithPublishedOlderBuild: the last success is for commit A,
// HEAD has moved to B → Merged, and the column says "Older".
func TestNewCommitWithPublishedOlderBuild(t *testing.T) {
	t.Parallel()

	store := &mockBuildStore{
		records:     map[domain.PackageName]*domain.Build{},
		lastSuccess: map[domain.PackageName]*domain.Build{"test-pkg": publishedAtA()},
	}
	if got := deriveViaDashboard(t, store, "B"); got != lifecycle.Merged {
		t.Errorf("state = %q, want %q", got, lifecycle.Merged)
	}
	m := DashboardModel{}
	if got := m.formatConfirmedPublished(publishedAtA(), "B"); got != "Older" {
		t.Errorf("Published column = %q, want %q", got, "Older")
	}
}

// TestFailedCardCountsFailedRebuildOfPublished: the Failed card reads the
// latest attempt, so a failed rebuild of a still-Published package counts.
func TestFailedCardCountsFailedRebuildOfPublished(t *testing.T) {
	t.Parallel()

	store := &mockBuildStore{
		records: map[domain.PackageName]*domain.Build{
			"rebuilt": {ID: "b2", Status: domain.BuildFailed, ReviewedCommit: "A"},
			"healthy": {ID: "b3", Status: domain.BuildSuccess, ReviewedCommit: "A"},
		},
		lastSuccess: map[domain.PackageName]*domain.Build{
			"rebuilt": publishedAtA(),
			"healthy": {ID: "b3", Status: domain.BuildSuccess, Confirmed: true, Published: true, ReviewedCommit: "A"},
		},
	}
	engine := build.NewEngine(&mockBuilder{}, store, &mockArtStore{}, &mockGitProvider{}, nil)
	m := NewDashboardModel(deps.Dependencies{BuildEngine: engine})
	m.packages = []domain.PackageName{"rebuilt", "healthy"}
	m.lifecycle = map[domain.PackageName]lifecycle.State{"rebuilt": lifecycle.Published, "healthy": lifecycle.Published}

	m.summary = m.buildSummary()
	if m.summary[CardFailed].Count != 1 {
		t.Errorf("CardFailed count = %d, want 1 (only the failed rebuild)", m.summary[CardFailed].Count)
	}
	if m.summary[CardConfirmed].Count != 0 {
		t.Errorf("CardConfirmed count = %d, want 0 (both are Published)", m.summary[CardConfirmed].Count)
	}
}

func TestFormatConfirmedPublished(t *testing.T) {
	t.Parallel()

	m := DashboardModel{}
	tests := []struct {
		name string
		ls   *domain.Build
		head string
		want string
	}{
		{"no last success", nil, "A", "No"},
		{"built at head", &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}, "A", "No"},
		{"confirmed at head", &domain.Build{Status: domain.BuildSuccess, Confirmed: true, ReviewedCommit: "A"}, "A", "No"},
		{"published at head", publishedAtA(), "A", "Yes"},
		{"published, head moved", publishedAtA(), "B", "Older"},
		{"published, head unknown", publishedAtA(), "", "Older"},
		{"confirmed, head moved", &domain.Build{Status: domain.BuildSuccess, Confirmed: true, ReviewedCommit: "A"}, "B", "No"},
	}
	for _, tt := range tests {
		if got := m.formatConfirmedPublished(tt.ls, tt.head); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestAvailabilityByState mirrors workflow.md's availability table for the
// actions whose rules #309 changed (Confirm, Publish) and the next action.
func TestAvailabilityByState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state      lifecycle.State
		confirm    bool
		publish    bool
		nextAction string
	}{
		{lifecycle.ReviewPending, false, false, "Review"},
		{lifecycle.Approved, false, false, "Merge"},
		{lifecycle.Rejected, false, false, "—"},
		{lifecycle.Merged, false, false, "Build"},
		{lifecycle.Built, true, false, "Confirm"},
		{lifecycle.Confirmed, false, true, "Publish"},
		{lifecycle.Published, false, false, "—"},
	}
	for _, tt := range tests {
		m := DashboardModel{lifecycle: map[domain.PackageName]lifecycle.State{"p": tt.state}}
		if got := m.isConfirmEnabled("p"); got != tt.confirm {
			t.Errorf("%s: Confirm enabled = %v, want %v", tt.state, got, tt.confirm)
		}
		if got := m.isPublishEnabled("p"); got != tt.publish {
			t.Errorf("%s: Publish enabled = %v, want %v", tt.state, got, tt.publish)
		}
		if got := nextActionFor(tt.state); got != tt.nextAction {
			t.Errorf("%s: next action = %q, want %q", tt.state, got, tt.nextAction)
		}
	}
}

// TestRebuildCheckUsesLastSuccess (R6): with a later failed attempt, the
// rebuild check compares the LAST SUCCESS against HEAD, so pressing Build
// is treated as a rebuild of that build, not as a new build.
func TestRebuildCheckUsesLastSuccess(t *testing.T) {
	t.Parallel()

	store := &mockBuildStore{
		records:     map[domain.PackageName]*domain.Build{"test-pkg": {ID: "b2", Status: domain.BuildFailed, ReviewedCommit: "OLD"}},
		lastSuccess: map[domain.PackageName]*domain.Build{"test-pkg": {ID: "b1", Status: domain.BuildSuccess, ReviewedCommit: "A"}},
	}
	engine := build.NewEngine(&mockBuilder{}, store, &mockArtStore{}, &mockGitProvider{}, nil)
	g := gateAt("test-pkg", buildGateRebuildChecking)
	g.deps = deps.Dependencies{BuildEngine: engine, Git: &headGit{head: "A"}}

	msg, ok := g.rebuildCheckCmd()().(buildGateRebuildMsg)
	if !ok {
		t.Fatalf("expected buildGateRebuildMsg")
	}
	if msg.build == nil || msg.build.ID != "b1" {
		t.Fatalf("rebuild check used %+v, want the last success b1", msg.build)
	}
	if msg.build.ReviewedCommit != msg.headCommit {
		t.Errorf("last success commit %q vs HEAD %q — should be treated as a rebuild", msg.build.ReviewedCommit, msg.headCommit)
	}
}
