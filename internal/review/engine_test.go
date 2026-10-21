package review

import (
	"context"
	"errors"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

type fakeGitProvider struct {
	diff *git.GitDiff
	err  error

	// diffCalls counts Diff calls; reviewDiffBase/Upstream record the
	// range of the last ReviewDiff call (#387).
	diffCalls          int
	reviewDiffCalls    int
	reviewDiffBase     string
	reviewDiffUpstream string
}

func (f *fakeGitProvider) Import(context.Context, domain.PackageName) error { return nil }
func (f *fakeGitProvider) Fetch(context.Context, domain.PackageName) error  { return nil }
func (f *fakeGitProvider) Status(context.Context, domain.PackageName) (*git.RepositoryStatus, error) {
	return nil, nil
}
func (f *fakeGitProvider) CommitsBehind(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}
func (f *fakeGitProvider) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	return "", nil
}
func (f *fakeGitProvider) Merge(context.Context, domain.PackageName, string) error { return nil }
func (f *fakeGitProvider) LocalOnlyCommits(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}
func (f *fakeGitProvider) Metadata(context.Context, domain.PackageName) (*git.RepositoryMetadata, error) {
	return nil, nil
}
func (f *fakeGitProvider) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	f.diffCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.diff, nil
}
func (f *fakeGitProvider) ReviewDiff(_ context.Context, _ domain.PackageName, base, upstream string) (*git.GitDiff, error) {
	f.reviewDiffCalls++
	f.reviewDiffBase = base
	f.reviewDiffUpstream = upstream
	if f.err != nil {
		return nil, f.err
	}
	return f.diff, nil
}
func (f *fakeGitProvider) CloneAt(context.Context, domain.PackageName, string, string) error {
	return nil
}
func (f *fakeGitProvider) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return "", nil
}
func (f *fakeGitProvider) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return false, nil
}
func (f *fakeGitProvider) FileAtCommit(context.Context, domain.PackageName, string, string) (string, error) {
	return "", nil
}

type fakeReviewStore struct {
	review  *domain.Review
	err     error
	saveErr error

	saveCalls int
}

func (s *fakeReviewStore) Get(domain.PackageName) (*domain.Review, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.review == nil {
		return nil, domain.ErrNotFound
	}
	r := *s.review
	return &r, nil
}

func (s *fakeReviewStore) Save(_ domain.PackageName, r *domain.Review) error {
	if s.saveErr != nil {
		return s.saveErr
	}

	s.saveCalls++

	c := *r
	s.review = &c
	return nil
}

func (s *fakeReviewStore) Delete(domain.PackageName) error {
	s.review = nil
	return nil
}

// fakeAnalyzerManager satisfies analyzer.Manager structurally — no
// import of the analyzer package needed here, since NewEngine's
// signature (defined in engine.go, same package) already resolves the
// parameter type; Go checks structural satisfaction at the call site.
type fakeAnalyzerManager struct {
	findings []domain.Finding
}

func (f *fakeAnalyzerManager) Run(context.Context, *git.GitDiff) []domain.Finding {
	return f.findings
}

// fakePolicyEngine satisfies policy.Engine the same way.
type fakePolicyEngine struct {
	assessment *domain.RiskAssessment
	calls      int
}

func (f *fakePolicyEngine) Severity(string) domain.Severity {
	return domain.SeverityLow
}

func (f *fakePolicyEngine) Assess(findings []domain.Finding) *domain.RiskAssessment {
	f.calls++
	if f.assessment != nil {
		return f.assessment
	}
	return &domain.RiskAssessment{
		HighestSeverity: domain.SeverityLow,
		PolicyVersion:   "fake-policy-v1",
	}
}

func TestInitiateReviewCreatesWhenMissing(t *testing.T) {
	engine := NewEngine(
		&fakeGitProvider{
			diff: &git.GitDiff{
				UpstreamCommit: "abc123",
			},
		},
		&fakeReviewStore{},
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	r, err := engine.InitiateReview(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if r.Status != domain.ReviewPending {
		t.Fatalf("status=%q", r.Status)
	}

	if r.UpstreamCommit != "abc123" {
		t.Fatalf("commit=%q", r.UpstreamCommit)
	}

	if r.ID != domain.ReviewID("yay@abc123") {
		t.Fatalf("id=%q", r.ID)
	}

	if r.FindingsStatus != domain.FindingsNotRun {
		t.Fatalf("findings=%q", r.FindingsStatus)
	}
}

func TestInitiateReviewReturnsExistingWhenCommitUnchanged(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@abc",
			PackageName:    "yay",
			UpstreamCommit: "abc",
			Status:         domain.ReviewPending,
			ReviewerNotes:  "keep me",
		},
	}

	engine := NewEngine(
		&fakeGitProvider{
			diff: &git.GitDiff{
				UpstreamCommit: "abc",
			},
		},
		store,
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	r, err := engine.InitiateReview(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if store.saveCalls != 0 {
		t.Fatal("Save called unexpectedly")
	}

	if r.ReviewerNotes != "keep me" {
		t.Fatal("review replaced unexpectedly")
	}
}

func TestInitiateReviewReplacesWhenCommitChanges(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "old",
			PackageName:    "yay",
			UpstreamCommit: "oldcommit",
			Status:         domain.ReviewApproved,
			ReviewerNotes:  "old notes",
			Rationale:      "old rationale",
		},
	}

	engine := NewEngine(
		&fakeGitProvider{
			diff: &git.GitDiff{
				UpstreamCommit: "newcommit",
			},
		},
		store,
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	r, err := engine.InitiateReview(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if store.saveCalls != 1 {
		t.Fatal("expected Save")
	}

	if r.Status != domain.ReviewPending {
		t.Fatal("review not reset")
	}

	if r.ReviewerNotes != "" {
		t.Fatal("notes not cleared")
	}

	if r.Rationale != "" {
		t.Fatal("rationale not cleared")
	}

	if r.UpstreamCommit != "newcommit" {
		t.Fatal("commit not updated")
	}
}

func TestRecordDecisionApprove(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:          "r",
			PackageName: "yay",
			Status:      domain.ReviewPending,
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := engine.RecordDecision(
		context.Background(),
		domain.PackageName("yay"),
		domain.ReviewApproved,
		"safe",
		"notes",
	)
	if err != nil {
		t.Fatal(err)
	}

	if r.Status != domain.ReviewApproved {
		t.Fatal("status")
	}

	if r.DecisionTime.IsZero() {
		t.Fatal("decision time")
	}

	if store.saveCalls != 1 {
		t.Fatal("save")
	}
}

func TestRecordDecisionReject(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			Status: domain.ReviewPending,
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := engine.RecordDecision(
		context.Background(),
		domain.PackageName("yay"),
		domain.ReviewRejected,
		"bad",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if r.Status != domain.ReviewRejected {
		t.Fatal("status")
	}
}

func TestRecordDecisionRejectsPendingOutcome(t *testing.T) {
	engine := NewEngine(
		&fakeGitProvider{},
		&fakeReviewStore{
			review: &domain.Review{
				Status: domain.ReviewPending,
			},
		},
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	_, err := engine.RecordDecision(
		context.Background(),
		domain.PackageName("yay"),
		domain.ReviewPending,
		"",
		"",
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRecordDecisionImmutable(t *testing.T) {
	engine := NewEngine(
		&fakeGitProvider{},
		&fakeReviewStore{
			review: &domain.Review{
				Status: domain.ReviewApproved,
			},
		},
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	_, err := engine.RecordDecision(
		context.Background(),
		domain.PackageName("yay"),
		domain.ReviewRejected,
		"",
		"",
	)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInitiateReviewPropagatesGitError(t *testing.T) {
	want := errors.New("git failed")

	engine := NewEngine(
		&fakeGitProvider{err: want},
		&fakeReviewStore{},
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	_, err := engine.InitiateReview(context.Background(), domain.PackageName("yay"))

	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestInitiateReviewPropagatesSaveError(t *testing.T) {
	want := errors.New("save failed")

	engine := NewEngine(
		&fakeGitProvider{
			diff: &git.GitDiff{
				UpstreamCommit: "abc",
			},
		},
		&fakeReviewStore{
			err:     domain.ErrNotFound,
			saveErr: want,
		},
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	_, err := engine.InitiateReview(context.Background(), domain.PackageName("yay"))

	if !errors.Is(err, want) {
		t.Fatalf("got %v want %v", err, want)
	}
}

func TestRunAnalyzersSetsFindingsAndStatus(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:     "yay@abc",
			Status: domain.ReviewPending,
		},
	}
	analyzers := &fakeAnalyzerManager{
		findings: []domain.Finding{
			{Category: "url_new", Summary: "new url"},
		},
	}

	engine := NewEngine(
		&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}},
		store,
		analyzers,
		&fakePolicyEngine{},
	)

	r, err := engine.RunAnalyzers(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if r.FindingsStatus != domain.FindingsRun {
		t.Fatalf("FindingsStatus=%q, want FindingsRun", r.FindingsStatus)
	}
	if len(r.Findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(r.Findings))
	}
	if r.Findings[0].ReviewID != r.ID {
		t.Fatalf("Finding.ReviewID=%q, want %q", r.Findings[0].ReviewID, r.ID)
	}
	if r.Findings[0].ID == "" {
		t.Fatal("Finding.ID is empty, want a generated ID")
	}
	if store.saveCalls != 1 {
		t.Fatal("expected exactly one Save")
	}
}

func TestRunAnalyzersEmptyFindingsStillSetsFindingsRun(t *testing.T) {
	// The tri-state distinction this sprint hinges on: zero findings
	// after a real run must read as FindingsRun, never collapse back to
	// FindingsNotRun.
	store := &fakeReviewStore{
		review: &domain.Review{ID: "yay@abc", Status: domain.ReviewPending},
	}

	engine := NewEngine(
		&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}},
		store,
		&fakeAnalyzerManager{findings: nil},
		&fakePolicyEngine{},
	)

	r, err := engine.RunAnalyzers(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}
	if r.FindingsStatus != domain.FindingsRun {
		t.Fatalf("FindingsStatus=%q, want FindingsRun even with zero findings", r.FindingsStatus)
	}
	if len(r.Findings) != 0 {
		t.Fatalf("got %d findings, want 0", len(r.Findings))
	}
}

func TestRunAnalyzersRejectsAlreadyDecidedReview(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{ID: "yay@abc", Status: domain.ReviewApproved},
	}

	engine := NewEngine(
		&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}},
		store,
		&fakeAnalyzerManager{},
		&fakePolicyEngine{},
	)

	_, err := engine.RunAnalyzers(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error running analyzers on an already-decided review")
	}
	if store.saveCalls != 0 {
		t.Fatal("Save called unexpectedly on rejected RunAnalyzers")
	}
}

func TestRunAnalyzersDoesNotComputeRiskAssessment(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{ID: "yay@abc", Status: domain.ReviewPending},
	}
	policyEngine := &fakePolicyEngine{}

	engine := NewEngine(
		&fakeGitProvider{diff: &git.GitDiff{UpstreamCommit: "abc"}},
		store,
		&fakeAnalyzerManager{findings: []domain.Finding{{Category: "url_new"}}},
		policyEngine,
	)

	r, err := engine.RunAnalyzers(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}
	if r.RiskAssessment != nil {
		t.Fatal("RunAnalyzers must not attach a RiskAssessment — that's RecordDecision's job")
	}
	if policyEngine.calls != 0 {
		t.Fatalf("Policy Engine.Assess called %d times by RunAnalyzers, want 0", policyEngine.calls)
	}
}

func TestRecordDecisionAttachesRiskAssessmentWhenFindingsRun(t *testing.T) {
	assessment := &domain.RiskAssessment{
		HighestSeverity: domain.SeverityHigh,
		PolicyVersion:   "v3",
	}
	policyEngine := &fakePolicyEngine{assessment: assessment}
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@abc",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings:       []domain.Finding{{Category: "url_new"}},
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, policyEngine)

	r, err := engine.RecordDecision(context.Background(), domain.PackageName("yay"), domain.ReviewApproved, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if policyEngine.calls != 1 {
		t.Fatalf("Policy Engine.Assess called %d times, want 1", policyEngine.calls)
	}
	if r.RiskAssessment == nil {
		t.Fatal("RiskAssessment is nil, want it attached")
	}
	if r.RiskAssessment.HighestSeverity != domain.SeverityHigh {
		t.Fatalf("HighestSeverity=%q, want high", r.RiskAssessment.HighestSeverity)
	}
	if r.RiskAssessment.ID == "" {
		t.Fatal("RiskAssessment.ID is empty, want a generated ID")
	}
	if r.RiskAssessment.ReviewID != r.ID {
		t.Fatalf("RiskAssessment.ReviewID=%q, want %q", r.RiskAssessment.ReviewID, r.ID)
	}
}

func TestRecordDecisionLeavesRiskAssessmentNilWhenFindingsNotRun(t *testing.T) {
	// The Approve-without-analysis path: nothing was ever analyzed, so
	// there is nothing to assess. Attaching a RiskAssessment here would
	// misrepresent "never run" as "run, found nothing".
	policyEngine := &fakePolicyEngine{}
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@abc",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, policyEngine)

	r, err := engine.RecordDecision(context.Background(), domain.PackageName("yay"), domain.ReviewApproved, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if policyEngine.calls != 0 {
		t.Fatalf("Policy Engine.Assess called %d times, want 0", policyEngine.calls)
	}
	if r.RiskAssessment != nil {
		t.Fatal("RiskAssessment should stay nil when FindingsStatus is FindingsNotRun")
	}
}

func TestRecordDecisionAttachesRiskAssessmentOnRejectToo(t *testing.T) {
	// RiskAssessment is an audit snapshot of what the reviewer saw,
	// regardless of outcome — not solely an approval artifact.
	policyEngine := &fakePolicyEngine{}
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@abc",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings:       []domain.Finding{{Category: "shell_exec"}},
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, policyEngine)

	r, err := engine.RecordDecision(context.Background(), domain.PackageName("yay"), domain.ReviewRejected, "too risky", "")
	if err != nil {
		t.Fatal(err)
	}

	if policyEngine.calls != 1 {
		t.Fatalf("Policy Engine.Assess called %d times, want 1", policyEngine.calls)
	}
	if r.RiskAssessment == nil {
		t.Fatal("RiskAssessment is nil, want it attached even on Reject")
	}
}

func TestGetReviewReturnsStoredReview(t *testing.T) {
	store := &fakeReviewStore{
		review: &domain.Review{
			ID:             "yay@abc",
			PackageName:    "yay",
			UpstreamCommit: "abc",
			Status:         domain.ReviewApproved,
		},
	}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	r, err := engine.GetReview(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if store.saveCalls != 0 {
		t.Fatal("Save called unexpectedly by GetReview")
	}

	if r.Status != domain.ReviewApproved {
		t.Fatalf("status=%q, want Approved", r.Status)
	}
}

func TestGetReviewReturnsErrNotFoundWhenMissing(t *testing.T) {
	store := &fakeReviewStore{}

	engine := NewEngine(&fakeGitProvider{}, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

	_, err := engine.GetReview(context.Background(), domain.PackageName("nonexistent"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	if store.saveCalls != 0 {
		t.Fatal("Save called unexpectedly by GetReview for missing package")
	}
}
