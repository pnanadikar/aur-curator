package theme

import "github.com/charmbracelet/lipgloss"

// MinScreenWidth is the minimum total width for a screen's outer
// border box — derived from the widest fixed-content line currently
// in the app (Dashboard's summary-cards row, 120 chars) plus
// SectionBorderStyle's horizontal frame (2).
const MinScreenWidth = 122

// DocStyle pads a full-screen message (loading, an error, no package
// selected) away from the screen edge.
func DocStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		PaddingTop(1).
		PaddingLeft(2).
		PaddingRight(2)
}

// HeaderStyle is bold, bright text for titles and section headings.
func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230"))
}

// SubtleStyle is grey text for secondary information: labels, hints,
// counts.
func SubtleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("247"))
}

// ErrorStyle is bold red text for errors and failures.
func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("203")).
		Bold(true)
}

// CautionStyle marks something worth a look that is not an error: the
// Confirm inspector's category headings (#525).
func CautionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)
}

// ActiveTabStyle highlights the selected tab or summary card.
func ActiveTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Padding(0, 1)
}

// InactiveTabStyle is a tab that is not selected.
func InactiveTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Padding(0, 1)
}

// EnabledActionStyle is bold text for an action that is available.
func EnabledActionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true)
}

// DimmedStyle is darker grey text for what is unavailable or of least
// importance.
func DimmedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
}

// SectionBorderStyle is the grey border drawn around a screen section.
func SectionBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))
}

// InputStyle is the text a user types into a field.
func InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("230"))
}

// FocusedButtonStyle highlights the focused button, and the running
// operation in the Operations panel and status bar.
func FocusedButtonStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(lipgloss.Color("222")).
		Padding(0, 1)
}

// ButtonStyle is a button that does not have focus.
func ButtonStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

// ModalStyle is the rounded, padded box a modal is drawn in.
func ModalStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("109")).
		Padding(1, 2).
		Background(lipgloss.Color("236"))
}

// ActionBarStyle is the action bar's background. Not used at present: the
// action bar styles its entries with ActionBarEnabledStyle and
// ActionBarDimmedStyle.
func ActionBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

// ActionBarEnabledStyle is an available action in the action bar.
func ActionBarEnabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("238"))
}

// ActionBarDimmedStyle is an unavailable action in the action bar.
func ActionBarDimmedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("238"))
}

// DiffHeaderStyle is a diff's header lines, and commit hashes.
func DiffHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("117")).
		Bold(true)
}

// DiffRemovedStyle is a removed line in a diff.
func DiffRemovedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("203"))
}

// DiffAddedStyle is an added line in a diff.
func DiffAddedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("78"))
}

// DiffContextStyle is an unchanged context line in a diff.
func DiffContextStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))
}

// NavChipEnabledStyle is a Nav Menu entry that can be chosen.
func NavChipEnabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

// NavChipDimmedStyle is a Nav Menu entry that cannot be chosen (Settings).
func NavChipDimmedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

//func HintStyle() lipgloss.Style {
//	return lipgloss.NewStyle().
//		Foreground(lipgloss.Color("245")).
//		Italic(true)
//}
