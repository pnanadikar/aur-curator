package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/version"
)

var ansi481 = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestHeaderShowsVersion: the App header's first line shows the running
// version right after the title, from version.Version, the same value
// --version prints (#481).
func TestHeaderShowsVersion(t *testing.T) {
	t.Parallel()
	app := NewApp(Dependencies{})

	first, _, _ := strings.Cut(ansi481.ReplaceAllString(app.viewHeader(), ""), "\n")
	if want := "aur-curator " + version.Version; !strings.HasPrefix(first, want) {
		t.Fatalf("header line 1 = %q, want it to start with %q", first, want)
	}
	if !strings.Contains(first, "Workspace: ") {
		t.Errorf("header line 1 = %q, lost the workspace", first)
	}
}
