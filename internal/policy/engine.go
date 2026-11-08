package policy

import (
	"encoding/json"
	"sync"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/domain"
)

// Engine resolves Finding severity from category and rolls findings up
// into a frozen RiskAssessment. It owns policy classification only — it
// does not produce findings (interfaces.md § Policy Engine).
type Engine interface {
	Severity(category string) domain.Severity
	Assess(findings []domain.Finding) *domain.RiskAssessment
	// Mapped reports whether category has a severity (an override or the
	// default table). An unmapped category is an internal inconsistency
	// the UI reports to the user (#475).
	Mapped(category string) bool
}

// defaultSeverity is the category → severity table
// (docs/domain/risk-analysis.md § Categories). Every analyzer.Categories
// entry must be here; TestEveryAnalyzerCategoryMapped enforces it (#475).
var defaultSeverity = map[string]domain.Severity{
	analyzer.CategoryURLNew:          domain.SeverityHigh,
	analyzer.CategoryPGPRemoved:      domain.SeverityHigh,
	analyzer.CategoryChecksumRemoved: domain.SeverityHigh,
	analyzer.CategoryShellExec:       domain.SeverityHigh,
	analyzer.CategoryNetworkDownload: domain.SeverityHigh,
	analyzer.CategoryArrayUnreadable: domain.SeverityHigh,
	analyzer.CategoryInstallChanged:  domain.SeverityMedium,
	analyzer.CategoryPGPKeyChanged:   domain.SeverityMedium,
	analyzer.CategoryDependsAdded:    domain.SeverityMedium,
	analyzer.CategoryChecksumRefresh: domain.SeverityLow,
	analyzer.CategoryVersionBump:     domain.SeverityLow,
}

// DefaultEngine is Engine's sole implementation for now, holding the
// active PolicyConfig as a point-in-time snapshot taken at construction.
type DefaultEngine struct {
	version   string
	overrides map[string]domain.Severity

	// onUnmapped is called the first time each unmapped category is seen
	// (#475). Severity runs on every render and from operations, so the
	// set of reported categories is guarded by mu.
	onUnmapped func(category string)
	mu         sync.Mutex
	reported   map[string]bool
}

// Option configures a DefaultEngine.
type Option func(*DefaultEngine)

// WithUnmappedHook sets fn to be called once per unmapped category per
// engine (in practice once per run), so the caller can write the error
// log. Without it, an unmapped category is still ranked High (#475).
func WithUnmappedHook(fn func(category string)) Option {
	return func(e *DefaultEngine) { e.onUnmapped = fn }
}

// NewEngine constructs a DefaultEngine from cfg. cfg.Overrides (per
// domain.md § PolicyConfig — a JSON category → severity map)
// is parsed once here, not on every Severity/Assess call: DefaultEngine
// is a snapshot, not a live view. If PolicyConfig changes later, a new
// DefaultEngine must be constructed to see it — no reload/refresh
// mechanism exists this sprint, since Settings wiring itself is still
// only sketched (developer-guide.md § Open Items).
//
// A malformed cfg.Overrides payload does not fail construction — it's
// treated as "no overrides," and the default taxonomy alone applies.
// This mirrors the project's general stance of degrading gracefully on
// malformed optional config rather than blocking startup over it.
func NewEngine(cfg domain.PolicyConfig, opts ...Option) *DefaultEngine {
	overrides := map[string]domain.Severity{}
	if len(cfg.Overrides) > 0 {
		var raw map[string]string
		if err := json.Unmarshal(cfg.Overrides, &raw); err == nil {
			for category, sev := range raw {
				overrides[category] = domain.Severity(sev)
			}
		}
	}

	e := &DefaultEngine{
		version:   cfg.Version,
		overrides: overrides,
		reported:  map[string]bool{},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Severity resolves category via the engine's PolicyConfig.Overrides,
// falling back to the default taxonomy table. A category present in
// neither is an internal inconsistency — a bug, or a finding stored by a
// version with a category this one no longer has — and resolves to
// SeverityHigh, so it is never ranked as routine; the finding is never
// dropped. The unmapped hook, if set, is called the first time each such
// category is seen (#475, risk-analysis.md § Unmapped categories).
func (e *DefaultEngine) Severity(category string) domain.Severity {
	if sev, ok := e.overrides[category]; ok {
		return sev
	}
	if sev, ok := defaultSeverity[category]; ok {
		return sev
	}
	e.reportUnmapped(category)
	return domain.SeverityHigh
}

// Mapped reports whether category has a severity: an override or an
// entry in the default table.
func (e *DefaultEngine) Mapped(category string) bool {
	if _, ok := e.overrides[category]; ok {
		return true
	}
	_, ok := defaultSeverity[category]
	return ok
}

// reportUnmapped calls the unmapped hook once per category.
func (e *DefaultEngine) reportUnmapped(category string) {
	e.mu.Lock()
	first := !e.reported[category]
	e.reported[category] = true
	e.mu.Unlock()
	if first && e.onUnmapped != nil {
		e.onUnmapped(category)
	}
}

// Assess rolls findings up into a RiskAssessment, using the engine's own
// PolicyVersion — callers never pass one in (see
// docs/sprints/s05-analyzers-policy.md § RiskAssessment Timing).
//
// ID and ReviewID are deliberately left zero-valued: Assess only sees
// findings, not the Review they belong to, so the caller
// (ReviewEngine.RecordDecision) is responsible for setting both after
// Assess returns — the same division of responsibility as
// ReviewEngine.RunAnalyzers assigning Finding.ID/ReviewID after
// AnalyzerManager.Run returns. Do not add ID generation here; it would
// duplicate that responsibility in the wrong layer.
//
// RecordDecision must only call Assess when FindingsStatus ==
// FindingsRun. A FindingsNotRun review (the Approve-without-analysis
// path) has no findings to assess at all — calling Assess(nil) would
// still return a structurally valid RiskAssessment{HighestSeverity:
// SeverityLow}, but that would misrepresent "nothing was ever analyzed"
// as "analysis found nothing," the exact collapse FindingsStatus exists
// to prevent. RiskAssessment should stay nil for FindingsNotRun reviews.
func (e *DefaultEngine) Assess(findings []domain.Finding) *domain.RiskAssessment {
	counts := map[domain.Severity]int{}
	highest := domain.SeverityLow

	for _, f := range findings {
		sev := e.Severity(f.Category)
		counts[sev]++
		if severityRank(sev) > severityRank(highest) {
			highest = sev
		}
	}

	return &domain.RiskAssessment{
		HighestSeverity: highest,
		CountBySeverity: counts,
		PolicyVersion:   e.version,
	}
}

// severityRank orders severities for HighestSeverity comparison.
// domain.Severity is a plain string enum with no inherent ordering —
// comparing severities as strings would sort "high" < "low" < "medium"
// alphabetically, silently wrong.
func severityRank(s domain.Severity) int {
	switch s {
	case domain.SeverityHigh:
		return 2
	case domain.SeverityMedium:
		return 1
	default:
		return 0
	}
}
