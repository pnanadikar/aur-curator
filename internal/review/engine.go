package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/trace"
)

// Engine coordinates the package review workflow. It calls into
// AnalyzerManager and Policy Engine as collaborators — it does not
// contain analyzer or policy logic itself (interfaces.md § Review
// Engine: "does not perform analysis itself").
type Engine struct {
	git       git.Provider
	reviews   domain.ReviewStore
	analyzers analyzer.Manager
	policy    policy.Engine
}

// NewEngine constructs a Review Engine.
func NewEngine(
	gitProvider git.Provider,
	reviewStore domain.ReviewStore,
	analyzers analyzer.Manager,
	policyEngine policy.Engine,
) *Engine {
	return &Engine{
		git:       gitProvider,
		reviews:   reviewStore,
		analyzers: analyzers,
		policy:    policyEngine,
	}
}

// newPendingReview builds a new Pending Review from a live diff. The diff's
// base and upstream commits become the review's stored range (domain.md
// § Reviewed Range, #387); every review is created here.
func (e *Engine) newPendingReview(pkgName domain.PackageName, diff *git.GitDiff) *domain.Review {
	return &domain.Review{
		ID:             domain.ReviewID(fmt.Sprintf("%s@%s", pkgName, diff.UpstreamCommit)),
		PackageName:    pkgName,
		UpstreamCommit: diff.UpstreamCommit,
		BaseCommit:     diff.BaseCommit,
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsNotRun,
		Findings:       nil,
	}
}

// EnsureReview returns the current Review for pkgName, creating a new
// Pending review only if none exists yet. Unlike InitiateReview, it
// never overwrites an existing Review — Pending or terminal — even if
// the upstream commit has since changed. Intended for read/entry paths
// (e.g. opening the Review screen) that should never discard an
// existing decision's notes/findings/rationale as a side effect of
// being viewed.
func (e *Engine) EnsureReview(ctx context.Context, pkgName domain.PackageName) (*domain.Review, error) {
	current, err := e.reviews.Get(pkgName)
	switch {
	case err == nil:
		return current, nil
	case errors.Is(err, domain.ErrNotFound):
		// create below
	default:
		return nil, err
	}

	diff, err := e.git.Diff(ctx, pkgName)
	if err != nil {
		return nil, err
	}

	r := e.newPendingReview(pkgName, diff)

	if err := e.reviews.Save(pkgName, r); err != nil {
		return nil, err
	}

	return r, nil
}

// InitiateReview returns the current Review for pkgName or creates a new
// Pending review when the upstream commit has changed — overwriting an
// existing Review, Pending or terminal, per domain.md § Review Scope:
// Current, Not Historical. Intended exclusively for the explicit Fetch
// action; do not call this from a passive view/entry path — see
// EnsureReview for that.
func (e *Engine) InitiateReview(ctx context.Context, pkgName domain.PackageName) (*domain.Review, error) {
	diff, err := e.git.Diff(ctx, pkgName)
	if err != nil {
		return nil, err
	}

	current, err := e.reviews.Get(pkgName)
	switch {
	case err == nil:
		if current.UpstreamCommit == diff.UpstreamCommit {
			return current, nil
		}

	case errors.Is(err, domain.ErrNotFound):
		// create below

	default:
		return nil, err
	}

	r := e.newPendingReview(pkgName, diff)

	if err := e.reviews.Save(pkgName, r); err != nil {
		return nil, err
	}

	return r, nil
}

// RunAnalyzers runs the configured analyzers against the current review's
// stored range (BaseCommit → UpstreamCommit, #387) — the same diff the
// Review screen shows — and persists the results on the review: Findings
// replaced wholesale, FindingsStatus set to
// FindingsRun. It does not compute or attach a RiskAssessment — that
// happens only inside RecordDecision, and only when FindingsStatus is
// FindingsRun (see RecordDecision below).
//
// Fresh Finding IDs are assigned on every call, even a re-run over an
// unchanged diff: Findings are fully replaced each time, so there is no
// prior ID to preserve and no cross-run identity requirement.
func (e *Engine) RunAnalyzers(ctx context.Context, pkgName domain.PackageName) (*domain.Review, error) {
	r, err := e.reviews.Get(pkgName)
	if err != nil {
		return nil, err
	}

	if r.Status != domain.ReviewPending {
		return nil, fmt.Errorf("cannot run analyzers on a review that is already decided")
	}

	diff, err := e.git.ReviewDiff(ctx, pkgName, r.BaseCommit, r.UpstreamCommit)
	if err != nil {
		return nil, err
	}

	findings := e.analyzers.Run(ctx, diff)
	for i := range findings {
		findings[i].ReviewID = r.ID
		findings[i].ID = domain.NewFindingID()
	}

	r.Findings = findings
	r.FindingsStatus = domain.FindingsRun

	if err := e.reviews.Save(pkgName, r); err != nil {
		return nil, err
	}

	return r, nil
}

// RecordDecision records an approval or rejection for the current
// review. When FindingsStatus is FindingsRun, this is also the sole
// point where a RiskAssessment is computed and frozen — never inside
// RunAnalyzers, never recomputed afterward even if PolicyConfig changes
// later (docs/sprints/s05-analyzers-policy.md § RiskAssessment Timing).
// For FindingsNotRun (the Approve-without-analysis path), RiskAssessment
// stays nil — there is nothing to assess, and computing one anyway would
// misrepresent "never analyzed" as "analyzed, found nothing", the exact
// collapse FindingsStatus exists to prevent.
func (e *Engine) RecordDecision(
	ctx context.Context,
	pkgName domain.PackageName,
	outcome domain.ReviewStatus,
	rationale string,
	notes string,
) (*domain.Review, error) {
	// ctx is unused: Policy Engine's Assess is a pure, synchronous
	// computation with no I/O, so nothing here needs it. Kept in the
	// signature for consistency with the Engine's other methods
	// (InitiateReview, RunAnalyzers) and any future async need.

	if outcome != domain.ReviewApproved &&
		outcome != domain.ReviewRejected {
		return nil, fmt.Errorf("invalid review outcome %q", outcome)
	}

	r, err := e.reviews.Get(pkgName)
	if err != nil {
		return nil, err
	}

	if r.Status != domain.ReviewPending {
		return nil, fmt.Errorf("review already decided")
	}

	r.Status = outcome
	r.DecisionTime = time.Now().UTC()
	r.Rationale = rationale
	r.ReviewerNotes = notes

	if r.FindingsStatus == domain.FindingsRun {
		ra := e.policy.Assess(r.Findings)
		ra.ID = domain.NewRiskAssessmentID()
		ra.ReviewID = r.ID
		r.RiskAssessment = ra
	}

	if err := e.reviews.Save(pkgName, r); err != nil {
		return nil, err
	}

	return r, nil
}

// GetReview returns the stored Review for a package without creating
// or modifying anything. Returns domain.ErrNotFound if none exists.
func (e *Engine) GetReview(ctx context.Context, name domain.PackageName) (*domain.Review, error) {
	return e.reviews.Get(name)
}

// Policy returns the Policy Engine used by this Review Engine.
// It is exposed so the UI can derive presentation-only severity
// from Finding.Category without duplicating policy logic.
func (e *Engine) Policy() policy.Engine {
	return e.policy
}

// FetchResult captures what FetchAndReview observed after fetching.
type FetchResult struct {
	Changed             bool   // true when upstream changed and InitiateReview was called
	Before              string // upstream commit before fetch (empty if not read)
	After               string // upstream commit after fetch (empty if not read)
	EmptyResult         bool   // true when Result should be omitted (beforeErr/afterErr)
	ForceInitiateCalled bool   // true when ForceInitiateReview was set and InitiateReview was called
}

// FetchAndReview runs a git fetch for pkg and, if the upstream commit
// changed, initiates a new Pending review. It returns a FetchResult
// describing what happened and an error only when the fetch itself
// failed.
//
// If ForceInitiateReview is true and reviews is non-nil, InitiateReview
// is called unconditionally regardless of whether upstream moved. This
// is intended for callers (e.g. the update command's bucket-2 confirmed
// path) that have already determined the review is stale by an
// independent check and want InitiateReview to fire even when the local
// ref already points at the latest upstream commit.
//
// If reviews is nil the review-initiation step is skipped.
//
// This function is the shared core of the Fetch operation, callable
// from both the TUI (app.go:OpFetch) and the update CLI command.
func FetchAndReview(ctx context.Context, g git.Provider, reviews *Engine, pkg domain.PackageName, forceInitiate bool) (FetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	before, beforeErr := g.UpstreamCommit(ctx, pkg)
	if err := g.Fetch(ctx, pkg); err != nil {
		return FetchResult{}, err
	}

	after, afterErr := g.UpstreamCommit(ctx, pkg)

	if beforeErr != nil || afterErr != nil {
		trace.Log(ctx, "handoff", "to", "store", "from", "git", "via", "UpstreamCommit", "pkg", pkg)
		return FetchResult{EmptyResult: true}, nil
	}

	if before == after && !forceInitiate {
		trace.Log(ctx, "handoff", "to", "store", "from", "git", "via", "UpstreamCommit", "pkg", pkg, "result", "Nothing upstream")
		return FetchResult{}, nil
	}

	if reviews != nil {
		_, err := reviews.InitiateReview(ctx, pkg)
		if err != nil {
			return FetchResult{}, err
		}
	}

	if forceInitiate {
		trace.Log(ctx, "handoff", "to", "store", "from", "git+reviews", "via", "Fetch+ForceInitiateReview", "pkg", pkg, "result", fmt.Sprintf("Forced InitiateReview (%s -> %s)", before, after))
		return FetchResult{ForceInitiateCalled: true, Before: before, After: after}, nil
	}

	trace.Log(ctx, "handoff", "to", "store", "from", "git+reviews", "via", "Fetch+InitiateReview", "pkg", pkg, "result", fmt.Sprintf("Upstream changed (%s -> %s)", before, after))
	return FetchResult{Changed: true, Before: before, After: after}, nil
}
