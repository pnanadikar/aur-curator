package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// #471 — findings are numbered i/N, right-aligned to the width of N; the
// Location keeps its indent after the number column; tabs in a summary
// are expanded.

// findingLines returns the Findings tab's lines from the first numbered
// finding on (tests render without colour).
func findingLines(t *testing.T, findings []domain.Finding) []string {
	t.Helper()
	model := loadFindings(t, readySized(t), findings)
	lines := strings.Split(model.renderFindingsContent(), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "Findings: ") {
			return lines[i+2:]
		}
	}
	t.Fatalf("no Findings header in:\n%s", strings.Join(lines, "\n"))
	return nil
}

func TestFindingsNumbered471(t *testing.T) {
	for _, n := range []int{1, 9, 10, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			findings := make([]domain.Finding, n)
			for i := range findings {
				findings[i] = domain.Finding{
					Analyzer: "checksum", Category: "version_bump",
					Summary: fmt.Sprintf("change %d", i+1), Location: fmt.Sprintf("PKGBUILD:%d", i+1),
				}
			}
			lines := findingLines(t, findings)
			digits := len(fmt.Sprint(n))
			indent := strings.Repeat(" ", 2*digits+2+7)
			for i := range n {
				wantFinding := fmt.Sprintf("%*d/%d [LOW] checksum: change %d", digits, i+1, n, i+1)
				wantLocation := fmt.Sprintf("%sPKGBUILD:%d", indent, i+1)
				if got := lines[2*i]; got != wantFinding {
					t.Fatalf("finding %d: got %q, want %q", i+1, got, wantFinding)
				}
				if got := lines[2*i+1]; got != wantLocation {
					t.Fatalf("location %d: got %q, want %q", i+1, got, wantLocation)
				}
			}
		})
	}
}

// TestFindingsNumberedInSortedOrder471: numbers follow the displayed
// (severity-sorted) order, and a finding without a Location has no
// Location line.
func TestFindingsNumberedInSortedOrder471(t *testing.T) {
	lines := findingLines(t, []domain.Finding{
		{Analyzer: "checksum", Category: "version_bump", Summary: "pkgver changed", Location: "PKGBUILD:3"},
		{Analyzer: "url", Category: "url_new", Summary: "New source URL: https://a.example/x"},
	})
	want := []string{
		"1/2 [HIGH] url: New source URL: https://a.example/x",
		"2/2 [LOW] checksum: pkgver changed",
		"           PKGBUILD:3",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d: got %q, want %q", i+1, lines[i], w)
		}
	}
}

// TestFindingsTabsExpanded471: a Shell summary holding a PKGBUILD line
// with tabs renders with spaces. (The Location is styled, and lipgloss
// expands its tabs.)
func TestFindingsTabsExpanded471(t *testing.T) {
	lines := findingLines(t, []domain.Finding{
		{Analyzer: "shell", Category: "network_download", Summary: "Network download: \tcurl -s https://a.example/x", Location: "PKGBUILD:\t7"},
	})
	if got, want := lines[0], "1/1 [HIGH] shell: Network download:     curl -s https://a.example/x"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(lines, "\n"), "\t") {
		t.Fatalf("tab left in the Findings tab:\n%s", strings.Join(lines, "\n"))
	}
}
