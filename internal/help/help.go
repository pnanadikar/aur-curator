// internal/help/help.go
package help

import (
	_ "embed"

	"github.com/charmbracelet/glamour"
)

//go:embed help.md
var markdown string

// Markdown returns the raw embedded help content, unrendered.
func Markdown() string {
	return markdown
}

// Render renders the embedded help content as Markdown via glamour,
// word-wrapped to the given width. styled selects glamour's terminal
// style (for the TUI, always a real tty) vs. a plain/notty style (for
// CLI output, which may be piped or redirected).
func Render(width int, styled bool) (string, error) {
	styleOpt := glamour.WithStandardStyle("notty")
	if styled {
		styleOpt = glamour.WithStandardStyle("dark")
	}
	r, err := glamour.NewTermRenderer(styleOpt, glamour.WithWordWrap(width))
	if err != nil {
		return "", err
	}
	return r.Render(markdown)
}
