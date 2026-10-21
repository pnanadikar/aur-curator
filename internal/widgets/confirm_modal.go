package widgets

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/theme"
)

// ModalKind identifies the interactive modal variant currently rendered.
type ModalKind int

const (
	// ModalKindChoice displays a Y/n confirmation prompt.
	ModalKindChoice ModalKind = iota
	// ModalKindText displays a text prompt with submit/skip actions.
	ModalKindText
	// ModalKindConfirmText displays a single-line text entry that submits on Enter.
	ModalKindConfirmText
)

// ModalResult reports the user's action from a modal update.
type ModalResult struct {
	Confirmed bool
	Cancelled bool
	Submitted bool
	Skipped   bool
	Value     string
}

// ConfirmModal renders either a choice modal or a text-entry modal.
type ConfirmModal struct {
	kind        ModalKind
	lines       []string
	prompt      string
	input       textinput.Model
	buttonFocus int
	width       int
}

// NewChoiceModal constructs a choice modal from one or more text lines.
func NewChoiceModal(lines ...string) ConfirmModal {
	return ConfirmModal{
		kind:  ModalKindChoice,
		lines: append([]string(nil), lines...),
	}
}

// NewTextModal constructs a text-entry modal with an optional prompt.
func NewTextModal(prompt string) ConfirmModal {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 256
	input.Focus()

	return ConfirmModal{
		kind:   ModalKindText,
		prompt: prompt,
		input:  input,
	}
}

// NewConfirmTextModal constructs a text-entry modal with static lines
// rendered above a single-line input. Enter confirms; Esc cancels.
func NewConfirmTextModal(lines []string, prompt string) ConfirmModal {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 256
	input.Focus()

	return ConfirmModal{
		kind:   ModalKindConfirmText,
		lines:  append([]string(nil), lines...),
		prompt: prompt,
		input:  input,
	}
}

// SetWidth updates the target width for centering the modal.
func (m *ConfirmModal) SetWidth(width int) {
	m.width = width
	m.input.Width = max(width-18, 24)
}

// Focus activates the modal's primary input.
func (m *ConfirmModal) Focus() tea.Cmd {
	switch m.kind {
	case ModalKindText, ModalKindConfirmText:
		return m.input.Focus()
	}
	return nil
}

// Update processes keyboard interaction for the modal.
func (m *ConfirmModal) Update(msg tea.Msg) (ModalResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return ModalResult{}, nil
	}

	switch m.kind {
	case ModalKindChoice:
		return m.updateChoice(keyMsg)
	case ModalKindText:
		return m.updateText(keyMsg)
	case ModalKindConfirmText:
		return m.updateConfirmText(keyMsg)
	default:
		return ModalResult{}, nil
	}
}

// View renders the modal content inside a centered overlay block.
func (m ConfirmModal) View() string {
	var lines []string
	switch m.kind {
	case ModalKindChoice:
		lines = append(lines, m.lines...)
		lines = append(lines, m.choiceButtons())
	case ModalKindText:
		lines = append(lines, m.prompt)
		lines = append(lines, theme.InputStyle().Render(m.input.View()))
		lines = append(lines, m.textButtons())
	case ModalKindConfirmText:
		lines = append(lines, m.lines...)
		lines = append(lines, m.prompt)
		lines = append(lines, theme.InputStyle().Render(m.input.View()))
		lines = append(lines, m.confirmTextHint())
	default:
		lines = append(lines, m.prompt)
	}

	content := theme.ModalStyle().Render(strings.Join(lines, "\n"))
	if m.width <= lipgloss.Width(content) {
		return content
	}
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
}

func (m *ConfirmModal) updateChoice(msg tea.KeyMsg) (ModalResult, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return ModalResult{Confirmed: true}, nil
	case "n", "N", "esc":
		return ModalResult{Cancelled: true}, nil
	case "left", "shift+tab":
		m.buttonFocus = 0
	case "right", "tab":
		m.buttonFocus = 1
	case "enter":
		if m.buttonFocus == 0 {
			return ModalResult{Confirmed: true}, nil
		}
		return ModalResult{Cancelled: true}, nil
	}
	return ModalResult{}, nil
}

func (m *ConfirmModal) updateText(msg tea.KeyMsg) (ModalResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return ModalResult{Skipped: true}, nil
	case "tab":
		m.buttonFocus = (m.buttonFocus + 1) % 3
		if m.buttonFocus == 0 {
			return ModalResult{}, m.input.Focus()
		}
		m.input.Blur()
		return ModalResult{}, nil
	case "shift+tab":
		m.buttonFocus = (m.buttonFocus + 2) % 3
		if m.buttonFocus == 0 {
			return ModalResult{}, m.input.Focus()
		}
		m.input.Blur()
		return ModalResult{}, nil
	case "left":
		if m.buttonFocus == 2 {
			m.buttonFocus = 1
		}
		return ModalResult{}, nil
	case "right":
		if m.buttonFocus == 1 {
			m.buttonFocus = 2
		}
		return ModalResult{}, nil
	case "enter":
		switch m.buttonFocus {
		case 0, 1:
			return ModalResult{Submitted: true, Value: strings.TrimSpace(m.input.Value())}, nil
		case 2:
			return ModalResult{Skipped: true}, nil
		}
	}

	if m.buttonFocus == 0 {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return ModalResult{}, cmd
	}
	return ModalResult{}, nil
}

func (m *ConfirmModal) updateConfirmText(msg tea.KeyMsg) (ModalResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return ModalResult{Cancelled: true}, nil
	case "enter":
		return ModalResult{Confirmed: true, Value: strings.TrimSpace(m.input.Value())}, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return ModalResult{}, cmd
}

func (m ConfirmModal) confirmTextHint() string {
	return theme.SubtleStyle().Render("[Enter] Confirm   [Esc] Cancel")
}

func (m ConfirmModal) choiceButtons() string {
	yes := theme.ButtonStyle().Render("[Y]")
	no := theme.ButtonStyle().Render("[n]")
	if m.buttonFocus == 0 {
		yes = theme.FocusedButtonStyle().Render("[Y]")
	} else {
		no = theme.FocusedButtonStyle().Render("[n]")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, yes, " ", no)
}

func (m ConfirmModal) textButtons() string {
	submit := theme.ButtonStyle().Render("[Enter]")
	skip := theme.ButtonStyle().Render("[Skip]")
	if m.buttonFocus == 1 {
		submit = theme.FocusedButtonStyle().Render("[Enter]")
	}
	if m.buttonFocus == 2 {
		skip = theme.FocusedButtonStyle().Render("[Skip]")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, submit, " ", skip)
}
