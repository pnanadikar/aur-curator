package publish

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
)

// Tests for #379: a failed publish leaves the repository directory as it
// was, so a file never differs from the db entry naming it. These tests
// replace repo-add with `true` or `false`; they do not need repo-add.

type artifactFile struct {
	name    string
	content []byte
}

// newPublisher379 sets up a confirmed last-success build whose artifacts
// are files (in order), and returns the publisher, running repoAdd in place
// of repo-add, and the repository directory.
func newPublisher379(t *testing.T, repoAdd string, files ...artifactFile) (*impl, string) {
	t.Helper()
	tmp := t.TempDir()
	buildID := domain.BuildID("build-379")
	now := time.Now()
	build := &domain.Build{ID: buildID, Confirmed: true, ConfirmedAt: &now}

	buildDir := filepath.Join(tmp, ".aur-curator", "builds", string(buildID))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var arts []domain.Artifact
	for i, f := range files {
		id := domain.ArtifactID("art-" + string(rune('a'+i)))
		build.ArtifactIDs = append(build.ArtifactIDs, id)
		arts = append(arts, domain.Artifact{ID: id, BuildID: buildID, Filename: f.name})
		if err := os.WriteFile(filepath.Join(buildDir, f.name), f.content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repoDir := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: arts},
		domain.PackageRepository{Location: repoDir},
		tmp,
		"custom.db.tar.gz",
	).(*impl)
	p.repoAdd = repoAdd
	return p, repoDir
}

func writeRepoFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", filepath.Base(path), got, want)
	}
}

// assertRepoNames checks the repository directory holds exactly want: no
// backups or temporary files left behind.
func assertRepoNames(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("repo dir = %v, want %v", got, want)
	}
}

const pkgA = "mypkg-1.0-1-x86_64.pkg.tar.zst"
const pkgB = "mypkg-libs-1.0-1-x86_64.pkg.tar.zst"

// TestPublish379_RepoAddFailureRestoresReplacedFile: a rebuild of the same
// version has the same file name; if repo-add fails, the file the db names
// is back in place.
func TestPublish379_RepoAddFailureRestoresReplacedFile(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "false", artifactFile{pkgA, []byte("rebuilt")})
	writeRepoFile(t, filepath.Join(repo, pkgA), []byte("published"))

	if err := p.Publish(context.Background(), "mypkg"); err == nil || !strings.Contains(err.Error(), "repo-add failed") {
		t.Fatalf("error = %v, want a repo-add failure", err)
	}
	assertContent(t, filepath.Join(repo, pkgA), []byte("published"))
	assertRepoNames(t, repo, pkgA)
}

// TestPublish379_RepoAddFailureRemovesNewFile: a file the repository did
// not have is removed again if repo-add fails.
func TestPublish379_RepoAddFailureRemovesNewFile(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "false", artifactFile{pkgA, []byte("new")})

	if err := p.Publish(context.Background(), "mypkg"); err == nil {
		t.Fatal("expected repo-add failure")
	}
	assertRepoNames(t, repo)
}

// TestPublish379_ReplacesDifferentFile: on success the new content is in
// place and the backup is gone.
func TestPublish379_ReplacesDifferentFile(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("rebuilt")})
	writeRepoFile(t, filepath.Join(repo, pkgA), []byte("published"))

	if err := p.Publish(context.Background(), "mypkg"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	assertContent(t, filepath.Join(repo, pkgA), []byte("rebuilt"))
	assertRepoNames(t, repo, pkgA)
}

// TestPublish379_IdenticalFileNotRewritten: a file already in the
// repository with the same content is left alone: same inode, and its
// modification time is not touched.
func TestPublish379_IdenticalFileNotRewritten(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("same")})
	dst := filepath.Join(repo, pkgA)
	writeRepoFile(t, dst, []byte("same"))
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(dst, past, past); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Publish(context.Background(), "mypkg"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	after, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !after.ModTime().Equal(past) {
		t.Fatal("identical file was rewritten")
	}
	assertRepoNames(t, repo, pkgA)
}

// TestPublish379_MultiArtifactRollback: when the second artifact cannot be
// put in place, the first one's change is undone too, and repo-add never
// runs (`false` would otherwise be the reported failure).
func TestPublish379_MultiArtifactRollback(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "false",
		artifactFile{pkgA, []byte("rebuilt-a")},
		artifactFile{pkgB, []byte("rebuilt-b")},
	)
	writeRepoFile(t, filepath.Join(repo, pkgA), []byte("published-a"))
	// A directory where the second package file should go.
	if err := os.Mkdir(filepath.Join(repo, pkgB), 0o755); err != nil {
		t.Fatal(err)
	}

	err := p.Publish(context.Background(), "mypkg")
	if err == nil || !strings.Contains(err.Error(), pkgB) || strings.Contains(err.Error(), "repo-add") {
		t.Fatalf("error = %v, want a failure naming %s, before repo-add", err, pkgB)
	}
	assertContent(t, filepath.Join(repo, pkgA), []byte("published-a"))
	assertRepoNames(t, repo, pkgA, pkgB)
}

// TestPublish379_LeftoverBackupRemoved: a backup left by a publish that
// never finished is removed, and the new publish goes ahead.
func TestPublish379_LeftoverBackupRemoved(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("rebuilt")})
	writeRepoFile(t, filepath.Join(repo, pkgA), []byte("unfinished"))
	writeRepoFile(t, filepath.Join(repo, pkgA+backupSuffix), []byte("published"))

	if err := p.Publish(context.Background(), "mypkg"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	assertContent(t, filepath.Join(repo, pkgA), []byte("rebuilt"))
	assertRepoNames(t, repo, pkgA)
}

// TestPublish379_LeftoverBackupRemovedThenFailure: if the new publish
// fails too, the file that was in place before it is restored and the
// leftover backup is gone.
func TestPublish379_LeftoverBackupRemovedThenFailure(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "false", artifactFile{pkgA, []byte("rebuilt")})
	writeRepoFile(t, filepath.Join(repo, pkgA), []byte("unfinished"))
	writeRepoFile(t, filepath.Join(repo, pkgA+backupSuffix), []byte("published"))

	if err := p.Publish(context.Background(), "mypkg"); err == nil {
		t.Fatal("expected repo-add failure")
	}
	assertContent(t, filepath.Join(repo, pkgA), []byte("unfinished"))
	assertRepoNames(t, repo, pkgA)
}

func TestSameFileContent(t *testing.T) {
	t.Parallel()
	big := bytes.Repeat([]byte("x"), 200*1024)
	bigLastDiffers := append(append([]byte(nil), big[:len(big)-1]...), 'y')

	tests := []struct {
		name    string
		src     []byte
		dst     []byte // nil: dst does not exist
		dstDir  bool
		want    bool
		wantErr bool
	}{
		{name: "dst missing", src: []byte("a"), want: false},
		{name: "equal", src: []byte("abc"), dst: []byte("abc"), want: true},
		{name: "same size, different", src: []byte("abc"), dst: []byte("abd"), want: false},
		{name: "different size", src: []byte("abc"), dst: []byte("abcd"), want: false},
		{name: "empty both", src: []byte{}, dst: []byte{}, want: true},
		{name: "large equal", src: big, dst: big, want: true},
		{name: "large, last byte differs", src: big, dst: bigLastDiffers, want: false},
		{name: "dst is a directory", src: []byte("a"), dstDir: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			writeRepoFile(t, src, tt.src)
			switch {
			case tt.dstDir:
				if err := os.Mkdir(dst, 0o755); err != nil {
					t.Fatal(err)
				}
			case tt.dst != nil:
				writeRepoFile(t, dst, tt.dst)
			}
			got, err := sameFileContent(src, dst)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
