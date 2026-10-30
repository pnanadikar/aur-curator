package policy

import (
	"encoding/json"

	"github.com/pramod/aur-curator/internal/domain"
)

// Engine resolves Finding severity from category and rolls findings up
// into a frozen RiskAssessment. It owns policy classification only — it
// does not produce findings (interfaces.md § Policy Engine).
type Engine interface {
	Severity(category string) domain.Severity
	Assess(findings []domain.Finding) *domain.RiskAssessment
}

// defaultSeverity is Sprint 5's category → severity taxonomy, per
// docs/sprints/s05-analyzers-policy.md § Category Taxonomy. A category
// introduced by a future analyzer must be added here explicitly — see
// Severity's doc comment for what happens if it isn't.
var defaultSeverity = map[string]domain.Severity{
	"url_new":          domain.SeverityHigh,
	"pgp_removed":      domain.SeverityHigh,
	"checksum_removed": domain.SeverityHigh,
	"shell_exec":       domain.SeverityHigh,
	"network_download": domain.SeverityHigh,
	"checksum_refresh": domain.SeverityLow,
	"version_bump":     domain.SeverityLow,
}

// DefaultEngine is Engine's sole implementation for now, holding the
// active PolicyConfig as a point-in-time snapshot taken at construction.
type DefaultEngine struct {
	version   string
	overrides map[string]domain.Severity
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
func NewEngine(cfg domain.PolicyConfig) *DefaultEngine {
	overrides := map[string]domain.Severity{}
	if len(cfg.Overrides) > 0 {
		var raw map[string]string
		if err := json.Unmarshal(cfg.Overrides, &raw); err == nil {
			for category, sev := range raw {
				overrides[category] = domain.Severity(sev)
			}
		}
	}

	return &DefaultEngine{
		version:   cfg.Version,
		overrides: overrides,
	}
}

// Severity resolves category via the engine's PolicyConfig.Overrides,
// falling back to the default taxonomy table. A category present in
// neither — e.g. a future analyzer's category not yet added to
// defaultSeverity — resolves to SeverityLow rather than panicking or
// erroring: an unclassified Finding should still reach the reviewer
// (Findings are never dropped), just without a severity it has no
// evidence to justify.
func (e *DefaultEngine) Severity(category string) domain.Severity {
	if sev, ok := e.overrides[category]; ok {
		return sev
	}
	if sev, ok := defaultSeverity[category]; ok {
		return sev
	}
	return domain.SeverityLow
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
