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

	// No line findings; the file-level install_changed (#476) is not a
	// line finding.
	if n := countCategory(findings, CategoryShellExec) + countCategory(findings, CategoryNetworkDownload); n != 0 {
		t.Fatalf("got %d line findings: %v", n, findings)
	}
}

func TestContextLinesIgnored(t *testing.T) {
	findings := runShell(t, git.FileKindInstall, `@@ -1 +1 @@
 eval "$cmd"
`)

	// No line findings; the file-level install_changed (#476) is not a
	// line finding.
	if n := countCategory(findings, CategoryShellExec) + countCategory(findings, CategoryNetworkDownload); n != 0 {
		t.Fatalf("got %d line findings: %v", n, findings)
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

// TestShellScope covers #474: every Added PKGBUILD line is scanned, not
// only build()/package()/prepare() bodies whose header is in the hunk.
func TestShellScope(t *testing.T) {
	type want struct {
		category string
		location string
	}
	tests := []struct {
		name  string
		patch string
		want  []want
	}{
		{
			name: "top level: command substitution in an assignment",
			patch: `@@ -1,2 +1,3 @@
 pkgname=foo
+pkgver=$(date +%Y)
 pkgrel=1
`,
			want: []want{{"shell_exec", "PKGBUILD:2"}},
		},
		{
			name: "top level: download piped into a shell",
			patch: `@@ -1,2 +1,3 @@
 pkgname=foo
+curl -s https://example.org/x | sh
 pkgrel=1
`,
			want: []want{{"network_download", "PKGBUILD:2"}, {"shell_exec", "PKGBUILD:2"}},
		},
		{
			name: "pkgver()",
			patch: `@@ -10,3 +10,4 @@
 pkgver() {
   cd "$pkgname"
+  printf "r%s" "$(git rev-list --count HEAD)"
 }
`,
			want: []want{{"shell_exec", "PKGBUILD:12"}},
		},
		{
			name: "check()",
			patch: `@@ -20,2 +20,3 @@
 check() {
+  wget https://example.org/testdata
 }
`,
			want: []want{{"network_download", "PKGBUILD:21"}},
		},
		{
			name: "package_<name>()",
			patch: `@@ -30,2 +30,3 @@
 package_foo-docs() {
+  eval "$install_cmd"
 }
`,
			want: []want{{"shell_exec", "PKGBUILD:31"}},
		},
		{
			name: "deep in build(), header outside the hunk",
			patch: `@@ -40,6 +40,7 @@
   make
   make install
   cd foo
+  curl https://example.org/payload
   cd ..
   true
 }
`,
			want: []want{{"network_download", "PKGBUILD:43"}},
		},
		{
			name: "source=( array declaration is not the source built-in",
			patch: `@@ -1,2 +1,3 @@
 pkgname=foo
+source=("https://example.org/foo.tar.gz")
 pkgrel=1
`,
			want: nil,
		},
		{
			name: "top level: removed line",
			patch: `@@ -1,3 +1,2 @@
 pkgname=foo
-curl https://example.org/x
 pkgrel=1
`,
			want: nil,
		},
		{
			name: "top level: context line",
			patch: `@@ -1,3 +1,3 @@
 pkgname=foo
 curl https://example.org/x
 pkgrel=1
`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runShell(t, git.FileKindPKGBUILD, tt.patch)
			var got []want
			for _, f := range findings {
				got = append(got, want{f.Category, f.Location})
			}
			if len(got) != len(tt.want) {
				t.Fatalf("findings = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("finding %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestShellSummaryFormat: Summary is "<label>: <line>" for the PKGBUILD
// and .install alike; no function name (#474).
func TestShellSummaryFormat(t *testing.T) {
	findings := runShell(t, git.FileKindPKGBUILD, `@@ -1,2 +1,3 @@
 build() {
+  curl https://example.org/file
 }
`)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if want := "Network download:   curl https://example.org/file"; findings[0].Summary != want {
		t.Errorf("Summary = %q, want %q", findings[0].Summary, want)
	}
}
