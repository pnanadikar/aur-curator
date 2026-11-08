package analyzer

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This file reads the arrays the analyzers check from one whole PKGBUILD,
// as text: nothing is executed or expanded. See
// docs/domain/analyzer-specs.md § PKGBUILD Parsing (#484).

// fileEntry is one entry of an array: the bash word with its quotes
// removed and expansions ($var, ${…}, $(…), `…`, braces) left as written,
// and the 1-based line the word starts on.
type fileEntry struct {
	Value string
	Line  int
}

// fileArray is every readable assignment of one label (name and arch
// suffix), in file order: `depends=(…)` in two package_*() functions and
// `source+=(…)` in an `if` all count (§ PKGBUILD Parsing rule 2).
type fileArray struct {
	Name    string      // base name: "source", "sha256sums", "depends"
	Arch    string      // arch suffix without the underscore; "" if none
	Line    int         // line of the label's first readable assignment
	Entries []fileEntry // from NAME=(…) and NAME+=(…), in order
	Indexed []indexedEntry
}

// indexedEntry is the value of an indexed assignment, NAME[N]=WORD, or
// NAME=WORD (N = 0). It is one more value position N may hold, not a
// replacement: the assignment may sit in a branch that does not run
// (§ PKGBUILD Parsing rule 6, #485).
type indexedEntry struct {
	Index int
	Entry fileEntry
}

// values returns every value the array may hold: its entries, then its
// indexed values, in file order. For checks that treat the array as a set
// (URL, Depends, PGP).
func (a fileArray) values() []fileEntry {
	out := append([]fileEntry(nil), a.Entries...)
	for _, x := range a.Indexed {
		out = append(out, x.Entry)
	}
	return out
}

// size is the number of positions: the entries, or one past the highest
// index, whichever is more.
func (a fileArray) size() int {
	n := len(a.Entries)
	for _, x := range a.Indexed {
		n = max(n, x.Index+1)
	}
	return n
}

// at returns the values position i may hold: the entry there, if any,
// then the indexed values for i, in file order. For the checksum checks,
// which compare by position.
func (a fileArray) at(i int) []fileEntry {
	var out []fileEntry
	if i < len(a.Entries) {
		out = append(out, a.Entries[i])
	}
	for _, x := range a.Indexed {
		if x.Index == i {
			out = append(out, x.Entry)
		}
	}
	return out
}

// label is the array's name as written in the PKGBUILD: "depends",
// "source_x86_64".
func (a fileArray) label() string {
	if a.Arch == "" {
		return a.Name
	}
	return a.Name + "_" + a.Arch
}

// unreadableArray is one assignment the parser could not read: no closing
// `)`, an unterminated quote or expansion, or a shell operator outside
// quotes. First is its opening line, Last the line where reading stopped.
// None of its entries are used.
type unreadableArray struct {
	Name, Arch  string
	First, Last int
}

// label is the array's name as written in the PKGBUILD.
func (u unreadableArray) label() string {
	if u.Arch == "" {
		return u.Name
	}
	return u.Name + "_" + u.Arch
}

// fileScalar is the value of a scalar (pkgver, pkgrel) at its last
// assignment: the bash word with quotes removed and expansions left as
// written, "" when the assignment has no value. Line is that
// assignment's line.
type fileScalar struct {
	Value string
	Line  int
}

// pkgbuildFile is what parsePKGBUILDFile reads from one PKGBUILD.
type pkgbuildFile struct {
	Arrays     []fileArray // by label, in order of first appearance
	Unreadable []unreadableArray
	Scalars    map[string]fileScalar // "pkgver", "pkgrel"; absent if never assigned
}

// arraysNamed returns every label of base name name (any arch suffix),
// in order of first appearance.
func (f pkgbuildFile) arraysNamed(name string) []fileArray {
	var out []fileArray
	for _, a := range f.Arrays {
		if a.Name == name {
			out = append(out, a)
		}
	}
	return out
}

// array returns the array with this label (name and arch suffix).
func (f pkgbuildFile) array(name, arch string) (fileArray, bool) {
	for _, a := range f.Arrays {
		if a.Name == name && a.Arch == arch {
			return a, true
		}
	}
	return fileArray{}, false
}

// fileAssignRe matches an assignment to one of the arrays the analyzers
// read, or to a scalar they read, up to its "=". Group 1: name. Group 2:
// arch suffix with its underscore. Group 3: index with its brackets.
// Group 4: "+" for an append.
var fileAssignRe = regexp.MustCompile(
	`(source|validpgpkeys|depends|makedepends|checkdepends|optdepends|(?:md5|sha1|sha224|sha256|sha384|sha512|b2|ck)sums|pkgver|pkgrel)(_[A-Za-z0-9_]+)?(\[[^\]\n]*\])?(\+?)=`)

// fileScalars are the scalars parsePKGBUILDFile reads (§ PKGBUILD
// Parsing rule 7).
var fileScalars = map[string]bool{"pkgver": true, "pkgrel": true}

// assignment is one match of fileAssignRe that starts a command.
type assignment struct {
	nameStart int    // offset of the name
	valueAt   int    // offset just after "="
	name      string // base name
	arch      string // arch suffix without its underscore
	index     string // text between the brackets; "" without an index
	indexed   bool   // brackets were written
	plus      bool   // "+=" rather than "="
}

// parsePKGBUILDFile reads every assignment to the analyzers' arrays and
// scalars from text (§ PKGBUILD Parsing). An assignment counts where a
// command can start: at the start of a line, or after `;`, `&&`, `||`,
// `{`, `(`, `then`, `else` or `do` on the same line, outside quotes and
// comments. Heredocs and strings spanning lines are not tracked (§ Known
// Limitations).
func parsePKGBUILDFile(text string) pkgbuildFile {
	r := fileReader{
		p:     fileParser{text: text, lineStarts: lineStartOffsets(text)},
		index: map[string]int{},
		out:   pkgbuildFile{Scalars: map[string]fileScalar{}},
	}

	off := 0
	for off < len(text) {
		lineEnd := strings.IndexByte(text[off:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += off
		}
		a, found := r.p.findAssignment(off, lineEnd)
		if !found {
			off = lineEnd + 1
			continue
		}
		end, ok := r.read(a)
		if ok {
			off = end // the same line may hold another assignment
			continue
		}
		// Resume on the line after the one reading stopped on: the rest
		// of a broken assignment is not trusted to hold assignments.
		next := strings.IndexByte(text[end:], '\n')
		if next < 0 {
			break
		}
		off = end + next + 1
	}
	return r.out
}

// fileReader collects what parsePKGBUILDFile reads.
type fileReader struct {
	p     fileParser
	index map[string]int // label → position in out.Arrays
	out   pkgbuildFile
}

// read applies one assignment and returns the offset after it. ok is
// false when it was unreadable (recorded for arrays); end is then where
// reading stopped.
func (r *fileReader) read(a assignment) (end int, ok bool) {
	t := r.p.text
	paren := a.valueAt < len(t) && t[a.valueAt] == '('

	if fileScalars[a.name] {
		if a.arch != "" || a.indexed || a.plus || paren {
			return a.valueAt, true // another variable, or not a scalar value
		}
		value, end, ok := r.p.scanWord(a.valueAt)
		if ok {
			r.out.Scalars[a.name] = fileScalar{Value: value, Line: r.p.lineOf(a.nameStart)}
		}
		return end, ok
	}

	switch {
	case paren && !a.indexed:
		entries, end, ok := r.p.scanArray(a.valueAt + 1)
		if !ok {
			return r.unreadable(a, end)
		}
		arr := r.label(a)
		arr.Entries = append(arr.Entries, entries...)
		return end, true
	case paren, a.plus:
		// NAME[N]=(…) is not bash; NAME+=WORD appends to element 0's
		// text, which the parser does not model (rule 6).
		return r.unreadable(a, a.valueAt)
	}

	// NAME[N]=WORD, or NAME=WORD, which bash reads as NAME[0]=WORD.
	n := 0
	if a.indexed {
		var err error
		if n, err = strconv.Atoi(a.index); err != nil || n < 0 || strings.TrimLeft(a.index, "0123456789") != "" {
			return r.unreadable(a, a.valueAt)
		}
	}
	value, end, ok := r.p.scanWord(a.valueAt)
	if !ok {
		return r.unreadable(a, end)
	}
	// One more value position n may hold, the entry already there kept:
	// the assignment may be in a branch that does not run, as in a real
	// PKGBUILD's `if (( _manual_clone )); then source[0]=…`, and replacing
	// would hide the URL and checksum it overrides (rule 6). Any index is
	// fine, sparse ones included (depends[125]=…).
	arr := r.label(a)
	arr.Indexed = append(arr.Indexed, indexedEntry{Index: n, Entry: fileEntry{Value: value, Line: r.p.lineOf(a.nameStart)}})
	return end, true
}

// label returns the array for a's label, adding it at a's line if this is
// its first readable assignment.
func (r *fileReader) label(a assignment) *fileArray {
	key := a.name + "_" + a.arch
	i, seen := r.index[key]
	if !seen {
		i = len(r.out.Arrays)
		r.index[key] = i
		r.out.Arrays = append(r.out.Arrays, fileArray{Name: a.name, Arch: a.arch, Line: r.p.lineOf(a.nameStart)})
	}
	return &r.out.Arrays[i]
}

// unreadable records a as unreadable, from its line to the line of end.
func (r *fileReader) unreadable(a assignment, end int) (int, bool) {
	r.out.Unreadable = append(r.out.Unreadable, unreadableArray{
		Name: a.name, Arch: a.arch,
		First: r.p.lineOf(a.nameStart), Last: r.p.lastLine(end),
	})
	return end, false
}

// fileParser scans one PKGBUILD's text.
type fileParser struct {
	text       string
	lineStarts []int
}

func lineStartOffsets(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineOf is the 1-based line of byte offset off.
func (p fileParser) lineOf(off int) int {
	return sort.Search(len(p.lineStarts), func(i int) bool { return p.lineStarts[i] > off })
}

// lastLine is the line reading stopped on, for an unreadable array: the
// line of end, or the file's last line when end is the end of the file.
func (p fileParser) lastLine(end int) int {
	if end >= len(p.text) && end > 0 {
		end = len(p.text) - 1
	}
	return p.lineOf(end)
}

// findAssignment returns the first assignment to an analyzer array or
// scalar that starts a command within text[from:lineEnd].
func (p fileParser) findAssignment(from, lineEnd int) (assignment, bool) {
	lineStart := p.lineStarts[p.lineOf(from)-1]
	line := p.text[lineStart:lineEnd]
	for _, m := range fileAssignRe.FindAllStringSubmatchIndex(line, -1) {
		at := lineStart + m[0]
		if at < from || !startsCommand(line[:m[0]]) {
			continue
		}
		a := assignment{
			nameStart: at,
			valueAt:   lineStart + m[1],
			name:      line[m[2]:m[3]],
			plus:      m[9] > m[8],
		}
		if m[4] >= 0 {
			a.arch = line[m[4]+1 : m[5]]
		}
		if m[6] >= 0 {
			a.indexed = true
			a.index = line[m[6]+1 : m[7]-1]
		}
		return a, true
	}
	return assignment{}, false
}

// startsCommand reports whether a command can start right after prefix,
// the text before a candidate on its line: prefix is blank, or ends,
// outside quotes and comments, with a command separator or one of the
// keywords then, else, do.
func startsCommand(prefix string) bool {
	if !outsideQuotes(prefix) {
		return false
	}
	t := strings.TrimRight(prefix, " \t")
	if t == "" {
		return true
	}
	if len(t) == len(prefix) {
		// No space before the name: only an operator may touch it
		// ("_source=(" or "mysource=(" is another array).
		return strings.ContainsAny(t[len(t)-1:], ";&|{(")
	}
	if strings.ContainsAny(t[len(t)-1:], ";&|{(") {
		return true
	}
	for _, kw := range []string{"then", "else", "do"} {
		if strings.HasSuffix(t, kw) {
			before := t[:len(t)-len(kw)]
			if before == "" || strings.ContainsAny(before[len(before)-1:], " \t;") {
				return true
			}
		}
	}
	return false
}

// outsideQuotes reports whether the end of s, a line prefix, is outside
// quotes and comments.
func outsideQuotes(s string) bool {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case quote == '"':
			switch c {
			case '\\':
				i++
			case '"':
				quote = 0
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return false
		}
	}
	return quote == 0
}

// operatorChars end a word and are a shell operator outside quotes; inside
// an array they make it unreadable (§ PKGBUILD Parsing rule 5). ")" is
// handled separately: it ends the array.
const operatorChars = "(;&|<>"

// scanArray reads the words of an array from off (just after its "(") to
// the unquoted ")". It returns the entries and the offset just after ")".
// ok is false when the array is unreadable; end is then where reading
// stopped.
func (p fileParser) scanArray(off int) (entries []fileEntry, end int, ok bool) {
	t := p.text
	i := off
	for i < len(t) {
		c := t[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\\' && i+1 < len(t) && t[i+1] == '\n':
			i += 2 // line continuation between words
		case c == '#':
			nl := strings.IndexByte(t[i:], '\n')
			if nl < 0 {
				return entries, len(t), false
			}
			i += nl
		case c == ')':
			return entries, i + 1, true
		case strings.IndexByte(operatorChars, c) >= 0:
			return entries, i, false
		default:
			value, next, wordOK := p.scanWord(i)
			if !wordOK {
				return entries, next, false
			}
			entries = append(entries, fileEntry{Value: value, Line: p.lineOf(i)})
			i = next
		}
	}
	return entries, len(t), false
}

// scanWord reads one bash word starting at off: unquoted text, '…', "…",
// backslash escapes, and $(…), ${…}, $'…' and `…` kept as written. It
// stops before unquoted whitespace, ")" or another operator. ok is false
// for an unterminated quote or expansion, or a backslash at the end of
// the file; next is then where reading stopped.
func (p fileParser) scanWord(off int) (value string, next int, ok bool) {
	t := p.text
	var b strings.Builder
	i := off
	for i < len(t) {
		c := t[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ')' || strings.IndexByte(operatorChars, c) >= 0:
			return b.String(), i, true
		case c == '\'':
			j := strings.IndexByte(t[i+1:], '\'')
			if j < 0 {
				return "", len(t), false
			}
			b.WriteString(t[i+1 : i+1+j])
			i += j + 2
		case c == '"':
			j, closed := p.scanDoubleQuoted(i+1, &b)
			if !closed {
				return "", j, false
			}
			i = j
		case c == '\\':
			if i+1 >= len(t) {
				return "", len(t), false
			}
			if t[i+1] != '\n' { // backslash-newline joins the word's lines
				b.WriteByte(t[i+1])
			}
			i += 2
		case c == '$' || c == '`':
			j, closed := p.scanExpansion(i)
			if !closed {
				return "", j, false
			}
			b.WriteString(t[i:j])
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), i, true
}

// scanDoubleQuoted reads a "…" span whose content starts at off, writing
// its value to b: a backslash escapes $ ` " \ and newline (removed),
// expansions are kept as written. It returns the offset after the
// closing quote.
func (p fileParser) scanDoubleQuoted(off int, b *strings.Builder) (int, bool) {
	t := p.text
	i := off
	for i < len(t) {
		c := t[i]
		switch {
		case c == '"':
			return i + 1, true
		case c == '\\' && i+1 < len(t):
			switch t[i+1] {
			case '\n':
			case '$', '`', '"', '\\':
				b.WriteByte(t[i+1])
			default:
				b.WriteByte('\\')
				b.WriteByte(t[i+1])
			}
			i += 2
		case c == '$' || c == '`':
			j, ok := p.scanExpansion(i)
			if !ok {
				return j, false
			}
			b.WriteString(t[i:j])
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return len(t), false
}

// scanExpansion reads an expansion starting at off ("$" or "`") and
// returns the offset after it: $(…) and $((…)) with balanced parentheses,
// ${…} with balanced braces, both quote-aware; $'…' with backslash
// escapes; `…` to the next unescaped backtick. A "$" followed by anything
// else is one character ($pkgver: the name is read as ordinary text).
func (p fileParser) scanExpansion(off int) (int, bool) {
	t := p.text
	if t[off] == '`' {
		for i := off + 1; i < len(t); i++ {
			switch t[i] {
			case '\\':
				i++
			case '`':
				return i + 1, true
			}
		}
		return len(t), false
	}
	if off+1 >= len(t) {
		return off + 1, true
	}
	switch t[off+1] {
	case '(':
		return p.scanBalanced(off+2, '(', ')')
	case '{':
		return p.scanBalanced(off+2, '{', '}')
	case '\'':
		for i := off + 2; i < len(t); i++ {
			switch t[i] {
			case '\\':
				i++
			case '\'':
				return i + 1, true
			}
		}
		return len(t), false
	}
	return off + 1, true
}

// scanBalanced reads from off, just inside an opening bracket, to its
// matching close, skipping quoted text and nested expansions, and returns
// the offset after the close.
func (p fileParser) scanBalanced(off int, openCh, closeCh byte) (int, bool) {
	t := p.text
	depth := 1
	i := off
	for i < len(t) {
		c := t[i]
		switch {
		case c == '\\':
			i += 2
		case c == '\'' && openCh == '(':
			j := strings.IndexByte(t[i+1:], '\'')
			if j < 0 {
				return len(t), false
			}
			i += j + 2
		case c == '"':
			var discard strings.Builder
			j, ok := p.scanDoubleQuoted(i+1, &discard)
			if !ok {
				return j, false
			}
			i = j
		case c == '$' || c == '`':
			j, ok := p.scanExpansion(i)
			if !ok {
				return j, false
			}
			i = j
		case c == openCh:
			depth++
			i++
		case c == closeCh:
			depth--
			i++
			if depth == 0 {
				return i, true
			}
		default:
			i++
		}
	}
	return len(t), false
}
