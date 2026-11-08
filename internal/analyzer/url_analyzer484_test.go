package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// urlFindings484 runs the URL analyzer on whole old and new PKGBUILDs and
// returns "summary @ location" per finding.
func urlFindings484(t *testing.T, old, new string) []string {
	t.Helper()
	findings, err := (&urlAnalyzer{}).Run(context.Background(), loaded484(old, new))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		if f.Analyzer != "url" || f.Category != CategoryURLNew {
			t.Fatalf("finding %+v: want analyzer url, category %s", f, CategoryURLNew)
		}
		got = append(got, f.Summary+" @ "+f.Location)
	}
	return got
}

func TestURLAnalyzer484(t *testing.T) {
	deep := func(extra string) string {
		var b strings.Builder
		b.WriteString("source=(\n")
		for i := 0; i < 25; i++ {
			b.WriteString("  \"https://a.example/f" + string(rune('a'+i)) + ".patch\"\n")
			if i == 18 {
				b.WriteString(extra)
			}
		}
		b.WriteString(")\n")
		return b.String()
	}
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{"host changed", "source=('https://a.example/x-1.tar.gz')", "source=('https://evil.example/x-1.tar.gz')",
			[]string{"New source URL: https://evil.example/x-1.tar.gz @ PKGBUILD:1"}},
		{"unquoted entry (#487 Q3)", "source=(https://a.example/x.tar.gz)", "source=(https://evil.example/x.tar.gz)",
			[]string{"New source URL: https://evil.example/x.tar.gz @ PKGBUILD:1"}},
		{"name::url, quoted in parts (#487 Q5)",
			`source=("x.tar.gz"::"https://a.example/x.tar.gz")`, `source=("x.tar.gz"::"https://evil.example/x.tar.gz")`,
			[]string{"New source URL: https://evil.example/x.tar.gz @ PKGBUILD:1"}},
		{"+= append (#487 S1)", "source=('https://a.example/x.tar.gz')",
			"source=('https://a.example/x.tar.gz')\nsource+=('https://evil.example/y.sh')",
			[]string{"New source URL: https://evil.example/y.sh @ PKGBUILD:2"}},
		{"arch-suffixed array added (#487 S2)", "source_x86_64=('https://a.example/x.deb')",
			"source_x86_64=('https://a.example/x.deb')\nsource_aarch64=('https://evil.example/y.deb')",
			[]string{"New source URL: https://evil.example/y.deb @ PKGBUILD:2"}},
		{"moved between arch arrays: not new (#487 S14)", "source=('https://a.example/x.deb')",
			"source_x86_64=('https://a.example/x.deb')", nil},
		{"reordered: not new", "source=('https://a.example/1' 'https://a.example/2')",
			"source=('https://a.example/2'\n        'https://a.example/1')", nil},
		{"repeated: one finding at its first line", "",
			"source_x86_64=('https://evil.example/y')\nsource_aarch64=('https://evil.example/y')",
			[]string{"New source URL: https://evil.example/y @ PKGBUILD:1"}},
		{"repeated across labels: at the earliest line", "",
			"source=('https://a.example/x')\nsource_x86_64=('https://evil.example/y')\nsource+=('https://evil.example/y')",
			[]string{
				"New source URL: https://a.example/x @ PKGBUILD:1",
				"New source URL: https://evil.example/y @ PKGBUILD:2",
			}},
		{"deep inside a long array (#484)", deep(""), deep("  \"https://evil.example/z.sh\"\n"),
			[]string{"New source URL: https://evil.example/z.sh @ PKGBUILD:21"}},
		{"on a changed closing line (#487 D2)", "source=('https://a.example/1'\n        'https://a.example/2')",
			"source=('https://a.example/1'\n        'https://a.example/2' 'https://evil.example/3')",
			[]string{"New source URL: https://evil.example/3 @ PKGBUILD:2"}},
		{"inside if and package_*(): every assignment counts", "source=('https://a.example/x')",
			"source=('https://a.example/x')\nif [[ $CARCH == aarch64 ]]; then\n  source+=('https://evil.example/arm')\nfi",
			[]string{"New source URL: https://evil.example/arm @ PKGBUILD:3"}},
		{"several new URLs in file order", "",
			"source=('https://b.example/2'\n        git+https://c.example/r.git#tag=v1)\nsource_x86_64=(\"x::https://a.example/1\")",
			[]string{
				"New source URL: https://b.example/2 @ PKGBUILD:1",
				"New source URL: git+https://c.example/r.git#tag=v1 @ PKGBUILD:2",
				"New source URL: https://a.example/1 @ PKGBUILD:3",
			}},
		{"Import, unquoted (#487 I2)", "", "pkgname=x\nsource=(https://a.example/x.tar.gz)",
			[]string{"New source URL: https://a.example/x.tar.gz @ PKGBUILD:2"}},
		{"old source unreadable: its URLs count as absent", "source=('https://a.example/x' ; )",
			"source=('https://a.example/x')", []string{"New source URL: https://a.example/x @ PKGBUILD:1"}},
		{"new source unreadable: no URL findings (pkgbuild reports it)", "source=('https://a.example/x')",
			"source=('https://evil.example/y' ; )", nil},
		{"variable host: not expanded (documented limitation)", "url=https://a.example\nsource=(\"$url/x.tar.gz\")",
			"url=https://evil.example\nsource=(\"$url/x.tar.gz\")", nil},
		{"other arrays are not sources", "", "_source=('https://a.example/x')\nnoextract=('https://b.example/y')", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := urlFindings484(t, tc.old, tc.new)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("findings\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestURLAnalyzerInput484: no PKGBUILD in the range is nothing to check;
// a PKGBUILD without its whole text is an error, never an empty result.
func TestURLAnalyzerInput484(t *testing.T) {
	findings, err := (&urlAnalyzer{}).Run(context.Background(), &git.GitDiff{})
	if err != nil || len(findings) != 0 {
		t.Fatalf("no PKGBUILD: got %v, %v; want nothing", findings, err)
	}
	notLoaded := &git.GitDiff{Files: []git.FileDiff{{Path: "PKGBUILD", Kind: git.FileKindPKGBUILD,
		Patch: "@@ -1 +1 @@\n-source=('https://a.example/x')\n+source=('https://evil.example/x')\n"}}}
	if _, err := (&urlAnalyzer{}).Run(context.Background(), notLoaded); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("not loaded: err = %v, want a not-loaded error", err)
	}
}
