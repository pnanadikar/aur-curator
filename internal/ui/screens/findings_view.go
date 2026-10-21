package screens

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/policy"
)

// findingsSummary renders the Review screen's tab-bar Findings label per
// tui-review.md § Tab Bar. It must always resolve to exactly one of the
// three states the wireframe defines — "Not Run", "0 findings", or
// "N findings" — never collapsed into one another (see sprint-plan.md
// § Known Gotchas, "FindingsStatus collapse").
func findingsSummary(p policy.Engine, review *domain.Review) string {
	if review == nil || review.FindingsStatus == domain.FindingsNotRun {
		return "Findings: Not Run"
	}

	if len(review.Findings) == 0 {
		return "Findings: 0 findings"
	}

	counts := countBySeverity(p, review.Findings)
	parts := []string{}
	if counts[domain.SeverityHigh] > 0 {
		parts = append(parts, fmt.Sprintf("%d HIGH", counts[domain.SeverityHigh]))
	}
	if counts[domain.SeverityMedium] > 0 {
		parts = append(parts, fmt.Sprintf("%d MEDIUM", counts[domain.SeverityMedium]))
	}
	if counts[domain.SeverityLow] > 0 {
		parts = append(parts, fmt.Sprintf("%d LOW", counts[domain.SeverityLow]))
	}

	return fmt.Sprintf("Findings: %d (%s)",
		len(review.Findings),
		strings.Join(parts, ", "),
	)
}

// countBySeverity counts findings by severity using the policy engine
// to resolve category-to-severity mappings. Callers that need sorted
// output should sort the slice before calling this function.
func countBySeverity(p policy.Engine, findings []domain.Finding) map[domain.Severity]int {
	counts := map[domain.Severity]int{
		domain.SeverityHigh:   0,
		domain.SeverityMedium: 0,
		domain.SeverityLow:    0,
	}
	for _, f := range findings {
		counts[p.Severity(f.Category)]++
	}
	return counts
}

// severityRank orders Findings for display: High, then Medium, then Low.
// Unrecognized severities sort last.
func severityRank(s domain.Severity) int {
	switch s {
	case domain.SeverityHigh:
		return 0
	case domain.SeverityMedium:
		return 1
	case domain.SeverityLow:
		return 2
	default:
		return 3
	}
}

// sortedFindings returns a severity-sorted copy of in. The input slice
// is never mutated.
func sortedFindings(p policy.Engine, in []domain.Finding) []domain.Finding {
	out := append([]domain.Finding(nil), in...)

	sort.SliceStable(out, func(i, j int) bool {
		return severityRank(p.Severity(out[i].Category)) <
			severityRank(p.Severity(out[j].Category))
	})

	return out
}
