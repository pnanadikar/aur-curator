package theme

import "github.com/charmbracelet/lipgloss"

// MinScreenWidth is the minimum total width for a screen's outer
// border box — derived from the widest fixed-content line currently
// in the app (Dashboard's summary-cards row, 120 chars) plus
// SectionBorderStyle's horizontal frame (2).
const MinScreenWidth = 122

func DocStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		PaddingTop(1).
		PaddingLeft(2).
		PaddingRight(2)
}

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230"))
}

func SubtleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("247"))
}

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("203")).
		Bold(true)
}

func ActiveTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("60")).
		Padding(0, 1)
}

func InactiveTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Padding(0, 1)
}

func EnabledActionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true)
}

func DimmedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
}

func SectionBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))
}

func InputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("230"))
}

func FocusedButtonStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(lipgloss.Color("222")).
		Padding(0, 1)
}

func ButtonStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

func ModalStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("109")).
		Padding(1, 2).
		Background(lipgloss.Color("236"))
}

func ActionBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

func ActionBarEnabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("238"))
}

func ActionBarDimmedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Background(lipgloss.Color("238"))
}

func DiffHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("117")).
		Bold(true)
}

func DiffRemovedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("203"))
}

func DiffAddedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("78"))
}

func DiffContextStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))
}

func NavChipEnabledStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Background(lipgloss.Color("238")).
		Padding(0, 1)
}

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
