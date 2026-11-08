package analyzer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

func runDepends(t *testing.T, old, new string) []domain.Finding {
	t.Helper()
	findings, err := (&dependsAnalyzer{}).Run(context.Background(), loaded484(old, new))
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

// #476: depends_added, one finding per dependency array that gains an
// entry, naming the added entries as written. #484: the old and new
// PKGBUILD are compared whole.
func TestDependsAdded(t *testing.T) {
	type want struct{ summary, location string }
	long := func(extra string) string {
		var b strings.Builder
		b.WriteString("depends=(\n")
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "  'lib%02d'\n", i)
			if i == 12 {
				b.WriteString(extra)
			}
		}
		b.WriteString(")\n")
		return b.String()
	}
	tests := []struct {
		name     string
		old, new string
		want     []want
	}{
		{
			name: "depends gains one",
			old:  "depends=('glibc')", new: "depends=('glibc' 'curl')",
			want: []want{{"Dependencies added (depends): curl", "PKGBUILD:1"}},
		},
		{
			name: "makedepends, checkdepends, optdepends each",
			old:  "makedepends=('go')\ncheckdepends=()\noptdepends=()",
			new:  "makedepends=('go' 'git')\ncheckdepends=('python')\noptdepends=('foo: for bar')",
			want: []want{
				{"Dependencies added (makedepends): git", "PKGBUILD:1"},
				{"Dependencies added (checkdepends): python", "PKGBUILD:2"},
				{"Dependencies added (optdepends): foo", "PKGBUILD:3"},
			},
		},
		{
			name: "optdepends named by package, descriptions left out",
			old:  "optdepends=()",
			new:  "optdepends=('bubblewrap-suid: works on linux-hardened' 'shellcheck: checks PKGBUILD scripts' 'git')",
			want: []want{{"Dependencies added (optdepends): bubblewrap-suid, shellcheck, git", "PKGBUILD:1"}},
		},
		{
			name: "arch suffix in the label",
			old:  "depends_x86_64=('lib32-a')", new: "depends_x86_64=('lib32-a' 'lib32-b')",
			want: []want{{"Dependencies added (depends_x86_64): lib32-b", "PKGBUILD:1"}},
		},
		{name: "version constraint changed: not added", old: "depends=('foo>=1.2')", new: "depends=('foo>=1.3')"},
		{name: "constraint added to an existing dependency: not added", old: "depends=('foo')", new: "depends=('foo<2')"},
		{name: "optdepends description changed: not added", old: "optdepends=('foo: old text')", new: "optdepends=('foo: new text')"},
		{name: "removed only: not flagged", old: "depends=('a' 'b')", new: "depends=('a')"},
		{
			name: "several added, multi-line",
			old:  "depends=(\n  'a'\n  'd'\n)",
			new:  "depends=(\n  'a'\n  'b>=2'\n  'c'\n  'd'\n)",
			want: []want{{"Dependencies added (depends): b>=2, c", "PKGBUILD:3"}},
		},
		{name: "unchanged array beside other changes: none", old: "depends=('a' 'b')\npkgver=1", new: "depends=('a' 'b')\npkgrel=2\npkgver=1"},
		{name: "reordered and reformatted: none", old: "depends=('a' 'b')", new: "depends=(\n  b\n  \"a\"\n)"},
		{
			name: "split package: every package_*() assignment of the label together",
			old:  "package_a() {\n  depends=('x')\n}\npackage_b() {\n  depends=('z')\n}",
			new:  "package_a() {\n  depends=('x' 'y')\n}\npackage_b() {\n  depends=('z' 'w')\n}",
			want: []want{{"Dependencies added (depends): y, w", "PKGBUILD:2"}},
		},
		{
			name: "arch-suffixed and plain arrays are separate",
			old:  "depends=('a')\ndepends_x86_64=('b')",
			new:  "depends=('a' 'c')\ndepends_x86_64=('b' 'd')",
			want: []want{
				{"Dependencies added (depends): c", "PKGBUILD:1"},
				{"Dependencies added (depends_x86_64): d", "PKGBUILD:2"},
			},
		},
		{name: "not a dependency array: none", old: "provides=('a')", new: "provides=('a' 'b')"},
		{
			name: "Import: every dependency named",
			old:  "", new: "pkgname=foo\ndepends=('glibc' 'curl>=8')\nmakedepends=('go')",
			want: []want{
				{"Dependencies added (depends): glibc, curl>=8", "PKGBUILD:2"},
				{"Dependencies added (makedepends): go", "PKGBUILD:3"},
			},
		},
		// #487 cases
		{
			name: "unquoted (#487 Q2)",
			old:  "depends=(glibc gtk3)", new: "depends=(glibc gtk3 evil)",
			want: []want{{"Dependencies added (depends): evil", "PKGBUILD:1"}},
		},
		{
			name: "brace expansion compared as written (#487 Q6)",
			old:  "depends=(qt5-{base,svg})", new: "depends=(qt5-{base,svg,evil})",
			want: []want{{"Dependencies added (depends): qt5-{base,svg,evil}", "PKGBUILD:1"}},
		},
		{
			name: "on a changed closing line (#487 P2)",
			old:  "depends=('a'\n         'b')", new: "depends=('a'\n         'b' 'evil')",
			want: []want{{"Dependencies added (depends): evil", "PKGBUILD:2"}},
		},
		{
			name: "deep inside a long array (#484, #487 P4)",
			old:  long(""), new: long("  'evil'\n"),
			want: []want{{"Dependencies added (depends): evil", "PKGBUILD:15"}},
		},
		{
			name: "comment, quote and paren inside the array (#487 S4, S5)",
			old:  "optdepends=('a: x'  # don't (yet)\n            'b: y')",
			new:  "optdepends=('a: x'  # don't (yet)\n            'evil: z'\n            'b: y')",
			want: []want{{"Dependencies added (optdepends): evil", "PKGBUILD:2"}},
		},
		{
			name: "backslash continuation (#487 S6)",
			old:  "depends=('a' \\\n  'b')", new: "depends=('a' \\\n  'b' 'evil')",
			want: []want{{"Dependencies added (depends): evil", "PKGBUILD:2"}},
		},
		{
			name: "assigned twice (#487 S10)",
			old:  "depends=('a')", new: "depends=('a')\ndepends=('a' 'evil')",
			want: []want{{"Dependencies added (depends): evil", "PKGBUILD:2"}},
		},
		{
			name: "+= after && on one line",
			old:  "makedepends=(cmake)", new: "makedepends=(cmake)\n((CUDA)) && makedepends+=(cuda)",
			want: []want{{"Dependencies added (makedepends): cuda", "PKGBUILD:2"}},
		},
		{
			name: "checksums and sources are not dependencies (#487 D3)",
			old:  "depends=(\n  'a'\n)\nsource=('https://a.example/1')\nsha256sums=('" + strings.Repeat("1", 64) + "')",
			new:  "depends=(\n  'a'\n)\nsource=('https://a.example/2')\nsha256sums=('" + strings.Repeat("2", 64) + "')",
		},
		{
			name: "old array unreadable: its entries count as absent",
			old:  "depends=(a; b)", new: "depends=(a b)",
			want: []want{{"Dependencies added (depends): a, b", "PKGBUILD:1"}},
		},
		{name: "new array unreadable: no finding (pkgbuild reports it)", old: "depends=(a)", new: "depends=(a; evil)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runDepends(t, tt.old, tt.new)
			if len(findings) != len(tt.want) {
				t.Fatalf("got %d findings, want %d: %v", len(findings), len(tt.want), findings)
			}
			for i, f := range findings {
				if f.Category != CategoryDependsAdded || f.Analyzer != "depends" {
					t.Errorf("finding %d = %s/%s, want depends/%s", i, f.Analyzer, f.Category, CategoryDependsAdded)
				}
				if f.Summary != tt.want[i].summary || f.Location != tt.want[i].location {
					t.Errorf("finding %d = %q at %q, want %q at %q", i, f.Summary, f.Location, tt.want[i].summary, tt.want[i].location)
				}
			}
		})
	}
}

// TestDependsAnalyzerInput484: no PKGBUILD in the range is nothing to
// check; a PKGBUILD without its whole text is an error.
func TestDependsAnalyzerInput484(t *testing.T) {
	findings, err := (&dependsAnalyzer{}).Run(context.Background(), &git.GitDiff{})
	if err != nil || len(findings) != 0 {
		t.Fatalf("no PKGBUILD: got %v, %v; want nothing", findings, err)
	}
	notLoaded := &git.GitDiff{Files: []git.FileDiff{{Path: "PKGBUILD", Kind: git.FileKindPKGBUILD,
		Patch: "@@ -1 +1 @@\n-depends=('a')\n+depends=('a' 'b')\n"}}}
	if _, err := (&dependsAnalyzer{}).Run(context.Background(), notLoaded); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("not loaded: err = %v, want a not-loaded error", err)
	}
}

func TestDependencyName(t *testing.T) {
	for entry, want := range map[string]string{
		"foo":           "foo",
		"foo>=1.2":      "foo",
		"foo<2":         "foo",
		"foo=1:2.0-1":   "foo",
		"foo: for bar":  "foo",
		" foo ":         "foo",
		"python-foo>=1": "python-foo",
	} {
		if got := dependencyName(entry); got != want {
			t.Errorf("dependencyName(%q) = %q, want %q", entry, got, want)
		}
	}
}

// TestDependsAnalyzerRegistered: New() runs the depends analyzer.
func TestDependsAnalyzerRegistered(t *testing.T) {
	m := New().(*manager)
	for _, a := range m.analyzers {
		if a.Name() == "depends" {
			return
		}
	}
	t.Fatal("depends analyzer is not registered in New()")
}
