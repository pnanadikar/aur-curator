package analyzer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

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
// only because none of them happen to match the array/scalar/function
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

// pkgbuildEntry pairs a piece of Added (or Context, still-present)
// content with its best-effort line number in the new (upstream) file.
// Used for both individual array entries and function-body Added lines
// — line tracking works the same way in both cases. There is no
// equivalent for Removed content: a Removed line doesn't exist in the
// new file, so a new-file line number for it would be meaningless.
type pkgbuildEntry struct {
	Value string
	Line  int
}

// pkgbuildArray is one array-style PKGBUILD assignment (source,
// sha256sums, validpgpkeys, etc.), split into individual quoted entries.
// Added/Removed reflect only what's visible in the diff patch — an
// unchanged entry far from any change may not appear in Patch text at
// all (outside the diff's context window), so this is not guaranteed to
// be the array's complete old/new contents. See
// docs/domain/analyzer-specs.md § Known Limitations.
type pkgbuildArray struct {
	Name    string // base name, arch suffix stripped: "source", "sha256sums", "validpgpkeys"
	Arch    string // arch suffix without leading underscore, e.g. "x86_64"; "" if unsuffixed
	Line    int    // best-effort new-file line of the array's opening "name=(" line; 0 if the array doesn't exist in the new file (wholly deleted, not replaced)
	Added   []pkgbuildEntry
	Removed []string
}

// pkgbuildScalar is one scalar assignment (pkgver, pkgrel).
type pkgbuildScalar struct {
	Name    string
	Old     string // "" if not present/changed among Removed lines
	New     string // "" if not present/changed among Added lines
	NewLine int    // best-effort new-file line of New's line; 0 if New == ""
}

// pkgbuildFunction is one function body's Added lines only — Removed and
// Context lines inside a function are not collected, since analyzers
// only flag *new* risk (a reviewer already implicitly accepted whatever
// was in a prior approved review).
type pkgbuildFunction struct {
	Name       string // "build", "package", "prepare"
	AddedLines []pkgbuildEntry
}

type pkgbuildParsed struct {
	Arrays    []pkgbuildArray
	Scalars   []pkgbuildScalar
	Functions []pkgbuildFunction
}

// Array returns the first parsed array matching name (base name, arch
// suffix ignored) — sufficient for this sprint's analyzers, which treat
// all arch variants of a given array the same way. Returns nil if absent.
func (p pkgbuildParsed) arraysNamed(name string) []pkgbuildArray {
	var out []pkgbuildArray
	for _, a := range p.Arrays {
		if a.Name == name {
			out = append(out, a)
		}
	}
	return out
}

func (p pkgbuildParsed) scalar(name string) (pkgbuildScalar, bool) {
	for _, s := range p.Scalars {
		if s.Name == name {
			return s, true
		}
	}
	return pkgbuildScalar{}, false
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

// parsePKGBUILDDiff extracts every array, scalar, and function-body this
// sprint's analyzers need from a PKGBUILD FileDiff's Patch text — one
// shared parsing pass all three PKGBUILD-touching analyzers (URL,
// Checksum, Shell) run against, rather than each re-scanning the same
// patch text with its own regexes. See docs/domain/analyzer-specs.md.
func parsePKGBUILDDiff(patch string) pkgbuildParsed {
	lines := splitPatchLines(patch)
	return pkgbuildParsed{
		Arrays:    parseArrays(lines),
		Scalars:   parseScalars(lines),
		Functions: parseFunctions(lines),
	}
}

// arrayStartRe matches the opening line of a PKGBUILD array assignment.
// Group 1: base name. Group 2: arch suffix including leading underscore,
// or "" if unsuffixed.
var arrayStartRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_]*?)(_[A-Za-z0-9_]+)?\s*=\s*\(`)

// quotedEntryRe matches one quoted array entry. Single-quoted spans are
// literal (bash takes everything up to the next ' as-is); a double-quoted
// span may contain backslash escapes, so `\"` does not end it — the same
// quoting rules structuralParenDelta follows.
var quotedEntryRe = regexp.MustCompile(`'([^']*)'|"((?:[^"\\]|\\.)*)"`)

func extractEntries(text string) []string {
	var out []string
	for _, m := range quotedEntryRe.FindAllStringSubmatch(text, -1) {
		if strings.HasPrefix(m[0], "'") {
			out = append(out, m[1])
		} else {
			out = append(out, unescapeDoubleQuoted(m[2]))
		}
	}
	return out
}

// unescapeDoubleQuoted applies bash's double-quote escape rules: a
// backslash is removed only before $, `, " or \; any other backslash is
// kept literally ("a\nb" stays a\nb).
func unescapeDoubleQuoted(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("$`\"\\", s[i+1]) >= 0 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// structuralParenDelta counts only the "(" and ")" characters in text
// that are outside any quoted string — array syntax parens, not
// incidental parens inside a URL or other quoted content (e.g.
// source=("https://example.com/thing(v1)") contains a real closing
// array paren plus a literal, unbalanced-looking pair inside the quoted
// URL; naively counting every "(" and ")" on the line conflates the two
// and can misjudge whether a multi-line array is still open). Handles
// both single- and double-quoted spans; a backslash inside a
// double-quoted span escapes the following character (bash convention)
// so `\"` doesn't end the span early. Single-quoted spans in bash take
// everything literally, including backslashes, so no escape handling
// applies there.
func structuralParenDelta(text string) int {
	delta := 0
	var inQuote rune // 0, '\'', or '"'

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case inQuote == '"' && c == '\\' && i+1 < len(runes):
			i++ // skip the escaped character
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '\'' || c == '"':
			inQuote = c
		case c == '(':
			delta++
		case c == ')':
			delta--
		}
	}

	return delta
}

// parseOneArray parses one contiguous PKGBUILD array block beginning at
// lines[start]. It returns the parsed array plus the index of the first
// line after the block.
func parseOneArray(lines []patchLine, start int) (pkgbuildArray, int) {
	m := arrayStartRe.FindStringSubmatch(lines[start].text)
	arr := pkgbuildArray{
		Name: m[1],
		Arch: strings.TrimPrefix(m[2], "_"),
		// lines[start].newLineNo is 0 for a Removed-starting block (the
		// array doesn't exist in the new file at this point) and a real
		// line number for an Added- or Context-starting block.
		Line: lines[start].newLineNo,
	}

	collect := func(l patchLine) {
		entries := extractEntries(l.text)
		switch l.kind {
		case lineAdded:
			for _, e := range entries {
				arr.Added = append(arr.Added, pkgbuildEntry{Value: e, Line: l.newLineNo})
			}
		case lineRemoved:
			arr.Removed = append(arr.Removed, entries...)
		case lineContext:
			for _, e := range entries {
				arr.Added = append(arr.Added, pkgbuildEntry{Value: e, Line: l.newLineNo})
			}
			arr.Removed = append(arr.Removed, entries...)
		}
	}

	depth := structuralParenDelta(lines[start].text)
	collect(lines[start])

	i := start + 1
	for depth > 0 && i < len(lines) {
		depth += structuralParenDelta(lines[i].text)
		collect(lines[i])
		i++
	}

	return arr, i
}

// parseArrays scans classified lines for array-assignment blocks,
// merging a Removed-only block immediately followed by an Added-only
// block of the same array into one logical entry (see parseOneArray and
// the merge step below).
func parseArrays(lines []patchLine) []pkgbuildArray {
	var arrays []pkgbuildArray

	for i := 0; i < len(lines); {
		if arrayStartRe.FindStringSubmatch(lines[i].text) == nil {
			i++
			continue
		}

		arr, next := parseOneArray(lines, i)

		// git diff commonly renders a changed array as:
		//
		//   -array=(...)
		//   +array=(...)
		//
		// Merge those two contiguous blocks into one logical array. The
		// merged array's Line must come from the Added (second) block —
		// arr.Line from the Removed-only first block is 0 (that block
		// doesn't exist in the new file), so keeping it would silently
		// lose the location this merge is meant to preserve.
		if next < len(lines) &&
			arrayStartRe.FindStringSubmatch(lines[next].text) != nil {

			nextArr, nextNext := parseOneArray(lines, next)

			if arr.Name == nextArr.Name &&
				arr.Arch == nextArr.Arch &&
				len(arr.Added) == 0 &&
				len(nextArr.Removed) == 0 {

				arr.Added = nextArr.Added
				arr.Line = nextArr.Line
				next = nextNext
			}
		}

		arrays = append(arrays, arr)
		i = next
	}

	return arrays
}

// scalarRe captures everything after "=" (leading whitespace included,
// so stripTrailingComment can tell "x= #c" from "x=#c"); parseScalars
// trims the value after stripping any comment.
var scalarRe = regexp.MustCompile(`^\s*(pkgver|pkgrel)\s*=(.+?)\s*$`)

// stripTrailingComment removes a trailing bash comment from a scalar's
// value: a "#" that starts a word (preceded by whitespace) and is
// outside any quotes. "1.2#3", "#1" (bash assigns it literally) and
// "'1.2 # x'" are returned unchanged. Quoting follows
// structuralParenDelta.
func stripTrailingComment(val string) string {
	var inQuote byte // 0, '\'', or '"'
	for i := 0; i < len(val); i++ {
		c := val[i]
		switch {
		case inQuote == '"' && c == '\\' && i+1 < len(val):
			i++ // skip the escaped character
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '\'' || c == '"':
			inQuote = c
		case c == '#' && i > 0 && (val[i-1] == ' ' || val[i-1] == '\t'):
			return strings.TrimRight(val[:i], " \t")
		}
	}
	return val
}

func parseScalars(lines []patchLine) []pkgbuildScalar {
	byName := map[string]*pkgbuildScalar{}
	var order []string

	for _, l := range lines {
		m := scalarRe.FindStringSubmatch(l.text)
		if m == nil {
			continue
		}
		name, val := m[1], strings.TrimSpace(stripTrailingComment(m[2]))
		if val == "" {
			continue // "pkgrel= # comment": no value, same as "pkgrel="
		}
		s, ok := byName[name]
		if !ok {
			s = &pkgbuildScalar{Name: name}
			byName[name] = s
			order = append(order, name)
		}
		switch l.kind {
		case lineRemoved:
			s.Old = val
		case lineAdded:
			s.New = val
			s.NewLine = l.newLineNo
		case lineContext:
			s.Old, s.New = val, val
			s.NewLine = l.newLineNo
		}
	}

	out := make([]pkgbuildScalar, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out
}

var funcStartRe = regexp.MustCompile(`^\s*(build|package|prepare)\s*\(\s*\)\s*\{`)
var funcEndRe = regexp.MustCompile(`^\}\s*$`)

// parseFunctions extracts build()/package()/prepare() bodies. This is a
// line-based brace-matcher, not a real bash parser — it breaks on nested
// { } inside the function body (a case statement, a subshell block).
// See docs/domain/analyzer-specs.md § Known Limitations.
func parseFunctions(lines []patchLine) []pkgbuildFunction {
	var funcs []pkgbuildFunction
	var current *pkgbuildFunction

	for _, l := range lines {
		if current == nil {
			if m := funcStartRe.FindStringSubmatch(l.text); m != nil {
				current = &pkgbuildFunction{Name: m[1]}
			}
			continue
		}
		if funcEndRe.MatchString(l.text) {
			funcs = append(funcs, *current)
			current = nil
			continue
		}
		if l.kind == lineAdded {
			current.AddedLines = append(current.AddedLines, pkgbuildEntry{Value: l.text, Line: l.newLineNo})
		}
	}
	if current != nil {
		// Function's closing brace fell outside the diff's visible hunks
		// (unmodified tail of a long function). Still return what was
		// collected rather than discarding it.
		funcs = append(funcs, *current)
	}

	return funcs
}
