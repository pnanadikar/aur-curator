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
