package git

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

const samplePatch = `diff --git a/PKGBUILD b/PKGBUILD
index 111..222 100644
--- a/PKGBUILD
+++ b/PKGBUILD
@@ -1 +1 @@
-pkgver=1.0
+pkgver=1.1
diff --git a/.SRCINFO b/.SRCINFO
index 333..444 100644
--- a/.SRCINFO
+++ b/.SRCINFO
@@ -1 +1 @@
-pkgver = 1.0
+pkgver = 1.1
diff --git a/yay.install b/yay.install
new file mode 100644
--- /dev/null
+++ b/yay.install
@@ -0,0 +1 @@
+post_install() { :; }
`

func TestParseFileDiffs(t *testing.T) {
	files := parseFileDiffs(samplePatch)
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	if files[0].Path != "PKGBUILD" || files[0].Kind != FileKindPKGBUILD {
		t.Fatalf("unexpected file[0]: %+v", files[0])
	}
	if files[1].Path != ".SRCINFO" || files[1].Kind != FileKindSRCINFO {
		t.Fatalf("unexpected file[1]: %+v", files[1])
	}
	if files[2].Path != "yay.install" || files[2].Kind != FileKindInstall {
		t.Fatalf("unexpected file[2]: %+v", files[2])
	}

	// Each FileDiff carries its own patch text, not one concatenated blob.
	if files[0].Patch == files[1].Patch {
		t.Fatal("expected per-file patches, got identical/concatenated content")
	}
}

func TestParseFileDiffsEmpty(t *testing.T) {
	if files := parseFileDiffs(""); files != nil {
		t.Fatal("expected nil")
	}
}

func TestClassifyFileKind(t *testing.T) {
	cases := []struct {
		path string
		want FileKind
	}{
		{"PKGBUILD", FileKindPKGBUILD},
		{".SRCINFO", FileKindSRCINFO},
		{"yay.install", FileKindInstall},
		{"fix-checksum.patch", FileKindPatch},
		{"legacy.diff", FileKindPatch},
		{"checksums.sha256", FileKindOther},
	}
	for _, c := range cases {
		if got := classifyFileKind(c.path); got != c.want {
			t.Errorf("classifyFileKind(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestParseCommits(t *testing.T) {
	data := "abc123\x1fAlice\x1f2026-07-22T10:00:00Z\x1fInitial import\n" +
		"def456\x1fBob\x1f2026-07-23T09:30:00Z\x1fUpdate PKGBUILD"

	commits := parseCommits(data)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Author != "Alice" {
		t.Fatalf("unexpected author: %q", commits[0].Author)
	}
	if commits[1].Subject != "Update PKGBUILD" {
		t.Fatalf("unexpected subject: %q", commits[1].Subject)
	}
}

func TestParseCommitsEmpty(t *testing.T) {
	if commits := parseCommits(""); commits != nil {
		t.Fatal("expected nil")
	}
}

func TestDiffWithLocalMaster(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"local123\n",    // for-each-ref refs/heads/master
			"upstream456\n", // rev-parse upstream/master
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fBump version\n", // log reviewRange
			"deadbeef\n", // rev-parse local123^{tree} — not empty
			samplePatch,  // diff reviewDiffRange
			"base789\n",  // merge-base local123 upstream456 (#387)
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.Diff(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if diff.LocalCommit != "local123" {
		t.Fatalf("unexpected LocalCommit: %q", diff.LocalCommit)
	}
	if diff.UpstreamCommit != "upstream456" {
		t.Fatalf("unexpected UpstreamCommit: %q", diff.UpstreamCommit)
	}
	if len(diff.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(diff.Commits))
	}
	if len(diff.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(diff.Files))
	}

	// The log calls must use the explicit master..upstream/master
	// range, never HEAD.
	logCall := r.calls[2]
	if logCall.Args[len(logCall.Args)-1] != reviewRange {
		t.Fatalf("log call did not use reviewRange: %#v", logCall.Args)
	}
	// The diff must use three dots (#322): upstream's changes since the
	// merge base, so local commits on master are not shown as removals.
	diffCall := r.calls[4]
	if diffCall.Args[len(diffCall.Args)-1] != "master...upstream/master" {
		t.Fatalf("diff call did not use master...upstream/master: %#v", diffCall.Args)
	}
}

func TestDiffNoLocalMaster(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"", // for-each-ref refs/heads/master -> no local master
			"upstream456\n",
			"u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fInitial import\n",
			samplePatch,
		},
	}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

	diff, err := p.Diff(context.Background(), domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if diff.LocalCommit != "" {
		t.Fatalf("expected empty LocalCommit for Import case, got %q", diff.LocalCommit)
	}

	// No local master: log and diff must fall back to full upstream
	// history and an empty-tree diff, not the master..upstream/master range.
	logCall := r.calls[2]
	if logCall.Args[len(logCall.Args)-1] != "upstream/master" {
		t.Fatalf("expected log against upstream/master, got %#v", logCall.Args)
	}
	diffCall := r.calls[3]
	if len(diffCall.Args) < 2 || diffCall.Args[len(diffCall.Args)-2] != emptyTreeHash {
		t.Fatalf("expected diff against emptyTreeHash, got %#v", diffCall.Args)
	}
}

func TestIsEmptyMaster(t *testing.T) {
	t.Run("no local commit", func(t *testing.T) {
		r := &fakeRunner{}
		p := &provider{runner: r, client: http.DefaultClient}
		empty, err := p.isEmptyMaster(context.Background(), "/tmp/repo", "")
		if err != nil {
			t.Fatal(err)
		}
		if !empty {
			t.Fatal("expected true for empty localCommit")
		}
		if len(r.calls) != 0 {
			t.Fatalf("expected no runner calls when localCommit is empty, got %d", len(r.calls))
		}
	})

	t.Run("empty tree", func(t *testing.T) {
		r := &fakeRunner{outputs: []string{emptyTreeHash + "\n"}}
		p := &provider{runner: r, client: http.DefaultClient}
		empty, err := p.isEmptyMaster(context.Background(), "/tmp/repo", "abc123")
		if err != nil {
			t.Fatal(err)
		}
		if !empty {
			t.Fatal("expected true when tree matches emptyTreeHash")
		}
	})

	t.Run("non-empty tree", func(t *testing.T) {
		r := &fakeRunner{outputs: []string{"deadbeef\n"}}
		p := &provider{runner: r, client: http.DefaultClient}
		empty, err := p.isEmptyMaster(context.Background(), "/tmp/repo", "abc123")
		if err != nil {
			t.Fatal(err)
		}
		if empty {
			t.Fatal("expected false when tree does not match emptyTreeHash")
		}
	})

	t.Run("runner error propagates", func(t *testing.T) {
		r := &fakeRunner{errs: []error{errors.New("boom")}}
		p := &provider{runner: r, client: http.DefaultClient}
		if _, err := p.isEmptyMaster(context.Background(), "/tmp/repo", "abc123"); err == nil {
			t.Fatal("expected error to propagate, not be swallowed")
		}
	})
}
