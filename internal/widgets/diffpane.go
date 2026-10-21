package widgets

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/theme"
)

// DiffPane renders a scrollable, styled unified diff over one or more
// FileDiffs. It has no opinion on which FileDiffs belong on which tab —
// callers (e.g. review_screen.go) filter GitDiff.Files by Kind before
// calling SetFiles, keeping tab-to-Kind mapping out of this widget so it
// stays reusable for any future screen that needs a diff view.
type DiffPane struct {
	viewport  viewport.Model
	files     []git.FileDiff
	emptyText string // shown when files is empty; see SetEmptyText
}

// defaultEmptyText is shown when no files are set and the caller has not
// set its own text. It names no refs: DiffPane does not know which range
// its caller compared (the Review screen sets range-specific text, #390).
const defaultEmptyText = "No changes."

// NewDiffPane constructs a DiffPane with a ready-to-use viewport.
func NewDiffPane() DiffPane {
	vp := viewport.New(0, 0)
	// Keyboard scrolling is intentionally limited to PgUp/PgDn (see
	// review_screen.go's scrollKey) so arrow keys stay available to the
	// adjacent Reviewer Notes field. Mouse wheel doesn't collide with
	// anything, so it's left on as the primary way to scroll by hand.
	vp.MouseWheelEnabled = true
	return DiffPane{viewport: vp, emptyText: defaultEmptyText}
}

// SetEmptyText sets the text shown when there are no file diffs to show,
// e.g. for a tab whose file kinds did not change while others did. It
// applies from the next SetFiles or SetSize.
func (d *DiffPane) SetEmptyText(text string) {
	d.emptyText = text
}

// SetSize updates the viewport dimensions.
func (d *DiffPane) SetSize(width, height int) {
	d.viewport.Width = max(width, 0)
	d.viewport.Height = max(height, 0)
	d.viewport.SetContent(renderFiles(d.files, d.emptyText, d.viewport.Width))
}

// SetFiles swaps in a new set of file diffs and refreshes the rendered
// content. Files render in document order, each carrying its own
// "diff --git ..."/"---"/"+++" header lines from Git, with a dim rule
// between consecutive files so one file's end is easy to see.
func (d *DiffPane) SetFiles(files []git.FileDiff) {
	d.files = files
	d.viewport.SetContent(renderFiles(files, d.emptyText, d.viewport.Width))
}

// Update processes scroll keys for the diff viewport.
func (d *DiffPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return cmd
}

// GotoTop scrolls to the first line (Home, #344).
func (d *DiffPane) GotoTop() { d.viewport.GotoTop() }

// GotoBottom scrolls to the last line (End, #344).
func (d *DiffPane) GotoBottom() { d.viewport.GotoBottom() }

// View returns the rendered viewport content.
func (d DiffPane) View() string {
	return d.viewport.View()
}

// ScrollPosition returns the current scroll line.
func (d DiffPane) ScrollPosition() int {
	return d.viewport.YOffset
}

// ViewportHeight returns the current viewport height.
func (d DiffPane) ViewportHeight() int {
	return d.viewport.Height
}

// fileSeparatorWidth is the rule's width when the pane has no width yet.
const fileSeparatorWidth = 40

func renderFiles(files []git.FileDiff, emptyText string, width int) string {
	if emptyText == "" {
		emptyText = defaultEmptyText
	}
	if len(files) == 0 {
		return theme.SubtleStyle().Render(emptyText)
	}

	if width <= 0 {
		width = fileSeparatorWidth
	}
	separator := theme.DimmedStyle().Render(strings.Repeat("─", width))

	rendered := make([]string, 0, len(files))
	for _, f := range files {
		rendered = append(rendered, renderDiff(f))
	}
	return strings.Join(rendered, "\n"+separator+"\n")
}

// renderDiff styles one file's patch. A file listed as changed but with
// no text patch says so by name rather than implying nothing changed.
func renderDiff(f git.FileDiff) string {
	if strings.TrimSpace(f.Patch) == "" {
		return theme.SubtleStyle().Render("No text changes in " + f.Path + ".")
	}

	lines := strings.Split(f.Patch, "\n")
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"),
			strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "@@"):
			rendered = append(rendered, theme.DiffHeaderStyle().Render(line))
		case strings.HasPrefix(line, "+"):
			rendered = append(rendered, theme.DiffAddedStyle().Render(line))
		case strings.HasPrefix(line, "-"):
			rendered = append(rendered, theme.DiffRemovedStyle().Render(line))
		default:
			rendered = append(rendered, theme.DiffContextStyle().Render(line))
		}
	}
	return strings.Join(rendered, "\n")
}
