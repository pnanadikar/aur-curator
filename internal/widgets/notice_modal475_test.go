package widgets

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// #475: an information-only modal; Enter or Esc dismisses it.
func TestNoticeModalKeys(t *testing.T) {
	tests := []struct {
		name          string
		key           tea.KeyMsg
		wantConfirmed bool
		wantCancelled bool
	}{
		{"enter dismisses as confirmed", tea.KeyMsg{Type: tea.KeyEnter}, true, false},
		{"esc dismisses as cancelled", tea.KeyMsg{Type: tea.KeyEsc}, false, true},
		{"y does nothing", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, false, false},
		{"n does nothing", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewNoticeModal("line one")
			result, _ := m.Update(tt.key)
			if result.Confirmed != tt.wantConfirmed || result.Cancelled != tt.wantCancelled {
				t.Errorf("result = %+v, want Confirmed=%v Cancelled=%v", result, tt.wantConfirmed, tt.wantCancelled)
			}
		})
	}
}

func TestNoticeModalView(t *testing.T) {
	m := NewNoticeModal("first line", "second line")
	view := m.View()
	for _, want := range []string{"first line", "second line", "[Enter/Esc] Dismiss"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(view, "[Y]") {
		t.Error("notice modal shows choice buttons")
	}
}
