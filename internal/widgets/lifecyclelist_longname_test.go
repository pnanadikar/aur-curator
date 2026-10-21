package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
)

// #325 — names longer than the 24-cell name column used to panic
// (negative strings.Repeat count) and, once truncated, were one cell too
// wide.

func nameRow(t *testing.T, name string) string {
	t.Helper()
	e := LifecycleEntry{Name: domain.PackageName(name), Lifecycle: lifecycle.Merged, NextAction: "Build"}
	return strings.SplitN(NewLifecycleList([]LifecycleEntry{e}).Render(), "\n", 2)[0]
}

func TestLifecycleListNameColumnLongNames(t *testing.T) {
	exact := strings.Repeat("a", 24)
	cases := []struct {
		name      string
		pkg       string
		wantShown string // the name as it should appear in the row
	}{
		{"short", "yay", "yay"},
		{"exactly 24", exact, exact},
		{"25 chars", "microsoft-edge-stable-bin", "microsoft-edge-stable-b…"},
		{"30 chars", strings.Repeat("b", 30), strings.Repeat("b", 23) + "…"},
	}
	var widths []int
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := nameRow(t, tc.pkg) // must not panic
			if !strings.Contains(row, tc.wantShown) {
				t.Fatalf("row %q does not show %q", row, tc.wantShown)
			}
			if len(tc.pkg) <= 24 && strings.Contains(row, "…") {
				t.Fatalf("name of %d chars must not be truncated: %q", len(tc.pkg), row)
			}
			widths = append(widths, lipgloss.Width(row))
		})
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Fatalf("row widths differ (columns misaligned): %v (case %d)", widths, i)
		}
	}
}
