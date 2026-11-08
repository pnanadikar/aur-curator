package publish

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Tests for #480: package files in the repository are 0644, not the 0600
// of the temporary file they are staged in.

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", filepath.Base(path), got, want)
	}
}

func TestPublish480_NewFileIs0644(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("new")})

	if err := p.Publish(context.Background(), "mypkg"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	assertMode(t, filepath.Join(repo, pkgA), 0o644)
}

func TestPublish480_ReplacedFileIs0644(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("rebuilt")})
	dst := filepath.Join(repo, pkgA)
	writeRepoFile(t, dst, []byte("published"))
	if err := os.Chmod(dst, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := p.Publish(context.Background(), "mypkg"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	assertContent(t, dst, []byte("rebuilt"))
	assertMode(t, dst, 0o644)
}

// TestPublish480_UnchangedFileRepaired: a file published 0600 before the
// fix is left in place (same content) but gets 0644.
func TestPublish480_UnchangedFileRepaired(t *testing.T) {
	t.Parallel()
	p, repo := newPublisher379(t, "true", artifactFile{pkgA, []byte("same")})
	dst := filepath.Join(repo, pkgA)
	writeRepoFile(t, dst, []byte("same"))
	if err := os.Chmod(dst, 0o600); err != nil {
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
	if !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("unchanged file was rewritten; only its mode should change")
	}
	assertMode(t, dst, 0o644)
}
