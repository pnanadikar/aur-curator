package widgets

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// TestContentPaneContent: Content is everything set, not the part in view.
func TestContentPaneContent(t *testing.T) {
	c := NewContentPane()
	c.SetSize(40, 2)
	text := "one\ntwo\nthree\nfour"
	c.SetContent(text)
	if got := c.Content(); got != text {
		t.Errorf("Content = %q, want %q", got, text)
	}
}

// TestDiffPaneContent: every file's lines, beyond the visible height; ""
// with no files.
func TestDiffPaneContent(t *testing.T) {
	d := NewDiffPane()
	d.SetSize(80, 2)
	if got := d.Content(); got != "" {
		t.Errorf("Content with no files = %q, want empty", got)
	}
	d.SetFiles([]git.FileDiff{
		{Path: "PKGBUILD", Patch: "diff --git a/PKGBUILD b/PKGBUILD\n+one\n+two"},
		{Path: "LICENSE", Patch: "diff --git a/LICENSE b/LICENSE\n+three"},
	})
	got := d.Content()
	for _, want := range []string{"+one", "+two", "diff --git a/LICENSE b/LICENSE", "+three"} {
		if !strings.Contains(got, want) {
			t.Errorf("Content misses %q:\n%s", want, got)
		}
	}
}
