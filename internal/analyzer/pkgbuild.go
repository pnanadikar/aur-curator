package analyzer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file reads unified-diff patch text for the Shell analyzer, which
// matches patterns in the lines a change adds (#474). The array and
// scalar checks read the whole old and new PKGBUILD instead
// (pkgbuildfile.go, #484, #485).

// lineKind classifies one line of a unified diff patch.
type lineKind int

const (
	lineContext lineKind = iota
	lineAdded
	lineRemoved
)

// patchLine is one classified, prefix-stripped line from a unified diff,
// in original file order (hunks concatenated in the order they appear
// in the patch). See docs/domain/analyzer-specs.md § Shared Conventions
// § Line classification.
type patchLine struct {
	kind      lineKind
	text      string
	newLineNo int // best-effort new-file line number; 0 if not tracked
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// gitExtendedHeaderPrefixes are the non-content lines git.Provider's
// diff.go includes in FileDiff.Patch ahead of the first "@@" hunk (per
// internal/git/diff.go's parseFileDiffs, which starts each file's body
// at the "diff --git a/X b/X" line itself, not at the first hunk).
// Without skipping these explicitly, they fall through to the default
// case below and get misclassified as Context content — harmless today
// only because none of them happen to match the array/scalar
// regexes, and because newLineNo self-corrects once a real "@@" header
// is reached. That "corrects itself later" behavior doesn't hold for a
// binary file diff, which has no "@@" header at all
// ("Binary files a/X and b/X differ") — newLineNo would never reset
// from 0. Low risk for this sprint's target files (PKGBUILD/.install
// are always text), but skipped explicitly here rather than relied on
// by accident.
var gitExtendedHeaderPrefixes = []string{
	"diff --git ",
	"index ",
	"new file mode ",
	"deleted file mode ",
	"old mode ",
	"new mode ",
	"similarity index ",
	"rename from ",
	"rename to ",
	"copy from ",
	"copy to ",
	"Binary files ",
}

func isGitExtendedHeader(line string) bool {
	for _, prefix := range gitExtendedHeaderPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// splitPatchLines classifies every content line of a unified diff patch.
// Hunk headers (@@...@@), file headers (+++/---), and git's extended
// header lines (diff --git, index, mode changes, renames — see
// gitExtendedHeaderPrefixes) are consumed only for bookkeeping (tracking
// the running new-file line number) and never appear in the returned
// slice. Shared by all three analyzers — the PKGBUILD-specific
// extraction below and shellAnalyzer's direct .install scan both start
// from this same classification.
func splitPatchLines(patch string) []patchLine {
	var lines []patchLine
	newLineNo := 0

	for _, raw := range strings.Split(patch, "\n") {
		// Ignore the trailing empty element produced by
		// strings.Split() when the patch ends with '\n'.
		if raw == "" {
			continue
		}

		switch {
		case isGitExtendedHeader(raw):
			// not content; not even eligible to reset newLineNo
		case strings.HasPrefix(raw, "@@"):
			if m := hunkHeaderRe.FindStringSubmatch(raw); m != nil {
				n, _ := strconv.Atoi(m[1])
				newLineNo = n
			}
		case strings.HasPrefix(raw, "+++"), strings.HasPrefix(raw, "---"):
			// file header; not content
		case strings.HasPrefix(raw, "+"):
			lines = append(lines, patchLine{kind: lineAdded, text: raw[1:], newLineNo: newLineNo})
			newLineNo++
		case strings.HasPrefix(raw, "-"):
			lines = append(lines, patchLine{kind: lineRemoved, text: raw[1:]})
		default:
			text := strings.TrimPrefix(raw, " ")
			lines = append(lines, patchLine{kind: lineContext, text: text, newLineNo: newLineNo})
			newLineNo++
		}
	}

	return lines
}

// location formats a Finding.Location value. line <= 0 means no
// new-file line number is available (e.g. content that only existed in
// the old file) — degrade to the bare path rather than a misleading
// ":0".
func location(path string, line int) string {
	if line <= 0 {
		return path
	}
	return fmt.Sprintf("%s:%d", path, line)
}
