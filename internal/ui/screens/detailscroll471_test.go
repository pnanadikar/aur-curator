package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/theme"
)

// #471 — Up/Down and k/j scroll Package Details' content pane by one
// line; no other key starts scrolling.

func detailWithLongContent() PackageDetailModel {
	m := detailAt(nil, "A")
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	m.contentPane.SetContent(manyLines(200))
	return m
}

func pressDetail(m PackageDetailModel, keys ...tea.KeyMsg) PackageDetailModel {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func TestDetailLineScroll471(t *testing.T) {
	m := pressDetail(detailWithLongContent(), downKeyMsg, jKeyMsg, jKeyMsg)
	if got := m.contentPane.ScrollPosition(); got != 3 {
		t.Fatalf("Down, j, j: scroll position %d, want 3", got)
	}
	m = pressDetail(m, upKeyMsg, kKeyMsg)
	if got := m.contentPane.ScrollPosition(); got != 1 {
		t.Fatalf("then Up, k: scroll position %d, want 1", got)
	}
	m = pressDetail(m, upKeyMsg, upKeyMsg)
	if got := m.contentPane.ScrollPosition(); got != 0 {
		t.Fatalf("Up past the top: scroll position %d, want 0", got)
	}
}

// TestDetailOtherKeysDoNotScroll471: an unavailable action's key falls
// through to the scroll handling (b with Build disabled); it must not
// scroll, nor may the viewport's other keys.
func TestDetailOtherKeysDoNotScroll471(t *testing.T) {
	m := detailWithLongContent()
	m.isMerged = false
	m.review = &domain.Review{Status: domain.ReviewPending}
	if m.isBuildEnabled() {
		t.Fatal("setup: Build is enabled")
	}
	m = pressDetail(m, jKeyMsg, jKeyMsg, jKeyMsg, jKeyMsg, jKeyMsg)
	for _, k := range notScrollKeys {
		m = pressDetail(m, k)
		if got := m.contentPane.ScrollPosition(); got != 5 {
			t.Fatalf("%q moved the content pane to %d, want 5", k.String(), got)
		}
	}
}

// TestDetailLineScrollBlockedByModal471: with the build gate's modal
// open, the keys belong to the modal.
func TestDetailLineScrollBlockedByModal471(t *testing.T) {
	m := detailWithLongContent()
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{localCommits: 1}))
	m.gate = g

	m = pressDetail(m, downKeyMsg, jKeyMsg)
	if got := m.contentPane.ScrollPosition(); got != 0 {
		t.Fatalf("scrolled to %d while a modal was open", got)
	}
}

// TestDetailScrollKeysInFullHelpOnly471: as on Review.
func TestDetailScrollKeysInFullHelpOnly471(t *testing.T) {
	m := detailAt(nil, "A")
	m.helpOpen = true
	help := m.renderActionBar()
	m.helpOpen = false
	assertScrollKeysInHelpOnly(t, help, m.renderActionBar())
}
