package screens

import (
	"context"
	"fmt"
	"strings"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/inspect"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
)

// artifactInspection is what one artifact installs with special power,
// or why it could not be read (#525; domain.md § Build → Confirmed).
type artifactInspection struct {
	report inspect.Report
	err    error
}

// inspectArtifacts inspects each artifact, in order. It does I/O: call it
// from a tea.Cmd only. A failure is kept with its artifact and written to
// the error log; the others are still inspected. nil when no Inspector is
// configured (tests).
func inspectArtifacts(ctx context.Context, d deps.Dependencies, pkg domain.PackageName, arts []domain.Artifact) []artifactInspection {
	if d.Inspector == nil || len(arts) == 0 {
		return nil
	}
	out := make([]artifactInspection, len(arts))
	for i, a := range arts {
		r, err := d.Inspector.Inspect(ctx, a.BuildID, a.Filename)
		out[i] = artifactInspection{report: r, err: err}
		if err != nil {
			trace.Log(ctx, "inspect", "pkg", pkg, "file", a.Filename, "error", err.Error())
			_ = errorlog.Log(ctx, d.Config.RootPath, "inspect", fmt.Sprintf("%s: %s: %v", pkg, a.Filename, err))
		}
	}
	return out
}

// nothingSpecial is the line for an artifact with no entry in any
// category.
const nothingSpecial = "Nothing with special power."

// cannotInspect is the line for an artifact that could not be read.
func cannotInspect(err error) string {
	return "⚠ cannot inspect: " + strings.ReplaceAll(sanitize.ForMessage(err.Error()), "\n", " ")
}

// inspectionLines is an artifact's full inspection for the Artifacts tab,
// each line starting with indent: the categories with their counts, and
// under each its entries.
func inspectionLines(ins artifactInspection, indent string) []string {
	if ins.err != nil {
		return []string{indent + theme.ErrorStyle().Render(cannotInspect(ins.err))}
	}
	if ins.report.Empty() {
		return []string{indent + theme.SubtleStyle().Render(nothingSpecial)}
	}
	var lines []string
	for _, g := range ins.report.Groups {
		lines = append(lines, indent+categoryHeading(g))
		for _, e := range g.Entries {
			lines = append(lines, indent+"  "+entryLine(g.Category, e))
		}
	}
	return lines
}

// inspectionSummary is the Confirm modal's view of one artifact: the
// categories with their counts and up to budget paths in all, in
// category order; it returns the lines, the paths shown and the paths
// left unshown.
func inspectionSummary(ins artifactInspection, budget int) (lines []string, shown, more int) {
	if ins.err != nil {
		return []string{cannotInspect(ins.err)}, 0, 0
	}
	if ins.report.Empty() {
		return []string{nothingSpecial}, 0, 0
	}
	for _, g := range ins.report.Groups {
		lines = append(lines, categoryHeading(g))
		for _, e := range g.Entries {
			if shown == budget {
				more++
				continue
			}
			lines = append(lines, "  "+entryLine(g.Category, e))
			shown++
		}
	}
	return lines, shown, more
}

// hasInstall reports whether any inspected artifact has an install
// script.
func hasInstall(ins []artifactInspection) bool {
	for _, i := range ins {
		if i.err == nil && i.report.HasInstall() {
			return true
		}
	}
	return false
}

// categoryHeading is a category with its count, highlighted so a finding
// stands out from the artifact's other lines (Dan, 15 Nov).
func categoryHeading(g inspect.Group) string {
	return theme.CautionStyle().Render(fmt.Sprintf("%s (%d)", g.Category, len(g.Entries)))
}

// entryLine is one entry: the path, with mode and owner for the two
// categories that are about them.
func entryLine(c inspect.Category, e inspect.Entry) string {
	p := displayPath(e.Path)
	if e.Type == "link" {
		p += " -> " + displayPath(e.Link)
	}
	switch c {
	case inspect.CatSetID, inspect.CatNotRoot:
		return fmt.Sprintf("%s %s %s  %s", modeString(e), owner(e.Uname, e.UID), owner(e.Gname, e.GID), p)
	}
	return p
}

// displayPath escapes what a path may hold that a terminal would act on
// or hide (#491), newlines included.
func displayPath(p string) string {
	return strings.ReplaceAll(sanitize.Escape(p), "\n", `\n`)
}

func owner(name string, id int) string {
	if name != "" {
		return displayPath(name)
	}
	return fmt.Sprint(id)
}

// modeString renders a mode as ls does: type, then rwx with s/S and t/T.
func modeString(e inspect.Entry) string {
	t := map[string]byte{"dir": 'd', "link": 'l', "char": 'c', "block": 'b', "fifo": 'p', "socket": 's'}[e.Type]
	if t == 0 {
		t = '-'
	}
	b := []byte{t, '-', '-', '-', '-', '-', '-', '-', '-', '-'}
	const perm = "rwxrwxrwx"
	for i := 0; i < len(perm); i++ {
		if e.Mode&(1<<(8-uint(i))) != 0 {
			b[i+1] = perm[i]
		}
	}
	special := func(bit uint32, pos int, set, unset byte) {
		if e.Mode&bit == 0 {
			return
		}
		if b[pos] == 'x' {
			b[pos] = set
		} else {
			b[pos] = unset
		}
	}
	special(0o4000, 3, 's', 'S')
	special(0o2000, 6, 's', 'S')
	special(0o1000, 9, 't', 'T')
	return string(b)
}
