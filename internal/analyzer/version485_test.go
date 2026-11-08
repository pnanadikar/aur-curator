package analyzer

import "testing"

// #485: version_bump compares pkgver and pkgrel of the whole old and new
// PKGBUILD, as bash words (quotes removed, #487 D12).
func TestVersionBumpWholeFiles485(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		want           string // "summary @ location"; "" for no finding
	}{
		{"quotes removed (#487 D12)", "pkgver='15.0.26'\npkgrel=1", "pkgver='16.0'\npkgrel=1",
			"pkgver: 15.0.26 → 16.0 @ PKGBUILD:1"},
		{"quoting changed only: no finding", "pkgver='1.0'", "pkgver=\"1.0\"", ""},
		{"trailing comment dropped (#24)", "pkgver=1.2.3 # pinned", "pkgver=1.2.4\t# bumped",
			"pkgver: 1.2.3 → 1.2.4 @ PKGBUILD:1"},
		{"pkgrel only, at its line", "pkgver=1\npkgrel=1", "pkgver=1\npkgrel=2", "pkgrel: 1 → 2 @ PKGBUILD:2"},
		{"both: one finding at pkgver's line", "pkgname=x\npkgver=1\npkgrel=3", "pkgname=x\npkgver=2\npkgrel=1",
			"pkgver: 1 → 2, pkgrel: 3 → 1 @ PKGBUILD:2"},
		{"last assignment counts", "pkgver=1\npkgver=2", "pkgver=1\npkgver=3", "pkgver: 2 → 3 @ PKGBUILD:2"},
		{"deep in a long file", "pkgname=x\n" + "# c\n# c\n# c\n# c\n# c\n" + "pkgver=1", "pkgname=x\n" + "# c\n# c\n# c\n# c\n# c\n" + "pkgver=2",
			"pkgver: 1 → 2 @ PKGBUILD:7"},
		{"expansion compared as written", "pkgver=${_v}", "pkgver=${_v//-/.}", "pkgver: ${_v} → ${_v//-/.} @ PKGBUILD:1"},
		{"comment-only value: no finding (#24)", "pkgrel= # set below", "pkgrel=2", ""},
		{"unchanged: no finding", "pkgver=1", "pkgver=1", ""},
		{"Import: no finding", "", "pkgver=1\npkgrel=1", ""},
		{"pkgver removed: no finding", "pkgver=1", "pkgname=x", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			for _, f := range runChecksumFiles(t, tc.old, tc.new) {
				if f.Category == CategoryVersionBump {
					if got != "" {
						t.Fatalf("more than one version_bump: %q and %q", got, f.Summary)
					}
					got = f.Summary + " @ " + f.Location
				}
			}
			if got != tc.want {
				t.Fatalf("version_bump = %q, want %q", got, tc.want)
			}
		})
	}
}
