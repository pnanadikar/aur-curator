package policy

import (
	"encoding/json"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestSeverity_Default(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})

	cases := map[string]domain.Severity{
		"url_new":          domain.SeverityHigh,
		"pgp_removed":      domain.SeverityHigh,
		"checksum_removed": domain.SeverityHigh,
		"shell_exec":       domain.SeverityHigh,
		"network_download": domain.SeverityHigh,
		"checksum_refresh": domain.SeverityLow,
		"version_bump":     domain.SeverityLow,
		"some_future_category_nobody_has_classified_yet": domain.SeverityLow,
	}

	for category, want := range cases {
		if got := e.Severity(category); got != want {
			t.Errorf("Severity(%q) = %q, want %q", category, got, want)
		}
	}
}

func TestSeverity_Override(t *testing.T) {
	overrides, _ := json.Marshal(map[string]string{"url_new": "low"})
	e := NewEngine(domain.PolicyConfig{Version: "v1", Overrides: overrides})

	if got := e.Severity("url_new"); got != domain.SeverityLow {
		t.Errorf("Severity(url_new) = %q, want low (overridden)", got)
	}
	// Unrelated category still resolves via the default table.
	if got := e.Severity("checksum_removed"); got != domain.SeverityHigh {
		t.Errorf("Severity(checksum_removed) = %q, want high (unaffected by override)", got)
	}
}

func TestSeverity_MalformedOverridesDoesNotPanic(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1", Overrides: json.RawMessage(`not valid json`)})

	if got := e.Severity("url_new"); got != domain.SeverityHigh {
		t.Errorf("Severity(url_new) = %q, want high (malformed overrides ignored, default applies)", got)
	}
}

func TestAssess_Empty(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})

	ra := e.Assess(nil)

	if ra.HighestSeverity != domain.SeverityLow {
		t.Errorf("HighestSeverity = %q, want low", ra.HighestSeverity)
	}
	if len(ra.CountBySeverity) != 0 {
		t.Errorf("CountBySeverity = %v, want empty", ra.CountBySeverity)
	}
	if ra.PolicyVersion != "v1" {
		t.Errorf("PolicyVersion = %q, want v1", ra.PolicyVersion)
	}
}

func TestAssess_MixedSeverities(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})

	findings := []domain.Finding{
		{Category: "checksum_refresh"}, // low
		{Category: "version_bump"},     // low
		{Category: "url_new"},          // high
	}

	ra := e.Assess(findings)

	if ra.HighestSeverity != domain.SeverityHigh {
		t.Errorf("HighestSeverity = %q, want high", ra.HighestSeverity)
	}
	if ra.CountBySeverity[domain.SeverityLow] != 2 {
		t.Errorf("CountBySeverity[low] = %d, want 2", ra.CountBySeverity[domain.SeverityLow])
	}
	if ra.CountBySeverity[domain.SeverityHigh] != 1 {
		t.Errorf("CountBySeverity[high] = %d, want 1", ra.CountBySeverity[domain.SeverityHigh])
	}
}

func TestAssess_ReflectsOverrides(t *testing.T) {
	overrides, _ := json.Marshal(map[string]string{"url_new": "medium"})
	e := NewEngine(domain.PolicyConfig{Version: "v2", Overrides: overrides})

	findings := []domain.Finding{
		{Category: "url_new"}, // overridden: high -> medium
	}

	ra := e.Assess(findings)

	if ra.HighestSeverity != domain.SeverityMedium {
		t.Errorf("HighestSeverity = %q, want medium (reflecting override, not the default high)", ra.HighestSeverity)
	}
	if ra.CountBySeverity[domain.SeverityHigh] != 0 {
		t.Errorf("CountBySeverity[high] = %d, want 0 — override should move the count out of high entirely", ra.CountBySeverity[domain.SeverityHigh])
	}
}

func TestAssess_PolicyVersionNeverPassedByCaller(t *testing.T) {
	// Two engines, two different configured versions, same findings.
	// PolicyVersion must come from whichever engine ran the assessment,
	// never from an argument to Assess (there isn't one).
	e1 := NewEngine(domain.PolicyConfig{Version: "v1"})
	e2 := NewEngine(domain.PolicyConfig{Version: "v2"})

	findings := []domain.Finding{{Category: "url_new"}}

	if got := e1.Assess(findings).PolicyVersion; got != "v1" {
		t.Errorf("e1 PolicyVersion = %q, want v1", got)
	}
	if got := e2.Assess(findings).PolicyVersion; got != "v2" {
		t.Errorf("e2 PolicyVersion = %q, want v2", got)
	}
}

func TestAssess_LeavesIDAndReviewIDZeroValued(t *testing.T) {
	// Regression guard: Assess must not assign ID/ReviewID itself — that
	// stays the caller's (ReviewEngine.RecordDecision's) responsibility,
	// since Assess never sees the Review these findings belong to.
	e := NewEngine(domain.PolicyConfig{Version: "v1"})

	ra := e.Assess([]domain.Finding{{Category: "url_new"}})

	if ra.ID != "" {
		t.Errorf("ID = %q, want empty — Assess must not assign it", ra.ID)
	}
	if ra.ReviewID != "" {
		t.Errorf("ReviewID = %q, want empty — Assess must not assign it", ra.ReviewID)
	}
}
