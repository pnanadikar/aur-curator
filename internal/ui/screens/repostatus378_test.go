package screens

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
)

// Tests for #378: Repository Status reads the database through
// internal/repodb. A missing database means nothing is published yet; an
// unreadable one is an error, never a silently partial list.

// repoStatusDeps returns dependencies pointing at a fresh repository
// directory, and that directory's database path.
func repoStatusDeps(t *testing.T) (deps.Dependencies, string) {
	t.Helper()
	repo := t.TempDir()
	d := deps.Dependencies{
		Repo:   domain.PackageRepository{Location: repo},
		Config: domain.WorkspaceConfig{RootPath: t.TempDir(), RepoDBName: "custom.db.tar.gz"},
	}
	return d, filepath.Join(repo, "custom.db.tar.gz")
}

func loadRepoStatus(t *testing.T, d deps.Dependencies) RepoStatusLoadedMsg {
	t.Helper()
	msg, ok := loadRepoStatusCmd(d)().(RepoStatusLoadedMsg)
	if !ok {
		t.Fatalf("loadRepoStatusCmd did not return a RepoStatusLoadedMsg")
	}
	return msg
}

func TestLoadRepoStatusMissingDatabaseIsEmpty(t *testing.T) {
	t.Parallel()
	d, _ := repoStatusDeps(t)
	msg := loadRepoStatus(t, d)
	if msg.Err != nil {
		t.Fatalf("Err = %v, want none for a missing database", msg.Err)
	}
	if !msg.Initialized {
		t.Error("Initialized = false, want true (the repository directory exists)")
	}
	if len(msg.Packages) != 0 {
		t.Errorf("Packages = %+v, want none", msg.Packages)
	}
	if !msg.LastRepoAdd.IsZero() {
		t.Errorf("LastRepoAdd = %v, want zero (shown as Never)", msg.LastRepoAdd)
	}
}

func TestLoadRepoStatusCorruptDatabaseIsError(t *testing.T) {
	t.Parallel()
	d, dbPath := repoStatusDeps(t)
	if err := os.WriteFile(dbPath, []byte("not a database"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	msg := loadRepoStatus(t, d)
	if msg.Err == nil {
		t.Fatalf("Err = nil, want an error; Packages = %+v", msg.Packages)
	}
	if !strings.Contains(msg.Err.Error(), "not gzip-compressed") {
		t.Errorf("Err = %q, want it to name the problem", msg.Err)
	}
}

func TestLoadRepoStatusListsPackages(t *testing.T) {
	t.Parallel()
	d, dbPath := repoStatusDeps(t)

	desc := "%FILENAME%\nfoo-1.2-1-x86_64.pkg.tar.zst\n\n%NAME%\nfoo\n\n%VERSION%\n1.2-1\n\n%SHA256SUM%\n" +
		strings.Repeat("a", 64) + "\n\n"
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "foo-1.2-1/desc", Mode: 0o644, Size: int64(len(desc)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write([]byte(desc)); err != nil {
		t.Fatalf("write desc: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(dbPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	msg := loadRepoStatus(t, d)
	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	want := []RepoPackageEntry{{Name: "foo", Version: "1.2-1"}}
	if len(msg.Packages) != 1 || msg.Packages[0] != want[0] {
		t.Errorf("Packages = %+v, want %+v", msg.Packages, want)
	}
	if msg.LastRepoAdd.IsZero() {
		t.Error("LastRepoAdd is zero, want the database's modification time")
	}
}
