package analyzer

import (
	"fmt"
	"strings"
	"testing"
)

// render484 writes what parsePKGBUILDFile read, one line per label
// ("label@line: value@line|value@line", then " +[N]value@line" per
// indexed value, #485) and per unreadable assignment ("!label
// first-last"), so a case states the whole result.
func render484(f pkgbuildFile) string {
	var lines []string
	for _, a := range f.Arrays {
		var vs []string
		for _, e := range a.Entries {
			vs = append(vs, fmt.Sprintf("%s@%d", e.Value, e.Line))
		}
		line := fmt.Sprintf("%s@%d: %s", a.label(), a.Line, strings.Join(vs, "|"))
		for _, x := range a.Indexed {
			line += fmt.Sprintf(" +[%d]%s@%d", x.Index, x.Entry.Value, x.Entry.Line)
		}
		lines = append(lines, line)
	}
	for _, u := range f.Unreadable {
		lines = append(lines, fmt.Sprintf("!%s %d-%d", u.label(), u.First, u.Last))
	}
	return strings.Join(lines, "\n")
}

func TestParsePKGBUILDFile484(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		// Words
		{"quoted", `depends=('a' "b")`, "depends@1: a@1|b@1"},
		{"unquoted", `depends=(glibc gtk3)`, "depends@1: glibc@1|gtk3@1"},
		{"segments make one word", `source=("x.tar.gz"::"https://a.example/x")`,
			"source@1: x.tar.gz::https://a.example/x@1"},
		{"double-quote escapes", `source=("a\"b" "c\$d" "e\xf" "g\\h")`,
			`source@1: a"b@1|c$d@1|e\xf@1|g\h@1`},
		{"backslash outside quotes", `depends=(a\ b c\(d\))`, "depends@1: a b@1|c(d)@1"},
		{"continuation between words", "depends=('a' \\\n  'b')", "depends@1: a@1|b@2"},
		{"continuation inside a word", "depends=(fo\\\no)", "depends@1: foo@1"},
		{"continuation inside double quotes", "depends=(\"fo\\\no\")", "depends@1: foo@1"},
		{"comment with quote and paren", "depends=('a'  # don't (yet)\n         'b')", "depends@1: a@1|b@2"},
		{"comment on its own line", "optdepends=('a: x'\n  # https://example.com/x\n  'b: y'\n)", "optdepends@1: a: x@1|b: y@3"},
		{"# inside a word is text", `source=(git+https://a.example/y.git#tag=v1)`,
			"source@1: git+https://a.example/y.git#tag=v1@1"},
		{"$var and ${…} kept", `source=("$pkgname-${pkgver//./_}.tar.gz::$url/x")`,
			"source@1: $pkgname-${pkgver//./_}.tar.gz::$url/x@1"},
		{"$(…) with ) | and quotes inside", `sha256sums=("$(curl -s https://a.example/sum | cut -d')' -f1)")`,
			`sha256sums@1: $(curl -s https://a.example/sum | cut -d')' -f1)@1`},
		{"unquoted $(…)", `sha256sums=($(echo a) b)`, "sha256sums@1: $(echo a)@1|b@1"},
		{"backticks", "depends=(`echo a`)", "depends@1: `echo a`@1"},
		{"$'…'", `depends=($'a\tb')`, `depends@1: $'a\tb'@1`},
		{"braces kept as written", `depends=(qt5-{base,svg} x)`, "depends@1: qt5-{base,svg}@1|x@1"},
		{"brace expansion suffix", `source=("https://a.example/x.tar.gz"{,.sig})`,
			"source@1: https://a.example/x.tar.gz{,.sig}@1"},
		{"empty array", `depends=()`, "depends@1: "},
		{"empty strings are entries", `md5sums=('' "")`, "md5sums@1: @1|@1"},

		// Positions
		{"closing ) on its own line", "depends=(\n  'a'\n  'b'\n)", "depends@1: a@2|b@3"},
		{"entry on the closing line", "sha256sums=('h1'\n            'SKIP')", "sha256sums@1: h1@1|SKIP@2"},
		{"40-line array", "depends=(\n" + strings.Repeat("  x\n", 39) + "  last)",
			"depends@1: " + strings.TrimSuffix(lines484("x", 2, 40), "|") + "|last@41"},
		{"line numbers after other code", "pkgname=x\n\nbuild() {\n  make\n}\n\nsource=('a')", "source@7: a@7"},

		// Assignments
		{"+= appends in file order", "source=('a')\nsource+=('b')", "source@1: a@1|b@2"},
		{"split package functions", "package_a() {\n  depends=('x')\n}\npackage_b() {\n  depends=('y')\n}",
			"depends@2: x@2|y@5"},
		{"inside if", "if [[ $CARCH == x86_64 ]]; then\n  source+=('u')\nfi", "source@2: u@2"},
		{"after && on the same line", "((X)) && makedepends+=(a b)", "makedepends@1: a@1|b@1"},
		{"after then on the same line", "if true; then depends=(a); fi", "depends@1: a@1"},
		{"two on one line", "depends=(a); makedepends=(b)", "depends@1: a@1\nmakedepends@1: b@1"},
		{"arch suffixes are their own labels", "source=('a')\nsource_x86_64=('b')\nsha256sums_x86_64=('h')",
			"source@1: a@1\nsource_x86_64@2: b@2\nsha256sums_x86_64@3: h@3"},
		{"assigned twice", "depends=('a')\ndepends=('a' 'b')", "depends@1: a@1|a@2|b@2"},
		{"other arrays and look-alikes are not read",
			"arch=('x86_64')\n_source=('x')\nnoextract=('y')\nmysource=('z')\necho depends=(w)\n# depends=(v)\nfoo=\"depends=(u)\"\n" +
				"foo=\"a; depends=(t)\"\nbar='b && depends=(s)'\ntrue # x; depends=(r)",
			""},
		{"heredoc line is read (documented limitation)", "cat <<EOF\ndepends=(x)\nEOF", "depends@2: x@2"},

		// Unreadable
		{"no closing paren", "depends=('a'\n  'b'\n", "!depends 1-2"},
		{"unterminated quote", "source=('a\nmakedepends=(b)\n", "!source 1-2"},
		{"unterminated $(", "source=($(echo a\n", "!source 1-1"},
		{"operator, then the next line is read", "depends=(a; b)\nmakedepends=(c)", "makedepends@2: c@2\n!depends 1-1"},
		{"bare ( inside", "depends=(a (b))", "!depends 1-1"},
		{"unquoted >", "depends=(foo>=1.2)", "!depends 1-1"},
		{"unquoted <", "depends=(foo<2)", "!depends 1-1"},
		{"unquoted |", "depends=(a|b)", "!depends 1-1"},
		{"unreadable beside a readable assignment of the label",
			"depends=('a')\ndepends+=(b; c)\ndepends+=('d')", "depends@1: a@1|d@3\n!depends 2-2"},
		{"stops where reading failed, multi-line", "source=('a'\n  'b'\n  c&d\n  'e')\nsha256sums=('h')",
			"sha256sums@5: h@5\n!source 1-3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := render484(parsePKGBUILDFile(tc.text)); got != tc.want {
				t.Fatalf("parse of\n%s\ngot\n%s\nwant\n%s", tc.text, got, tc.want)
			}
		})
	}
}

// lines484 renders value@line for lines from..to.
func lines484(value string, from, to int) string {
	var b strings.Builder
	for l := from; l <= to; l++ {
		fmt.Fprintf(&b, "%s@%d|", value, l)
	}
	return b.String()
}

// TestParsePKGBUILDFileLookups484: arraysNamed returns every arch of a
// name, array one label.
func TestParsePKGBUILDFileLookups484(t *testing.T) {
	f := parsePKGBUILDFile("source=('a')\nsource_x86_64=('b')\ndepends=('c')")
	if got := len(f.arraysNamed("source")); got != 2 {
		t.Fatalf("arraysNamed(source) = %d arrays, want 2", got)
	}
	a, ok := f.array("source", "x86_64")
	if !ok || len(a.Entries) != 1 || a.Entries[0].Value != "b" {
		t.Fatalf("array(source, x86_64) = %+v, %v", a, ok)
	}
	if _, ok := f.array("validpgpkeys", ""); ok {
		t.Fatal("array(validpgpkeys) found in a file without it")
	}
}

// TestParsePKGBUILDFileEmpty484: an empty file (Import's old side) reads
// as no arrays.
func TestParsePKGBUILDFileEmpty484(t *testing.T) {
	if f := parsePKGBUILDFile(""); len(f.Arrays) != 0 || len(f.Unreadable) != 0 {
		t.Fatalf("empty file parsed as %+v", f)
	}
}
