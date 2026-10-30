package widgets

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/theme"
)

// AvailableColWidth is the Available column's width in cells. The
// Dashboard uses it to pick a value that fits (#331).
const AvailableColWidth = 25

// Column widths in cells. HeaderRow and Render share them, so the header's
// labels sit over their columns (#448). State has no constant: its width
// depends on the page (stateColWidth).
const (
	nameColWidth       = 24
	nextActionColWidth = 12
	publishedColWidth  = 15
	riskColWidth       = 10
	localColWidth      = 8
)

// stateHeader is the State column's header label. The column is never
// narrower than it.
const stateHeader = "State"

// StateColMaxWidth is the State column's greatest width in cells, versions
// included (tui-dashboard-wireframe.md § State, #428).
const StateColMaxWidth = 34

// stateColWidth returns the State column's width for a page (see
// stateLayout), so the header and the rows agree on every page (#448).
func stateColWidth(entries []LifecycleEntry) int {
	_, w := stateLayout(entries)
	return w
}

// StateVersions are the versions the State column shows after the state
// (tui-dashboard-wireframe.md § State, #428). Each is the PKGBUILD's
// "[epoch:]pkgver-pkgrel" at a commit, or "" when it was not read or
// could not be.
type StateVersions struct {
	Base     string // at the review's base commit
	Reviewed string // at the review's reviewed commit (UpstreamCommit)
	Head     string // at local master HEAD

	// FirstReview: the review has no base commit, so Base is "" by
	// design, not because its read failed.
	FirstReview bool
}

// stateParts splits what the State cell shows after the state: base is
// the version before the arrow, arrow says whether there is an arrow, and
// target is the version the row is about. Review Pending and Approved show
// base → reviewed ("→ reviewed" on a first review); Rejected shows the
// reviewed version; Merged to Published show the version at master HEAD.
// All are empty when there is nothing to show, including when a version
// could not be read: never half a pair.
func stateParts(state lifecycle.State, v StateVersions) (base string, arrow bool, target string) {
	switch state {
	case lifecycle.ReviewPending, lifecycle.Approved:
		switch {
		case v.Reviewed == "":
			return "", false, ""
		case v.FirstReview:
			return "", true, v.Reviewed
		case v.Base == "":
			return "", false, ""
		}
		return v.Base, true, v.Reviewed
	case lifecycle.Rejected:
		return "", false, v.Reviewed
	case lifecycle.Merged, lifecycle.Built, lifecycle.Confirmed, lifecycle.Published:
		return "", false, v.Head
	default:
		return "", false, ""
	}
}

// stateLayout returns each row's State cell text for a page, and the
// column's width (tui-dashboard-wireframe.md § State, #428).
//
// The versions line up across the page: the state names are padded to
// the longest one, what comes before the target version ("base → " or
// "→ ") is right-aligned, so arrows sit in one column and every target
// version starts in the same column. A row with no versions shows its
// state alone.
//
// The column is at most StateColMaxWidth cells. When the page does not
// fit, the pairs drop their base version; a cell still too long is cut
// with "…". The state itself is never cut. The width is never less than
// the header label, and with no rows it is the label's width.
func stateLayout(entries []LifecycleEntry) (cells []string, width int) {
	build := func(dropBase bool) ([]string, int) {
		prefixes := make([]string, len(entries))
		targets := make([]string, len(entries))
		nameW, prefixW := 0, 0
		for i, e := range entries {
			base, arrow, target := stateParts(e.Lifecycle, e.Versions)
			if target == "" {
				continue
			}
			switch {
			case arrow && base != "" && !dropBase:
				prefixes[i] = base + " → "
			case arrow:
				prefixes[i] = "→ "
			}
			targets[i] = target
			nameW = max(nameW, len(e.Lifecycle))
			prefixW = max(prefixW, lipgloss.Width(prefixes[i]))
		}
		out := make([]string, len(entries))
		w := len(stateHeader)
		for i, e := range entries {
			if targets[i] == "" {
				out[i] = string(e.Lifecycle)
			} else {
				out[i] = padRight(string(e.Lifecycle), nameW) + " " +
					strings.Repeat(" ", prefixW-lipgloss.Width(prefixes[i])) + prefixes[i] + targets[i]
			}
			w = max(w, lipgloss.Width(out[i]))
		}
		return out, w
	}

	cells, width = build(false)
	if width > StateColMaxWidth {
		cells, width = build(true)
	}
	if width > StateColMaxWidth {
		for i, c := range cells {
			if lipgloss.Width(c) > StateColMaxWidth {
				cells[i] = string([]rune(c)[:StateColMaxWidth-1]) + "…"
			}
		}
		width = StateColMaxWidth
	}
	return cells, width
}

// stateText returns one row's State cell as it would show on a page of its
// own.
func stateText(state lifecycle.State, v StateVersions) string {
	cells, _ := stateLayout([]LifecycleEntry{{Lifecycle: state, Versions: v}})
	return cells[0]
}

// padRight pads s with spaces to w display cells. "…", "—" and "→" are
// several bytes but one cell (#325).
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// LifecycleEntry holds one package's name and its computed lifecycle state.
type LifecycleEntry struct {
	Name               domain.PackageName
	Lifecycle          lifecycle.State
	Updates            int
	NextAction         string
	Available          string
	ConfirmedPublished string
	Risk               string

	// LocalCommits is the number of commits on local master that are not
	// on upstream (workflow.md § Local Commits Not on Upstream).
	// LocalCommitsUnknown means that check failed: the column shows "? local",
	// never blank, so a failed check is not mistaken for "none".
	LocalCommits        int
	LocalCommitsUnknown bool

	// Versions are shown after the state in the State column (#428).
	// Lifecycle stays the bare state: sorting and filtering use it.
	Versions StateVersions
}

// LifecycleList renders a scrollable package list with lifecycle status,
// cursor navigation, search filtering, lifecycle state filtering, and
// sorting.
type LifecycleList struct {
	entries  []LifecycleEntry
	cursor   int
	page     int // 0-based page index for pagination
	pageSize int
	filter   lifecycle.State // zero value means "all"
	sortBy   string          // "name" or "updates"
	search   string
}

// NewLifecycleList creates a LifecycleList from a set of lifecycle entries.
func NewLifecycleList(entries []LifecycleEntry) LifecycleList {
	return LifecycleList{
		entries:  entries,
		pageSize: 20,
		filter:   "",
		sortBy:   "name",
	}
}

// SetPageSize sets the number of rows per page and resets the cursor.
func (l *LifecycleList) SetPageSize(n int) {
	l.pageSize = n
	l.cursor = 0
	l.page = 0
}

// PageSize returns the current page size.
func (l LifecycleList) PageSize() int {
	return l.pageSize
}

// SetFilter sets the lifecycle filter. Pass zero value for "all".
func (l *LifecycleList) SetFilter(f lifecycle.State) {
	l.filter = f
	l.cursor = 0
	l.page = 0
}

// SetSearch sets the search text for name filtering.
func (l *LifecycleList) SetSearch(s string) {
	l.search = s
	l.cursor = 0
	l.page = 0
}

// SetSort sets the sort mode ("name" or "updates").
func (l *LifecycleList) SetSort(s string) {
	l.sortBy = s
}

// VisibleEntries returns the entries for the current page, already
// filtered and sorted.
func (l LifecycleList) VisibleEntries() []LifecycleEntry {
	filtered := l.filtered()
	sorted := l.sorted(filtered)
	start := l.page * l.pageSize
	if start >= len(sorted) {
		return nil
	}
	end := start + l.pageSize
	if end > len(sorted) {
		end = len(sorted)
	}
	result := make([]LifecycleEntry, end-start)
	copy(result, sorted[start:end])
	return result
}

// TotalPages returns the number of pages.
func (l LifecycleList) TotalPages() int {
	filtered := l.filtered()
	sorted := l.sorted(filtered)
	if len(sorted) == 0 {
		return 1
	}
	return (len(sorted) + l.pageSize - 1) / l.pageSize
}

// Cursor returns the current cursor position within the visible page.
func (l LifecycleList) Cursor() int {
	return l.cursor
}

// SetCursor sets the cursor position, clamping to the current page.
func (l *LifecycleList) SetCursor(c int) {
	visible := l.VisibleEntries()
	if len(visible) == 0 {
		l.cursor = 0
		return
	}
	if c < 0 {
		l.cursor = 0
		return
	}
	if c >= len(visible) {
		l.cursor = len(visible) - 1
		return
	}
	l.cursor = c
}

// CursorUp moves the cursor up one row. On the first row of any page but
// the first, it moves to the previous page with the cursor on its last
// row, the mirror of CursorDown (#447).
func (l *LifecycleList) CursorUp() {
	if l.cursor > 0 {
		l.cursor--
		return
	}
	if l.page == 0 {
		return
	}
	l.page--
	l.cursor = max(len(l.VisibleEntries())-1, 0)
}

// CursorDown moves the cursor down one row. On the last row of a full
// page, it moves to the next page with the cursor on its first row.
func (l *LifecycleList) CursorDown() {
	visible := l.VisibleEntries()
	if l.cursor < len(visible)-1 {
		l.cursor++
		return
	}
	if len(visible) == l.pageSize {
		l.NextPage()
	}
}

// NextPage moves to the next page if available.
func (l *LifecycleList) NextPage() {
	if l.page < l.TotalPages()-1 {
		l.page++
		l.cursor = 0
	}
}

// PrevPage moves to the previous page if available.
func (l *LifecycleList) PrevPage() {
	if l.page > 0 {
		l.page--
		l.cursor = 0
	}
}

// Filter returns the current lifecycle filter.
func (l LifecycleList) Filter() lifecycle.State {
	return l.filter
}

// filtered returns entries matching the current search and lifecycle filter.
func (l LifecycleList) filtered() []LifecycleEntry {
	var result []LifecycleEntry
	for _, e := range l.entries {
		if l.filter != "" && e.Lifecycle != l.filter {
			continue
		}
		if l.search != "" && !strings.Contains(strings.ToLower(string(e.Name)), strings.ToLower(l.search)) {
			continue
		}
		result = append(result, e)
	}
	return result
}

// lifecycleOrder returns the sort priority for a lifecycle state.
// Lower values sort first.
func lifecycleOrder(s lifecycle.State) int {
	switch s {
	case lifecycle.NotReviewed:
		return 0
	case lifecycle.ReviewPending:
		return 1
	case lifecycle.Approved:
		return 2
	case lifecycle.Merged:
		return 3
	case lifecycle.Built:
		return 4
	case lifecycle.Confirmed:
		return 5
	case lifecycle.Published:
		return 6
	case lifecycle.Rejected:
		return 7
	default:
		return 8
	}
}

// sorted returns entries sorted by the current sort mode.
func (l LifecycleList) sorted(entries []LifecycleEntry) []LifecycleEntry {
	sorted := make([]LifecycleEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		switch l.sortBy {
		case "lifecycle":
			oi := lifecycleOrder(sorted[i].Lifecycle)
			oj := lifecycleOrder(sorted[j].Lifecycle)
			if oi != oj {
				return oi < oj
			}
			return strings.ToLower(string(sorted[i].Name)) < strings.ToLower(string(sorted[j].Name))
		case "updates":
			return sorted[i].Updates > sorted[j].Updates
		default:
			return strings.ToLower(string(sorted[i].Name)) < strings.ToLower(string(sorted[j].Name))
		}
	})
	return sorted
}

// Render renders the package list with cursor, lifecycle status, and
// upstream update count columns.
func (l LifecycleList) Render() string {
	entries := l.VisibleEntries()
	if len(entries) == 0 {
		return theme.SubtleStyle().Render("No packages match the current filter.")
	}

	stateCells, stateWidth := stateLayout(entries)

	var b strings.Builder
	for i, e := range entries {
		var prefix string
		if i == l.cursor {
			prefix = "> "
		} else {
			prefix = "  "
		}

		// Name column: always exactly nameColWidth cells. Padding uses display
		// width, not bytes — "…" is 3 bytes but 1 cell (#325). AUR names are
		// ASCII, so slicing by bytes cannot split a character.
		name := string(e.Name)
		if lipgloss.Width(name) > nameColWidth {
			name = name[:nameColWidth-1] + "…"
		}
		nameCol := prefix + theme.HeaderStyle().Render(name) + strings.Repeat(" ", max(nameColWidth-lipgloss.Width(name), 0))

		lifecycleStyle := lifecycleStyle(e.Lifecycle)
		// The state in its colour, then its versions unstyled (#428).
		st := stateCells[i]
		lifecycleCol := lifecycleStyle.Render(string(e.Lifecycle)) + st[len(e.Lifecycle):]
		lifecycleCol += strings.Repeat(" ", max(stateWidth-lipgloss.Width(st), 0))

		// Cut by characters and pad by display width: "—" is 3 bytes but
		// 1 cell (#325, #351).
		na := e.NextAction
		if r := []rune(na); len(r) > nextActionColWidth {
			na = string(r[:nextActionColWidth])
		}
		nextActionCol := padRight(na, nextActionColWidth)

		// Cut by characters, never bytes ("→", "…", "—" are multi-byte),
		// and mark the cut (#331).
		avail := e.Available
		if r := []rune(avail); len(r) > AvailableColWidth {
			avail = string(r[:AvailableColWidth-1]) + "…"
		}
		availableCol := avail + strings.Repeat(" ", max(AvailableColWidth-lipgloss.Width(avail), 0))

		cp := e.ConfirmedPublished
		if len(cp) > publishedColWidth {
			cp = cp[:publishedColWidth]
		}
		confPubCol := cp + strings.Repeat(" ", publishedColWidth-len(cp))

		var confPubStyle lipgloss.Style
		switch e.ConfirmedPublished {
		case "Yes":
			confPubStyle = theme.SubtleStyle().Foreground(lipgloss.Color("42"))
		case "Confirmed":
			confPubStyle = theme.SubtleStyle().Foreground(lipgloss.Color("220"))
		default:
			confPubStyle = theme.DimmedStyle()
		}
		confPubCol = confPubStyle.Render(confPubCol)

		r := e.Risk
		if len(r) > riskColWidth {
			r = r[:riskColWidth]
		}
		riskCol := r + strings.Repeat(" ", riskColWidth-len(r))

		var riskStyle lipgloss.Style
		switch e.Risk {
		case "HIGH":
			riskStyle = theme.SubtleStyle().Foreground(lipgloss.Color("203"))
		case "MEDIUM":
			riskStyle = theme.SubtleStyle().Foreground(lipgloss.Color("220"))
		case "LOW", "NONE":
			riskStyle = theme.SubtleStyle().Foreground(lipgloss.Color("34"))
		default:
			riskStyle = theme.DimmedStyle()
		}
		riskCol = riskStyle.Render(riskCol)

		// Local: commits on master not on upstream. The header names the
		// column (#448): blank when none, the count when some, "?" when the
		// check failed.
		localText := ""
		switch {
		case e.LocalCommitsUnknown:
			localText = "?"
		case e.LocalCommits > 0:
			localText = strconv.Itoa(e.LocalCommits)
		}
		if len(localText) > localColWidth {
			localText = localText[:localColWidth]
		}
		localCol := theme.SubtleStyle().Foreground(lipgloss.Color("220")).Render(localText + strings.Repeat(" ", localColWidth-len(localText)))

		updatesCol := ""
		if e.Updates > 0 {
			updatesCol = theme.SubtleStyle().Render("↑" + strconv.Itoa(e.Updates))
		}

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, nameCol, "  ", lifecycleCol, "  ", nextActionCol, "  ", availableCol, "  ", confPubCol, "  ", riskCol, "  ", localCol))
		if updatesCol != "" {
			b.WriteString("  " + updatesCol)
		}
		b.WriteString("\n")
	}
	if pad := l.pageSize - len(entries); pad > 0 {
		b.WriteString(strings.Repeat("\n", pad))
	}
	return b.String()
}

// HeaderRow returns the row that names the columns, for the current page
// (tui-dashboard-wireframe.md § Header Row, #448). Each label starts where
// its column starts: Package after the two-cell cursor prefix, State at
// the page's State width. Behind labels the trailing ↑N count, which has
// no fixed width because it is last. The header is returned even when no
// rows match the filter, so the layout does not move.
func (l LifecycleList) HeaderRow() string {
	labels := []string{
		"  " + padRight("Package", nameColWidth),
		padRight(stateHeader, stateColWidth(l.VisibleEntries())),
		padRight("Next Action", nextActionColWidth),
		padRight("Available", AvailableColWidth),
		padRight("Published", publishedColWidth),
		padRight("Risk", riskColWidth),
		padRight("Local", localColWidth),
		"Behind",
	}
	return theme.SubtleStyle().Render(strings.Join(labels, "  "))
}

func lifecycleStyle(s lifecycle.State) lipgloss.Style {
	switch s {
	case lifecycle.Approved:
		return theme.SubtleStyle().Foreground(lipgloss.Color("78"))
	case lifecycle.ReviewPending:
		return theme.SubtleStyle().Foreground(lipgloss.Color("220"))
	case lifecycle.Rejected:
		return theme.ErrorStyle()
	default:
		return theme.DimmedStyle()
	}
}
