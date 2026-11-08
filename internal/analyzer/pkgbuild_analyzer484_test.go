package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// loaded484 is a GitDiff whose PKGBUILD has its whole old and new text,
// as ReviewDiff gives it.
func loaded484(old, new string) *git.GitDiff {
	return &git.GitDiff{Files: []git.FileDiff{{
		Path: "PKGBUILD", Kind: git.FileKindPKGBUILD, Patch: "(diff not read)",
		Old: old, New: new, ContentLoaded: true,
	}}}
}

func TestPkgbuildAnalyzer484(t *testing.T) {
	for _, tc := range []struct {
		name     string
		old, new string
		want     []string // "summary @ location"
	}{
		{"all readable", "depends=('a')", "depends=('a' 'b')", nil},
		{"unreadable in the new file, one line", "depends=('a')", "pkgname=x\ndepends=(a; b)",
			[]string{"depends: could not read line 2; findings for it may be missing. Check the diff. @ PKGBUILD:2"}},
		{"unreadable over several lines", "", "source=('a'\n  'b'\n  c&d\n  'e')",
			[]string{"source: could not read lines 1–3; findings for it may be missing. Check the diff. @ PKGBUILD:1"}},
		{"one finding per assignment, arch in the label",
			"", "sha256sums_x86_64=(a|b)\ndepends=('x')\ndepends+=(y>1)",
			[]string{
				"sha256sums_x86_64: could not read line 1; findings for it may be missing. Check the diff. @ PKGBUILD:1",
				"depends: could not read line 3; findings for it may be missing. Check the diff. @ PKGBUILD:3",
			}},
		{"unreadable only in the old file", "depends=(a; b)", "depends=('a')", nil},
		{"Import (no old file)", "", "depends=(a; b)",
			[]string{"depends: could not read line 1; findings for it may be missing. Check the diff. @ PKGBUILD:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := (&pkgbuildAnalyzer{}).Run(context.Background(), loaded484(tc.old, tc.new))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range findings {
				if f.Analyzer != "pkgbuild" || f.Category != CategoryArrayUnreadable {
					t.Fatalf("finding %+v: want analyzer pkgbuild, category %s", f, CategoryArrayUnreadable)
				}
				got = append(got, f.Summary+" @ "+f.Location)
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("findings\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestPkgbuildAnalyzerNoPKGBUILD484: a range that does not change the
// PKGBUILD has nothing to read.
func TestPkgbuildAnalyzerNoPKGBUILD484(t *testing.T) {
	diff := &git.GitDiff{Files: []git.FileDiff{{Path: "x.install", Kind: git.FileKindInstall}}}
	findings, err := (&pkgbuildAnalyzer{}).Run(context.Background(), diff)
	if err != nil || len(findings) != 0 {
		t.Fatalf("got %v, %v; want nothing", findings, err)
	}
}

// TestPkgbuildAnalyzerNotLoaded484: a PKGBUILD without its whole text is
// an error, never an empty result.
func TestPkgbuildAnalyzerNotLoaded484(t *testing.T) {
	diff := &git.GitDiff{Files: []git.FileDiff{{Path: "PKGBUILD", Kind: git.FileKindPKGBUILD, Patch: "+depends=(a; b)"}}}
	_, err := (&pkgbuildAnalyzer{}).Run(context.Background(), diff)
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("err = %v, want a not-loaded error", err)
	}
}

// TestPkgbuildAnalyzerRegistered484: New() runs the pkgbuild analyzer.
func TestPkgbuildAnalyzerRegistered484(t *testing.T) {
	for _, a := range New().(*manager).analyzers {
		if a.Name() == "pkgbuild" {
			return
		}
	}
	t.Fatal("pkgbuild analyzer is not registered in New()")
}
