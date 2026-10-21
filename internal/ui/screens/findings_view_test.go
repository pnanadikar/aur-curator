package screens

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/policy"
)

func testPolicyEngine(t *testing.T) policy.Engine {
	t.Helper()

	return policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	})
}

func TestFindingsSummaryNotRun(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	review := &domain.Review{
		FindingsStatus: domain.FindingsNotRun,
	}

	got := findingsSummary(p, review)
	want := "Findings: Not Run"

	if got != want {
		t.Fatalf("findingsSummary() = %q, want %q", got, want)
	}
}

func TestFindingsSummaryZeroFindings(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	review := &domain.Review{
		FindingsStatus: domain.FindingsRun,
	}

	got := findingsSummary(p, review)
	want := "Findings: 0 findings"

	if got != want {
		t.Fatalf("findingsSummary() = %q, want %q", got, want)
	}
}

func TestFindingsSummaryCounts(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	review := &domain.Review{
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "shell_exec", Summary: "high 1"},
			{Category: "url_new", Summary: "high 2"},
			{Category: "version_bump", Summary: "low"},
		},
	}

	got := findingsSummary(p, review)
	want := "Findings: 3 (2 HIGH, 1 LOW)"

	if got != want {
		t.Fatalf("findingsSummary() = %q, want %q", got, want)
	}
}

func TestSortedFindingsStableBySeverity(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	in := []domain.Finding{
		{Category: "version_bump", Summary: "low"},
		{Category: "shell_exec", Summary: "high 1"},
		{Category: "url_new", Summary: "high 2"},
	}

	got := sortedFindings(p, in)

	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	want := []string{
		"high 1",
		"high 2",
		"low",
	}

	for i, s := range want {
		if got[i].Summary != s {
			t.Fatalf("finding[%d] = %q, want %q", i, got[i].Summary, s)
		}
	}

	// Verify the input slice was not modified.
	if in[0].Summary != "low" {
		t.Fatal("sortedFindings modified input slice")
	}
}

func TestCountBySeverityEmpty(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	got := countBySeverity(p, nil)

	if got[domain.SeverityHigh] != 0 {
		t.Errorf("High count = %d, want 0", got[domain.SeverityHigh])
	}
	if got[domain.SeverityMedium] != 0 {
		t.Errorf("Medium count = %d, want 0", got[domain.SeverityMedium])
	}
	if got[domain.SeverityLow] != 0 {
		t.Errorf("Low count = %d, want 0", got[domain.SeverityLow])
	}
}

func TestCountBySeverityAllHigh(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	findings := []domain.Finding{
		{Category: "shell_exec", Summary: "high 1"},
		{Category: "url_new", Summary: "high 2"},
	}

	got := countBySeverity(p, findings)

	if got[domain.SeverityHigh] != 2 {
		t.Errorf("High count = %d, want 2", got[domain.SeverityHigh])
	}
	if got[domain.SeverityMedium] != 0 {
		t.Errorf("Medium count = %d, want 0", got[domain.SeverityMedium])
	}
	if got[domain.SeverityLow] != 0 {
		t.Errorf("Low count = %d, want 0", got[domain.SeverityLow])
	}
}

func TestCountBySeverityMixed(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	findings := []domain.Finding{
		{Category: "shell_exec", Summary: "high 1"},
		{Category: "url_new", Summary: "high 2"},
		{Category: "version_bump", Summary: "low"},
		{Category: "network_download", Summary: "high 3"},
	}

	got := countBySeverity(p, findings)

	if got[domain.SeverityHigh] != 3 {
		t.Errorf("High count = %d, want 3", got[domain.SeverityHigh])
	}
	if got[domain.SeverityMedium] != 0 {
		t.Errorf("Medium count = %d, want 0", got[domain.SeverityMedium])
	}
	if got[domain.SeverityLow] != 1 {
		t.Errorf("Low count = %d, want 1", got[domain.SeverityLow])
	}
}

func TestCountBySeverityDoesNotSortInput(t *testing.T) {
	t.Parallel()

	p := testPolicyEngine(t)

	in := []domain.Finding{
		{Category: "version_bump", Summary: "low"},
		{Category: "shell_exec", Summary: "high"},
	}

	countBySeverity(p, in)

	if in[0].Summary != "low" {
		t.Fatal("countBySeverity should not modify the input slice")
	}
}
