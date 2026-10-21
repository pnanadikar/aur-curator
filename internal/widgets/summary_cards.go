package widgets

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// SummaryCard renders a single summary card with a label, count, and
// description. When selected it is highlighted with a background colour
// so the user can see which filter is active.
type SummaryCard struct {
	Card

	selected bool
}

// Card holds the data displayed by one summary card.
type Card struct {
	Label   string
	Count   int
	IsStub  bool // when true, renders as "— / not yet implemented" instead of a number
	Unknown bool // when true, renders "?" instead of Count: it could not be determined (#331)
}

// NewSummaryCard creates a new SummaryCard with the given card data.
func NewSummaryCard(c Card) SummaryCard {
	return SummaryCard{Card: c}
}

// Selected returns whether this card is currently selected.
func (c SummaryCard) Selected() bool {
	return c.selected
}

// SetSelected marks this card as selected (or deselected).
func (c *SummaryCard) SetSelected(selected bool) {
	c.selected = selected
}

// RenderCards renders all summary cards as compact inline items on a
// single line, separated by a pipe. Each card shows "Label: Count"
// with no description text. The selected card is highlighted.
func RenderCards(cards []SummaryCard) string {
	var parts []string
	for _, c := range cards {
		selected := c.selected

		var card string
		if c.IsStub {
			raw := c.Label + ": \u2014"
			if selected {
				card = theme.ActiveTabStyle().Render("[ " + raw + " ]")
			} else {
				card = theme.DimmedStyle().Render(raw)
			}
		} else {
			raw := c.Label + ": " + cardCount(c.Card)
			if selected {
				card = theme.ActiveTabStyle().Render("[ " + raw + " ]")
			} else {
				card = lipgloss.NewStyle().
					Foreground(lipgloss.Color("15")).
					Render(raw)
			}
		}

		parts = append(parts, card)
	}
	return strings.Join(parts, " | ")
}

// cardCount renders a card's count, or "?" when it is unknown (#331).
func cardCount(c Card) string {
	if c.Unknown {
		return "?"
	}
	return strconv.Itoa(c.Count)
}
