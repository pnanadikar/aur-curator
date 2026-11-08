package analyzer

import (
	"strings"
	"testing"
)

// #485: checksum_removed and checksum_refresh compare each checksum
// label of the whole old and new PKGBUILD, by position.
func TestChecksumWholeFiles485(t *testing.T) {
	h := func(c string) string { return strings.Repeat(c, 64) }
	long := func(at int, value string) string {
		var b strings.Builder
		b.WriteString("sha256sums=(\n")
		for i := 0; i < 12; i++ {
			v := h(string(rune('a' + i%6)))
			if i == at {
				v = value
			}
			b.WriteString("  '" + v + "'\n")
		}
		b.WriteString(")\n")
		return b.String()
	}
	for _, tc := range []struct {
		name, old, new string
		want           []string // "category summary @ location"
	}{
		{"closing line → SKIP (#487 P3)",
			"sha256sums=('" + h("1") + "'\n            '" + h("2") + "')",
			"sha256sums=('" + h("1") + "'\n            'SKIP')",
			[]string{"checksum_removed Checksum verification removed (sha256sums) @ PKGBUILD:2"}},
		{"deep entry → SKIP (#487 P5)", long(-1, ""), long(9, "SKIP"),
			[]string{"checksum_removed Checksum verification removed (sha256sums) @ PKGBUILD:11"}},
		{"deep refresh", long(-1, ""), long(9, h("f")),
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:11"}},
		{"adjacent arrays, one → SKIP, other refreshed (#487 A1)",
			"sha256sums=('" + h("1") + "')\nb2sums=('" + h("2") + "')",
			"sha256sums=('SKIP')\nb2sums=('" + h("3") + "')",
			[]string{
				"checksum_removed Checksum verification removed (sha256sums) @ PKGBUILD:1",
				"checksum_refresh Checksum values updated (b2sums) @ PKGBUILD:2",
			}},
		{"adjacent arch arrays both refreshed: one each (#487 A2)",
			"sha256sums_x86_64=('" + h("1") + "')\nsha256sums_aarch64=('" + h("2") + "')",
			"sha256sums_x86_64=('" + h("3") + "')\nsha256sums_aarch64=('" + h("4") + "')",
			[]string{
				"checksum_refresh Checksum values updated (sha256sums_x86_64) @ PKGBUILD:1",
				"checksum_refresh Checksum values updated (sha256sums_aarch64) @ PKGBUILD:2",
			}},
		{"indexed assignment → SKIP (#487 S7, spotify)",
			"sha512sums=('" + h("1") + "' '" + h("2") + "')",
			"sha512sums=('" + h("1") + "' '" + h("2") + "')\nsha512sums[1]='SKIP'",
			[]string{"checksum_removed Checksum verification removed (sha512sums) @ PKGBUILD:2"}},
		{"hash → command substitution (#487 S9, tor-browser-bin)",
			"sha256sums_x86_64=('" + h("1") + "'\n                   'SKIP')",
			"sha256sums_x86_64=(\"$(_dist_checksum x86_64)\"\n                   'SKIP')",
			[]string{"checksum_removed Checksum verification removed (sha256sums_x86_64) @ PKGBUILD:1"}},
		{"SKIP → empty: refresh, not removal (nothing was verified)",
			"sha256sums=('SKIP')", "sha256sums=('')",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:1"}},
		{"$(…) changed: refresh, not removal",
			"sha256sums=(\"$(f a)\")", "sha256sums=(\"$(f b)\")",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:1"}},
		{"$(…) unchanged: nothing", "sha256sums=(\"$(f)\" 'SKIP')", "sha256sums=(\"$(f)\" 'SKIP')", nil},
		{"unquoted hash → SKIP (#487 Q4)", "sha256sums=(" + h("1") + ")", "sha256sums=(SKIP)",
			[]string{"checksum_removed Checksum verification removed (sha256sums) @ PKGBUILD:1"}},
		{"SKIP stays SKIP beside a refresh: refresh only",
			"sha256sums=('SKIP' '" + h("1") + "')", "sha256sums=('SKIP' '" + h("2") + "')",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:1"}},
		{"entry added: refresh at the new entry",
			"sha256sums=('" + h("1") + "')", "sha256sums=('" + h("1") + "'\n            '" + h("2") + "')",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:2"}},
		{"entry removed: refresh at the opening line",
			"pkgname=x\nsha256sums=('" + h("1") + "' '" + h("2") + "')", "pkgname=x\nsha256sums=('" + h("1") + "')",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:2"}},
		{"conditional override hides nothing (ungoogled-chromium)",
			"sha256sums=('" + h("1") + "' 'SKIP')\nif (( _manual_clone )); then\n  sha256sums[0]='" + h("9") + "'\nfi",
			"sha256sums=('" + h("2") + "' 'SKIP')\nif (( _manual_clone )); then\n  sha256sums[0]='" + h("9") + "'\nfi",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:1"}},
		{"override SKIP already in the old file: refresh, not removal (spotify)",
			"sha512sums=('" + h("1") + "' '" + h("2") + "')\nsha512sums[1]='SKIP'",
			"sha512sums=('" + h("1") + "' '" + h("3") + "')\nsha512sums[1]='SKIP'",
			[]string{"checksum_refresh Checksum values updated (sha512sums) @ PKGBUILD:1"}},
		{"same effective SKIPs written differently: refresh (spotify-38847847)",
			"sha512sums=('" + h("1") + "' 'SKIP')",
			"sha512sums=('" + h("1") + "' '" + h("2") + "')\nsha512sums[1]='SKIP'",
			[]string{"checksum_refresh Checksum values updated (sha512sums) @ PKGBUILD:1"}},
		{"override with a hash added: refresh, not removal",
			"sha256sums=('" + h("1") + "')", "sha256sums=('" + h("1") + "')\nsha256sums[0]='" + h("2") + "'",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:2"}},
		{"reformatted only: nothing",
			"sha256sums=('" + h("1") + "' '" + h("2") + "')", "sha256sums=(\n  " + h("1") + "\n  \"" + h("2") + "\"\n)", nil},
		{"Import: nothing (#487 D6)", "", "sha256sums=('" + h("1") + "')\nb2sums_x86_64=('" + h("2") + "')", nil},
		{"new arch label: nothing", "sha256sums=('" + h("1") + "')",
			"sha256sums=('" + h("1") + "')\nsha256sums_aarch64=('" + h("2") + "')", nil},
		{"deleted label: nothing (makepkg requires checksums)",
			"sha256sums=('" + h("1") + "')\nb2sums=('" + h("2") + "')", "sha256sums=('" + h("1") + "')", nil},
		{"new array unreadable: no removal (array_unreadable reports it)",
			"sha256sums=('" + h("1") + "')\nsha256sums+=('" + h("2") + "')",
			"sha256sums=('SKIP')\nsha256sums+=('" + h("2") + "' ; )",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:1"}},
		{"sha224sums and cksums are checksums too",
			"sha224sums=('" + h("1") + "')\ncksums=('" + h("2") + "')", "sha224sums=('SKIP')\ncksums=('" + h("3") + "')",
			[]string{
				"checksum_removed Checksum verification removed (sha224sums) @ PKGBUILD:1",
				"checksum_refresh Checksum values updated (cksums) @ PKGBUILD:2",
			}},
		{"cross-hunk shape: depends before, hash far below (#487 D3)",
			"depends=(\n  'a'\n)\n\n\n\n\n\nsha256sums=('" + h("1") + "')",
			"depends=(\n  'a'\n  'b'\n)\n\n\n\n\n\nsha256sums=('" + h("2") + "')",
			[]string{"checksum_refresh Checksum values updated (sha256sums) @ PKGBUILD:10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, f := range runChecksumFiles(t, tc.old, tc.new) {
				if f.Category == CategoryChecksumRemoved || f.Category == CategoryChecksumRefresh {
					got = append(got, f.Category+" "+f.Summary+" @ "+f.Location)
				}
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("findings\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
