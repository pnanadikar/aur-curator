package screens

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// TestFindingsShowHiddenCharacters491: a Shell finding quotes the line as
// written; its control and bidi characters show as escapes (#491).
func TestFindingsShowHiddenCharacters491(t *testing.T) {
	lines := findingLines(t, []domain.Finding{{
		Analyzer: "shell", Category: "shell_exec",
		Summary:  "curl x | sh # \x1b[2K\rok \u202e",
		Location: "PKGBUILD:3",
	}})
	out := strings.Join(lines, "\n")
	for _, raw := range []string{"\x1b", "\r", "\u202e"} {
		if strings.Contains(out, raw) {
			t.Errorf("findings hold a raw %q:\n%q", raw, out)
		}
	}
	if want := `curl x | sh # \x1b[2K\rok \u202e`; !strings.Contains(out, want) {
		t.Errorf("findings missing %q:\n%s", want, out)
	}
}

// TestCommitsShowHiddenCharacters491: a commit's author and subject are
// the committer's text; they show as escapes (#491).
func TestCommitsShowHiddenCharacters491(t *testing.T) {
	out := renderCommitsContent([]git.Commit{{
		Hash: "0123456789abcdef", Author: "eve\x1b[8m", Subject: "upgpkg: 1.0\u202e\u200b", Date: "2026-11-09",
	}})
	for _, raw := range []string{"\x1b", "\u202e", "\u200b"} {
		if strings.Contains(out, raw) {
			t.Errorf("commits hold a raw %q:\n%q", raw, out)
		}
	}
	for _, want := range []string{`eve\x1b[8m`, `upgpkg: 1.0\u202e\u200b`} {
		if !strings.Contains(out, want) {
			t.Errorf("commits missing %q:\n%s", want, out)
		}
	}
}
