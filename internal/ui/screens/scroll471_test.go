package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/theme"
)

// #471 — Up/Down and k/j scroll the Review panes and Package Details'
// content pane by one line; no other key starts scrolling.

var (
	upKeyMsg   = tea.KeyMsg{Type: tea.KeyUp}
	downKeyMsg = tea.KeyMsg{Type: tea.KeyDown}
	kKeyMsg    = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	jKeyMsg    = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
)

// notScrollKeys are keys the viewport's own Update scrolls on (f, b, d,
// u, space, h, l, left, right), which #471 does not bind.
var notScrollKeys = []tea.KeyMsg{
	{Type: tea.KeyRunes, Runes: []rune{'f'}},
	{Type: tea.KeyRunes, Runes: []rune{'b'}},
	{Type: tea.KeyRunes, Runes: []rune{'d'}},
	{Type: tea.KeyRunes, Runes: []rune{'u'}},
	{Type: tea.KeyRunes, Runes: []rune{'h'}},
	{Type: tea.KeyRunes, Runes: []rune{'l'}},
	{Type: tea.KeySpace, Runes: []rune{' '}},
	{Type: tea.KeyLeft},
	{Type: tea.KeyRight},
}

func reviewWithLongPanes(t *testing.T) ReviewScreen {
	t.Helper()
	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	upd, _ := model.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	model = upd
	model.diffPane.SetFiles([]git.FileDiff{{Patch: manyLines(200)}})
	model.findingsPane.SetContent(manyLines(200))
	return model
}

func pressReview(t *testing.T, model ReviewScreen, keys ...tea.KeyMsg) ReviewScreen {
	t.Helper()
	for _, k := range keys {
		upd, _ := model.Update(k)
		model = upd
	}
	return model
}

func TestReviewLineScroll471(t *testing.T) {
	for _, tc := range []struct {
		name     string
		findings bool
	}{{"Diff", false}, {"Findings", true}} {
		t.Run(tc.name, func(t *testing.T) {
			model := reviewWithLongPanes(t)
			active, other := func() int { return model.diffPane.ScrollPosition() }, func() int { return model.findingsPane.ScrollPosition() }
			if tc.findings {
				model.tabs.SetActive(4)
				active, other = other, active
			}

			model = pressReview(t, model, downKeyMsg, jKeyMsg, jKeyMsg)
			if got := active(); got != 3 {
				t.Fatalf("Down, j, j: scroll position %d, want 3", got)
			}
			model = pressReview(t, model, upKeyMsg, kKeyMsg)
			if got := active(); got != 1 {
				t.Fatalf("then Up, k: scroll position %d, want 1", got)
			}
			if got := other(); got != 0 {
				t.Fatalf("the inactive pane moved to %d", got)
			}
			model = pressReview(t, model, upKeyMsg, upKeyMsg)
			if got := active(); got != 0 {
				t.Fatalf("Up past the top: scroll position %d, want 0", got)
			}
		})
	}
}

// TestReviewLineScrollAtBottom471: Down stops at the last page.
func TestReviewLineScrollAtBottom471(t *testing.T) {
	model := pressReview(t, reviewWithLongPanes(t), endKeyMsg)
	bottom := model.diffPane.ScrollPosition()
	model = pressReview(t, model, downKeyMsg, jKeyMsg)
	if got := model.diffPane.ScrollPosition(); got != bottom {
		t.Fatalf("Down at the bottom: scroll position %d, want %d", got, bottom)
	}
}

// TestReviewOtherKeysDoNotScroll471: only the bound keys scroll; the
// viewport's own keys never reach it, and ctrl+left/right still switch
// tabs without scrolling.
func TestReviewOtherKeysDoNotScroll471(t *testing.T) {
	model := pressReview(t, reviewWithLongPanes(t), jKeyMsg, jKeyMsg, jKeyMsg, jKeyMsg, jKeyMsg)
	for _, k := range notScrollKeys {
		model = pressReview(t, model, k)
		if got := model.diffPane.ScrollPosition(); got != 5 {
			t.Fatalf("%q moved the Diff pane to %d, want 5", k.String(), got)
		}
	}
	before := model.tabs.Active().Label
	model = pressReview(t, model, tea.KeyMsg{Type: tea.KeyCtrlRight})
	if model.tabs.Active().Label == before {
		t.Fatal("ctrl+right no longer switches tabs")
	}
	model = pressReview(t, model, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if got := model.tabs.Active().Label; got != before {
		t.Fatalf("ctrl+left: active tab %q, want %q", got, before)
	}
	if got := model.findingsPane.ScrollPosition(); got != 0 {
		t.Fatalf("tab keys scrolled the Findings pane to %d", got)
	}
}

// TestReviewLineScrollBlocked471: no line scrolling with the Approve
// modal open (its notes input owns the keys) or while loading.
func TestReviewLineScrollBlocked471(t *testing.T) {
	model := pressReview(t, reviewWithLongPanes(t), tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal == nil {
		t.Fatal("setup: Enter did not open the Approve modal")
	}
	model = pressReview(t, model, downKeyMsg, jKeyMsg)
	if got := model.diffPane.ScrollPosition(); got != 0 {
		t.Fatalf("scrolled to %d with the Approve modal open", got)
	}

	model = reviewWithLongPanes(t)
	model.loading = true
	model = pressReview(t, model, downKeyMsg, jKeyMsg)
	if got := model.diffPane.ScrollPosition(); got != 0 {
		t.Fatalf("scrolled to %d while loading", got)
	}
}

// TestReviewScrollKeysInFullHelpOnly471: the full help lists the scroll
// keys; the compact bar does not (it is not cut to the width).
func TestReviewScrollKeysInFullHelpOnly471(t *testing.T) {
	model := reviewWithLongPanes(t)
	model.helpOpen = true
	assertScrollKeysInHelpOnly(t, model.renderActionBar(), func() string {
		model.helpOpen = false
		return model.renderActionBar()
	}())
}

func assertScrollKeysInHelpOnly(t *testing.T, help, compact string) {
	t.Helper()
	for _, want := range []string{"↑/↓ k/j", "Scroll line", "PgUp/PgDn", "Scroll page", "Home/End", "Top/Bottom"} {
		if !strings.Contains(help, want) {
			t.Errorf("full help missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(compact, "Scroll") {
		t.Errorf("compact bar lists scroll keys:\n%s", compact)
	}
}
