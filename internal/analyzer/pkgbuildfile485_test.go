package analyzer

import (
	"strings"
	"testing"
)

// TestParsePKGBUILDFileIndexed485: NAME[N]=WORD, and NAME=WORD (N = 0),
// add one more value position N may hold; the entry there is kept. A
// non-literal index, NAME+=WORD and NAME[N]=(…) are unreadable
// (§ PKGBUILD Parsing rule 6).
func TestParsePKGBUILDFileIndexed485(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"value added at its index, entry kept", "sha512sums=('h0' 'h1' 'h2')\nsha512sums[1]='SKIP'",
			"sha512sums@1: h0@1|h1@1|h2@1 +[1]SKIP@2"},
		{"conditional override kept beside the entry (ungoogled-chromium)",
			"source=(https://a.example/x.tar.xz 'local.patch')\nif (( _manual_clone )); then\n  source[0]=fetch-release\nfi",
			"source@1: https://a.example/x.tar.xz@1|local.patch@1 +[0]fetch-release@3"},
		{"index 0 creates the label", "depends[0]=glibc", "depends@1:  +[0]glibc@1"},
		{"NAME=WORD is NAME[0]=WORD", "source=('a' 'b')\nsource=c", "source@1: a@1|b@1 +[0]c@2"},
		{"leading zeros are a literal index", "depends=(a b)\ndepends[01]=c", "depends@1: a@1|b@1 +[1]c@2"},
		{"sparse index (real idiom)", "depends=(a)\ndepends[125]=gnome-shell", "depends@1: a@1 +[125]gnome-shell@2"},
		{"arch label", "sha256sums_x86_64=('h0' 'h1')\nsha256sums_x86_64[0]=\"$(fetch)\"",
			"sha256sums_x86_64@1: h0@1|h1@1 +[0]$(fetch)@2"},
		{"inside a function, after &&", "prepare() {\n  true && sha256sums[0]=SKIP\n}\nsha256sums=(h0)",
			"sha256sums@2: h0@4 +[0]SKIP@2"},
		{"empty value", "md5sums=('h0')\nmd5sums[0]=", "md5sums@1: h0@1 +[0]@2"},
		{"several on one line", "depends=(a b); depends[0]=x; depends[1]=y", "depends@1: a@1|b@1 +[0]x@1 +[1]y@1"},
		{"spotify's form", "sha512sums=('h0' 'h1' 'h2' 'h3' 'h4')\nsha512sums[4]='SKIP'  # verified in prepare()",
			"sha512sums@1: h0@1|h1@1|h2@1|h3@1|h4@1 +[4]SKIP@2"},

		{"variable index", "depends=(a)\ndepends[$i]=b", "depends@1: a@1\n!depends 2-2"},
		{"negative index", "depends=(a)\ndepends[-1]=b", "depends@1: a@1\n!depends 2-2"},
		{"empty index", "depends=(a)\ndepends[]=b", "depends@1: a@1\n!depends 2-2"},
		{"+= without parentheses", "source=('a')\nsource+=b", "source@1: a@1\n!source 2-2"},
		{"indexed +=", "source=('a')\nsource[0]+=b", "source@1: a@1\n!source 2-2"},
		{"indexed with parentheses", "source=('a')\nsource[0]=(b)", "source@1: a@1\n!source 2-2"},
		{"unterminated value", "source=('a')\nsource[0]='b\n", "source@1: a@1\n!source 2-2"},
		{"next line still read", "depends=(a)\ndepends[$i]=b\nmakedepends=(c)",
			"depends@1: a@1\nmakedepends@3: c@3\n!depends 2-2"},
		{"look-alikes are not assignments", "local depends=x\necho source[0]=y\nfoo=\"md5sums[0]=z\"", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := render484(parsePKGBUILDFile(tc.text)); got != tc.want {
				t.Fatalf("parse of\n%s\ngot\n%s\nwant\n%s", tc.text, got, tc.want)
			}
		})
	}
}

// TestFileArrayPositions485: values lists entries then indexed values;
// size and at give the positions the checksum checks compare.
func TestFileArrayPositions485(t *testing.T) {
	a, _ := parsePKGBUILDFile("sums=(x)\nsha256sums=('h0' 'h1')\nsha256sums[1]=SKIP\nsha256sums[3]=late").array("sha256sums", "")
	var vs []string
	for _, e := range a.values() {
		vs = append(vs, e.Value)
	}
	if got := strings.Join(vs, " "); got != "h0 h1 SKIP late" {
		t.Fatalf("values = %q", got)
	}
	if a.size() != 4 {
		t.Fatalf("size = %d, want 4", a.size())
	}
	for i, want := range []string{"h0", "h1 SKIP", "", "late"} {
		var got []string
		for _, e := range a.at(i) {
			got = append(got, e.Value)
		}
		if strings.Join(got, " ") != want {
			t.Fatalf("at(%d) = %q, want %q", i, strings.Join(got, " "), want)
		}
	}
}

// TestParsePKGBUILDFileScalars485: pkgver and pkgrel at their last
// assignment, as bash words: quotes removed, expansions kept, a trailing
// comment dropped (#24, § PKGBUILD Parsing rule 7).
func TestParsePKGBUILDFileScalars485(t *testing.T) {
	for _, tc := range []struct {
		name, text   string
		pkgver       string
		line         int
		pkgverAbsent bool
	}{
		{name: "unquoted", text: "pkgver=1.2.3", pkgver: "1.2.3", line: 1},
		{name: "single quotes removed (#487 D12)", text: "pkgver='15.0.26'", pkgver: "15.0.26", line: 1},
		{name: "double quotes removed", text: `pkgver="16.0"`, pkgver: "16.0", line: 1},
		{name: "trailing comment dropped (#24)", text: "pkgver=1.2.4\t# bumped", pkgver: "1.2.4", line: 1},
		{name: "# inside a word is the value (#24)", text: "pkgver=1.2#3", pkgver: "1.2#3", line: 1},
		{name: "# right after = is the value (#24)", text: "pkgver=#1", pkgver: "#1", line: 1},
		{name: "# inside quotes is the value (#24)", text: "pkgver='1.2 # x'", pkgver: "1.2 # x", line: 1},
		{name: "escaped quote (#24)", text: `pkgver="a\" # b" #c`, pkgver: `a" # b`, line: 1},
		{name: "comment-only value is empty (#24)", text: "pkgver= # set below", pkgver: "", line: 1},
		{name: "space after = is empty, as in bash", text: "pkgver= 1.2", pkgver: "", line: 1},
		{name: "expansion kept as written", text: "_v=1-2\npkgver=${_v/-/.}", pkgver: "${_v/-/.}", line: 2},
		{name: "command substitution kept", text: "pkgver=$(cat VERSION)", pkgver: "$(cat VERSION)", line: 1},
		{name: "last assignment wins", text: "pkgver=1\nif true; then\n  pkgver=2\nfi", pkgver: "2", line: 3},
		{name: "pkgver() function is not an assignment", text: "pkgver=1\npkgver() {\n  echo 2\n}", pkgver: "1", line: 1},
		{name: "other variables are not pkgver", text: "_pkgver=9\npkgver_x=8\nlocal pkgver=7", pkgverAbsent: true},
		{name: "not assigned", text: "pkgname=x", pkgverAbsent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePKGBUILDFile(tc.text).Scalars["pkgver"]
			if tc.pkgverAbsent {
				if ok {
					t.Fatalf("pkgver = %+v, want absent", got)
				}
				return
			}
			if !ok || got.Value != tc.pkgver || got.Line != tc.line {
				t.Fatalf("pkgver = %+v (present %v), want %q at line %d", got, ok, tc.pkgver, tc.line)
			}
		})
	}
	f := parsePKGBUILDFile("pkgver=1.0\npkgrel=2 # rebuild")
	if r := f.Scalars["pkgrel"]; r.Value != "2" || r.Line != 2 {
		t.Fatalf("pkgrel = %+v, want 2 at line 2", r)
	}
	if strings.Contains(render484(f), "pkgver") {
		t.Fatal("scalars must not be read as arrays")
	}
}

// TestIndexedValuesReachTheChecks485: an indexed value counts in the
// set-based checks, and the entry it would override still counts too.
func TestIndexedValuesReachTheChecks485(t *testing.T) {
	chromium := "source=(https://a.example/chromium-1.tar.xz 'local.patch')\n" +
		"if (( _manual_clone )); then\n  source[0]=https://b.example/fetch.py\nfi"
	got := urlFindings484(t, "", chromium)
	want := "New source URL: https://a.example/chromium-1.tar.xz @ PKGBUILD:1\n" +
		"New source URL: https://b.example/fetch.py @ PKGBUILD:3"
	if strings.Join(got, "\n") != want {
		t.Fatalf("url_new on Import\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}

	deps := runDepends(t, "depends=(a)", "depends=(a)\ndepends[125]=gnome-shell")
	if len(deps) != 1 || deps[0].Summary != "Dependencies added (depends): gnome-shell" {
		t.Fatalf("depends_added = %v", deps)
	}

	const keyA, keyB = "AAAAAAAAAAAAAAAA", "BBBBBBBBBBBBBBBB"
	pgp := runChecksumFiles(t, "validpgpkeys=('"+keyA+"')", "validpgpkeys=('"+keyA+"')\nvalidpgpkeys[1]="+keyB)
	if countCategory(pgp, CategoryPGPKeyChanged) != 1 {
		t.Fatalf("pgp_key_changed missing: %v", pgp)
	}
}
