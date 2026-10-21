package analyzer

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

func runShell(t *testing.T, kind git.FileKind, patch string) []domain.Finding {
	t.Helper()

	a := &shellAnalyzer{}

	findings, err := a.Run(context.Background(), &git.GitDiff{
		Files: []git.FileDiff{
			{
				Path:  "PKGBUILD",
				Kind:  kind,
				Patch: patch,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return findings
}

func TestShellEval(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+eval "$cmd"
 }
`)

	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
}

func TestShellSource(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 prepare() {
+source helper.sh
 }
`)

	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
}

func TestShellDotCommand(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 prepare() {
+. ./helper.sh
 }
`)

	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
}

func TestNetworkDownload(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+curl https://example.org/file
 }
`)

	if got := countCategory(findings, "network_download"); got != 1 {
		t.Fatalf("got %d network_download findings", got)
	}
}

func TestCurlPipeBashProducesBothCategories(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+curl https://example.org/install.sh | bash
 }
`)

	if got := countCategory(findings, "network_download"); got != 1 {
		t.Fatalf("got %d network_download findings", got)
	}
	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
}

func TestDuplicateNetworkPatternsProduceOneFinding(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+curl https://a && wget https://b
 }
`)

	if got := countCategory(findings, "network_download"); got != 1 {
		t.Fatalf("got %d network_download findings", got)
	}
}

func TestDuplicateShellPatternsProduceOneFinding(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+eval "$(echo hi)"
 }
`)

	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
}

func TestInstallScriptScanned(t *testing.T) {
	findings := runShell(t, git.FileKindInstall, `@@ -1 +1 @@
+eval "$cmd"
`)

	if got := countCategory(findings, "shell_exec"); got != 1 {
		t.Fatalf("got %d shell_exec findings", got)
	}
}

func TestRemovedLinesIgnored(t *testing.T) {
	findings := runShell(t, git.FileKindInstall, `@@ -1 +0 @@
-eval "$cmd"
`)

	if len(findings) != 0 {
		t.Fatalf("got %d findings", len(findings))
	}
}

func TestContextLinesIgnored(t *testing.T) {
	findings := runShell(t, git.FileKindInstall, `@@ -1 +1 @@
 eval "$cmd"
`)

	if len(findings) != 0 {
		t.Fatalf("got %d findings", len(findings))
	}
}

func TestSafeCommand(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+make all
 }
`)

	if len(findings) != 0 {
		t.Fatalf("got %d findings", len(findings))
	}
}
