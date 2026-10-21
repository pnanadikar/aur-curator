package screens

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/deps"
)

// SettingsModel represents the read-only settings screen showing
// workspace root, pacman repository location, and policy overrides.
type SettingsModel struct {
	deps deps.Dependencies
}

// NewSettingsModel constructs a new SettingsModel.
func NewSettingsModel(deps deps.Dependencies) SettingsModel {
	return SettingsModel{deps: deps}
}

// Init returns the initialization command for the settings screen.
func (m SettingsModel) Init() tea.Cmd {
	return nil
}

// Update processes messages for the settings screen.
func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg.(type) {
	case tea.WindowSizeMsg:
		return m, nil
	}
	return m, nil
}

// View renders the settings screen.
func (m SettingsModel) View() string {
	return "Settings (not implemented yet)\n"
}
