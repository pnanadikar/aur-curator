package screens

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/theme"
)

// aurLines are the Findings tab's AUR lines for r, from the fields stored
// on it (tui-review.md § AUR Lines, #495): who maintains the package, on a
// first review its age and standing, and whether the maintainer was
// compared. ok is false when the read failed; the single line then says
// why. No lines for a review from before #495 or one whose analysis had
// no AUR check.
func aurLines(r *domain.Review) (lines []string, ok bool) {
	if r == nil {
		return nil, true
	}
	if r.AURError != "" {
		return []string{"AUR record not read: " + r.AURError + ". Maintainer not compared."}, false
	}
	if r.AUR == nil {
		return nil, true
	}

	who := "AUR: maintainer " + r.AUR.Maintainer
	if r.AUR.Orphaned {
		who = "AUR: orphaned"
	}
	if len(r.AUR.CoMaintainers) > 0 {
		who += "; co-maintainers " + strings.Join(r.AUR.CoMaintainers, ", ")
	}
	lines = append(lines, who)

	if r.BaseCommit == "" && r.AURInfo != nil {
		lines = append(lines, "AUR: "+standing(*r.AURInfo, r.AUR.ReadAt))
	}

	if !hasCategory(r.Findings, analyzer.CategoryMaintainerChanged) {
		if r.PriorAUR == nil {
			lines = append(lines, "AUR: maintainer not compared: no earlier record")
		} else {
			lines = append(lines, "AUR: maintainer unchanged since "+r.PriorAUR.ReadAt.Format(time.DateOnly))
		}
	}
	return lines, true
}

// standing is the first review's age and popularity line, its age counted
// to when the record was read, so the line does not change afterwards.
func standing(info domain.AURInfo, readAt time.Time) string {
	var parts []string
	if !info.FirstSubmitted.IsZero() {
		days := int(readAt.Sub(info.FirstSubmitted).Hours() / 24)
		parts = append(parts, fmt.Sprintf("first submitted %s (%s old)",
			info.FirstSubmitted.Format(time.DateOnly), plural(days, "day")))
	}
	parts = append(parts, plural(info.NumVotes, "vote"))
	// Three significant digits: popularity is often far below 0.01
	// (duplicati-bin: 0.00011), which a fixed two decimals shows as 0.
	parts = append(parts, "popularity "+strconv.FormatFloat(info.Popularity, 'g', 3, 64))
	return strings.Join(parts, "; ")
}

// plural is "1 day", "2 days".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// hasCategory reports whether any finding has category.
func hasCategory(findings []domain.Finding, category string) bool {
	for _, f := range findings {
		if f.Category == category {
			return true
		}
	}
	return false
}

// renderAURLines styles aurLines: dimmed, or in the error style when the
// read failed. "" when there are none.
func renderAURLines(r *domain.Review) string {
	lines, ok := aurLines(r)
	if len(lines) == 0 {
		return ""
	}
	style := theme.DimmedStyle()
	if !ok {
		style = theme.ErrorStyle()
	}
	return style.Render(strings.Join(lines, "\n"))
}
