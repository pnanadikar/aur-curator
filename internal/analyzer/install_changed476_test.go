package analyzer

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

func runShellFiles(t *testing.T, files ...git.FileDiff) []domain.Finding {
	t.Helper()
	findings, err := (&shellAnalyzer{}).Run(context.Background(), &git.GitDiff{Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func installFile(path, patch string) git.FileDiff {
	return git.FileDiff{Path: path, Kind: git.FileKindInstall, Patch: patch}
}

// #476: install_changed for a .install file added or changed, not deleted.
func TestInstallChanged(t *testing.T) {
	tests := []struct {
		name        string
		patch       string
		wantSummary string // "" means no install_changed finding
		wantLoc     string
	}{
		{
			name: "added (Import or new file)",
			patch: `diff --git a/foo.install b/foo.install
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/foo.install
@@ -0,0 +1,3 @@
+post_install() {
+  echo hi
+}
`,
			wantSummary: ".install added: foo.install", wantLoc: "foo.install:1",
		},
		{
			name: "changed",
			patch: `diff --git a/foo.install b/foo.install
index 1111111..2222222 100644
--- a/foo.install
+++ b/foo.install
@@ -1,3 +1,4 @@
 post_install() {
   echo hi
+  echo there
 }
`,
			wantSummary: ".install changed: foo.install", wantLoc: "foo.install:3",
		},
		{
			name: "changed, only a line removed",
			patch: `diff --git a/foo.install b/foo.install
index 1111111..2222222 100644
--- a/foo.install
+++ b/foo.install
@@ -1,4 +1,3 @@
 post_install() {
-  echo there
   echo hi
 }
`,
			wantSummary: ".install changed: foo.install", wantLoc: "foo.install",
		},
		{
			name: "deleted: no finding",
			patch: `diff --git a/foo.install b/foo.install
deleted file mode 100644
index 1111111..0000000
--- a/foo.install
+++ /dev/null
@@ -1,3 +0,0 @@
-post_install() {
-  echo hi
-}
`,
		},
		{
			name: "hunk without headers is changed",
			patch: `@@ -1,2 +1,3 @@
 post_install() {
+  echo there
 }
`,
			wantSummary: ".install changed: foo.install", wantLoc: "foo.install:2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runShellFiles(t, installFile("foo.install", tt.patch))
			var got []domain.Finding
			for _, f := range findings {
				if f.Category == CategoryInstallChanged {
					got = append(got, f)
				}
			}
			if tt.wantSummary == "" {
				if len(got) != 0 {
					t.Fatalf("got %v, want no install_changed", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d install_changed, want 1: %v", len(got), findings)
			}
			if got[0].Summary != tt.wantSummary || got[0].Location != tt.wantLoc {
				t.Errorf("finding = %q at %q, want %q at %q", got[0].Summary, got[0].Location, tt.wantSummary, tt.wantLoc)
			}
		})
	}
}

// TestInstallChangedEveryFile: a split package's several .install files
// each get install_changed, and each is line-scanned (before #476 only
// the last .install file was scanned).
func TestInstallChangedEveryFile(t *testing.T) {
	changed := func(cmd string) string {
		return "@@ -1,2 +1,3 @@\n post_install() {\n+  " + cmd + "\n }\n"
	}
	findings := runShellFiles(t,
		installFile("a.install", changed("curl https://example.org/a")),
		installFile("b.install", changed("echo b")),
	)
	if n := countCategory(findings, CategoryInstallChanged); n != 2 {
		t.Fatalf("install_changed = %d, want 2: %v", n, findings)
	}
	if n := countCategory(findings, CategoryNetworkDownload); n != 1 {
		t.Fatalf("network_download = %d, want 1 (from a.install): %v", n, findings)
	}
}

// TestInstallChangedNotForPKGBUILD: only .install files.
func TestInstallChangedNotForPKGBUILD(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, "@@ -1 +1,2 @@\n pkgname=foo\n+install=foo.install\n")
	if n := countCategory(findings, CategoryInstallChanged); n != 0 {
		t.Fatalf("install_changed = %d for a PKGBUILD, want 0", n)
	}
}
