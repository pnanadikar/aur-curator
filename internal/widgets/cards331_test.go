package widgets

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/lifecycle"
)

// #331 — a card whose count is unknown renders "?", not a number.
func TestCardCountUnknown(t *testing.T) {
	if got := cardCount(Card{Count: 3}); got != "3" {
		t.Fatalf("known count: %q", got)
	}
	if got := cardCount(Card{Count: 0, Unknown: true}); got != "?" {
		t.Fatalf("unknown count: %q, want ?", got)
	}
	var cards []SummaryCard
	cards = append(cards, NewSummaryCard(Card{Label: "AUR Update", Unknown: true}))
	if out := RenderCards(cards); !strings.Contains(out, "AUR Update: ?") {
		t.Fatalf("rendered cards lack %q:\n%s", "AUR Update: ?", out)
	}
}

// #331 — Available values with multi-byte characters ("—", "…", "→")
// keep the column aligned (padding by display width, not bytes).
func TestAvailableColumnAlignment(t *testing.T) {
	var widths []int
	for _, avail := range []string{"—", "…", "?", "not on AUR", "1.1-1 → 1.2-1"} {
		e := LifecycleEntry{Name: "pkg", Lifecycle: lifecycle.Merged, Available: avail}
		row := strings.SplitN(NewLifecycleList([]LifecycleEntry{e}).Render(), "\n", 2)[0]
		widths = append(widths, lipgloss.Width(row))
	}
	for _, w := range widths {
		if w != widths[0] {
			t.Fatalf("row widths differ (column misaligned): %v", widths)
		}
	}
}

// #331 — an over-long Available value is cut by characters with "…",
// never mid-character, and keeps the column width.
func TestAvailableColumnCut(t *testing.T) {
	long := strings.Repeat("a", 22) + " → " + strings.Repeat("b", 10) // "→" inside the first 25 chars
	e := LifecycleEntry{Name: "pkg", Lifecycle: lifecycle.Merged, Available: long}
	row := strings.SplitN(NewLifecycleList([]LifecycleEntry{e}).Render(), "\n", 2)[0]
	if !utf8.ValidString(row) {
		t.Fatalf("row is not valid UTF-8: %q", row)
	}
	if !strings.Contains(row, strings.Repeat("a", 22)+" →…") {
		t.Fatalf("value not cut at 25 characters with …: %q", row)
	}
}
