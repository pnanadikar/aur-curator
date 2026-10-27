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

// ReviewChangedError is returned by RunAnalyzers and RecordDecision when the
// review stored for a package is not the review the caller is acting on —
// usually because a newer upstream commit replaced it. When it is returned,
// nothing has been run, saved or recorded (#399).
type ReviewChangedError struct {
	Package  domain.PackageName
	Expected domain.ReviewID
	Current  domain.Review // the review now stored
}

func (e *ReviewChangedError) Error() string {
	return fmt.Sprintf("review for %s changed while it was being viewed: expected %s, stored %s",
		e.Package, e.Expected, e.Current.ID)
}

// checkReviewID refuses when the stored review is not the one the caller
// expects (#399).
func checkReviewID(name domain.PackageName, expected domain.ReviewID, stored *domain.Review) error {
	if stored.ID != expected {
		return &ReviewChangedError{Package: name, Expected: expected, Current: *stored}
	}
	return nil
}

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
//
// expected is the ID of the review the caller is acting on; if the stored
// review has a different ID, a *ReviewChangedError is returned and nothing
// is run or recorded.
//
// If any analyzer fails, or ctx is done by the time the analyzers finish,
// an error is returned and nothing is saved: Findings and FindingsStatus
// keep their previous values (#410).
func (e *Engine) RunAnalyzers(ctx context.Context, pkgName domain.PackageName, expected domain.ReviewID) (*domain.Review, error) {
	r, err := e.reviews.Get(pkgName)
	if err != nil {
		return nil, err
	}

	if err := checkReviewID(pkgName, expected, r); err != nil {
		return nil, err
	}

	if r.Status != domain.ReviewPending {
		return nil, fmt.Errorf("cannot run analyzers on a review that is already decided")
	}

	diff, err := e.git.ReviewDiff(ctx, pkgName, r.BaseCommit, r.UpstreamCommit)
	if err != nil {
		return nil, err
	}

	// A failed analyzer or a cancelled run saves nothing: storing what
	// did finish as FindingsRun would show a failed analyzer as one
	// that found nothing (#410, domain.md § FindingsStatus).
	findings, err := e.analyzers.Run(ctx, diff)
	if err != nil {
		return nil, fmt.Errorf("analysis of %s not saved: %w", pkgName, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analysis of %s not saved: %w", pkgName, err)
	}

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
//
// expected is the ID of the review the caller is acting on; if the stored
// review has a different ID, a *ReviewChangedError is returned and nothing
// is run or recorded.
func (e *Engine) RecordDecision(
	ctx context.Context,
	pkgName domain.PackageName,
	outcome domain.ReviewStatus,
	rationale string,
	notes string,
	expected domain.ReviewID,
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

	if err := checkReviewID(pkgName, expected, r); err != nil {
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

// FetchResult captures what FetchAndReview observed and did.
type FetchResult struct {
	Changed       bool   // upstream/master moved during this fetch
	Before        string // upstream commit before the fetch (empty if not read)
	After         string // upstream commit after the fetch (empty if not read)
	EmptyResult   bool   // a commit read failed; no review step ran
	ReviewStarted bool   // a new review replaced the stored one (or none existed)
	ReviewCommit  string // upstream commit of the current review after the review step; empty if no review step ran
}

// FetchAndReview fetches upstream for pkg and, when reviews is non-nil,
// makes sure the stored review covers the current upstream/master: it
// calls InitiateReview, which starts a new Pending review whenever the
// stored review's upstream commit differs from upstream/master (or no
// review exists) and otherwise leaves the stored review unchanged. The
// decision is made from the stored review, not from whether the ref moved
// during this fetch, so it also covers commits fetched earlier — by the
// update command or a git fetch outside aur-curator (#405).
//
// If reviews is nil, no review step runs. It returns an error only when
// the fetch, the review read, or InitiateReview fails.
//
// The whole sequence runs under git.GitTimeout (#409); cancelling ctx
// still stops it at once. An error caused by that timeout says so.
//
// This function is the shared core of the Fetch operation, called from
// the TUI (app.go, OpFetch) and the update command.
func FetchAndReview(ctx context.Context, g git.Provider, reviews *Engine, pkg domain.PackageName) (FetchResult, error) {
	return fetchAndReview(ctx, g, reviews, pkg, git.GitTimeout)
}

// fetchAndReview is FetchAndReview with the timeout as a parameter, so
// tests can use a short one.
func fetchAndReview(ctx context.Context, g git.Provider, reviews *Engine, pkg domain.PackageName, timeout time.Duration) (FetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent := ctx
	ctx, cancel := git.WithTimeout(parent, timeout)
	defer cancel()

	// timedOut labels err when this function's own deadline caused it —
	// not a cancel or a deadline of the caller's ctx. The wording comes
	// from git.Interrupted, the same as every other git timeout (#417);
	// an error the git runner already labelled keeps its label.
	timedOut := func(err error) error {
		if parent.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("fetch %s: %w", pkg, git.Interrupted(ctx, err))
		}
		return err
	}

	before, beforeErr := g.UpstreamCommit(ctx, pkg)
	if err := g.Fetch(ctx, pkg); err != nil {
		return FetchResult{}, timedOut(err)
	}
	after, afterErr := g.UpstreamCommit(ctx, pkg)

	// A commit read that failed because this function's time ran out is
	// a timeout, not an empty result.
	for _, err := range []error{beforeErr, afterErr} {
		if err != nil && parent.Err() == nil && ctx.Err() != nil {
			return FetchResult{}, timedOut(err)
		}
	}

	if beforeErr != nil || afterErr != nil {
		trace.Log(ctx, "handoff", "to", "store", "from", "git", "via", "UpstreamCommit", "pkg", pkg)
		return FetchResult{EmptyResult: true}, nil
	}

	res := FetchResult{Changed: before != after, Before: before, After: after}

	if reviews == nil {
		trace.Log(ctx, "handoff", "to", "store", "from", "git", "via", "Fetch", "pkg", pkg,
			"before", before, "after", after, "result", "no review step")
		return res, nil
	}

	var priorID domain.ReviewID
	stored, err := reviews.GetReview(ctx, pkg)
	switch {
	case err == nil:
		priorID = stored.ID
	case errors.Is(err, domain.ErrNotFound):
		// no review yet; InitiateReview creates one
	default:
		return FetchResult{}, timedOut(err)
	}

	r, err := reviews.InitiateReview(ctx, pkg)
	if err != nil {
		return FetchResult{}, timedOut(err)
	}
	res.ReviewStarted = r.ID != priorID
	res.ReviewCommit = r.UpstreamCommit

	result := "review unchanged"
	if res.ReviewStarted {
		result = "review started"
	}
	trace.Log(ctx, "handoff", "to", "store", "from", "git+reviews", "via", "Fetch", "pkg", pkg,
		"before", before, "after", after, "result", result)
	return res, nil
}
