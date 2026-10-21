package publish

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
)

// ---------------------------------------------------------------------------
// Fakes for Publisher tests
// ---------------------------------------------------------------------------

type fakeBuildStoreForPublish struct {
	build *domain.Build
}

func (f *fakeBuildStoreForPublish) Get(_ domain.PackageName) (*domain.Build, error) {
	return f.build, nil
}

func (f *fakeBuildStoreForPublish) Save(_ domain.PackageName, _ *domain.Build) error { return nil }

func (f *fakeBuildStoreForPublish) ReadBuildLog(_ domain.BuildID, _ string) (string, error) {
	return "", nil
}

func (f *fakeBuildStoreForPublish) Delete(_ domain.PackageName) error { return nil }

func (f *fakeBuildStoreForPublish) GetLastSuccess(_ domain.PackageName) (*domain.Build, error) {
	return f.build, nil
}

func (f *fakeBuildStoreForPublish) SaveLastSuccess(_ domain.PackageName, _ *domain.Build) error {
	return nil
}

type fakeArtifactStoreForPublish struct {
	artifacts []domain.Artifact
}

func (f *fakeArtifactStoreForPublish) Get(id domain.ArtifactID) (*domain.Artifact, error) {
	for _, a := range f.artifacts {
		if a.ID == id {
			cp := a
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeArtifactStoreForPublish) Save(_ domain.ArtifactID, _ *domain.Artifact) error { return nil }

func (f *fakeArtifactStoreForPublish) GetArtifactsByPackage(_ domain.PackageName) ([]domain.Artifact, error) {
	return f.artifacts, nil
}

func (f *fakeArtifactStoreForPublish) Delete(_ domain.ArtifactID) error { return nil }

func (f *fakeArtifactStoreForPublish) ListAll() ([]domain.Artifact, error) {
	return f.artifacts, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeValidPkgTarGz creates a minimal valid .pkg.tar.gz pacman package
// containing a .PKGINFO file with the given pkginfo content.
func makeValidPkgTarGz(pkginfo string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name: ".PKGINFO",
		Mode: 0o644,
		Size: int64(len(pkginfo)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write([]byte(pkginfo)); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPublish_CopiesArtifactsIntoRepoLocation(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// Set up a confirmed build.
	buildID := domain.BuildID("test-build-001")
	build := &domain.Build{
		ID:          buildID,
		Confirmed:   true,
		ArtifactIDs: []domain.ArtifactID{"art-1", "art-2"},
		ConfirmedAt: func() *time.Time {
			t := time.Now()
			return &t
		}(),
	}
	artifacts := []domain.Artifact{
		{
			ID:       domain.ArtifactID("art-1"),
			BuildID:  buildID,
			Filename: "mypkg-1.0-1-x86_64.pkg.tar.gz",
		},
		{
			ID:       domain.ArtifactID("art-2"),
			BuildID:  buildID,
			Filename: "mypkg-libs-1.0-1-x86_64.pkg.tar.gz",
		},
	}

	// Create valid pacman package files in the builds directory.
	buildDir := filepath.Join(tmp, ".aur-curator", "builds", string(buildID))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, a := range artifacts {
		pkginfo := "pkgname = mypkg\npkgver = 1.0\narch = x86_64\nbuildnum = " + string(rune('0'+i))
		content, err := makeValidPkgTarGz(pkginfo)
		if err != nil {
			t.Fatalf("failed to create valid package for %s: %v", a.Filename, err)
		}
		path := filepath.Join(buildDir, a.Filename)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repoDir := filepath.Join(tmp, "repo")

	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: artifacts},
		domain.PackageRepository{Location: repoDir},
		tmp,
		"custom.db.tar.gz",
	)

	ctx := context.Background()
	err := p.Publish(ctx, "mypkg")
	if err != nil {
		t.Fatalf("Publish returned unexpected error: %v", err)
	}

	// Assert each artifact was copied into repo.Location.
	for _, a := range artifacts {
		copiedPath := filepath.Join(repoDir, a.Filename)
		if _, err := os.Stat(copiedPath); err != nil {
			t.Errorf("artifact %s not found in repo location after Publish: %v", a.Filename, err)
			continue
		}
	}

	// Assert the repo database was created (repo-add modifies it in place).
	dbPath := filepath.Join(repoDir, "custom.db.tar.gz")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("repo database %s not found after Publish: %v", dbPath, err)
	}
}

func TestPublish_CopyFailureReportsWhichArtifactFailed(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	buildID := domain.BuildID("test-build-002")
	build := &domain.Build{
		ID:        buildID,
		Confirmed: true,
		ConfirmedAt: func() *time.Time {
			t := time.Now()
			return &t
		}(),
	}
	artifacts := []domain.Artifact{
		{
			ID:       domain.ArtifactID("art-1"),
			BuildID:  buildID,
			Filename: "pkg-a-1.0-1-x86_64.pkg.tar.gz",
		},
		{
			ID:       domain.ArtifactID("art-2"),
			BuildID:  buildID,
			Filename: "pkg-b-1.0-1-x86_64.pkg.tar.gz",
		},
	}

	// Only create the first artifact file.
	buildDir := filepath.Join(tmp, ".aur-curator", "builds", string(buildID))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := makeValidPkgTarGz("pkgname = pkg-a\npkgver = 1.0\narch = x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "pkg-a-1.0-1-x86_64.pkg.tar.gz"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: artifacts},
		domain.PackageRepository{Location: filepath.Join(tmp, "repo")},
		tmp,
		"custom.db.tar.gz",
	)

	ctx := context.Background()
	err = p.Publish(ctx, "mypkg")
	if err == nil {
		t.Fatal("expected Publish to fail when an artifact file is missing")
	}
}

func TestPublish_NoArtifacts(t *testing.T) {
	t.Parallel()

	buildID := domain.BuildID("test-build-003")
	build := &domain.Build{
		ID:        buildID,
		Confirmed: true,
		ConfirmedAt: func() *time.Time {
			t := time.Now()
			return &t
		}(),
	}

	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: []domain.Artifact{}},
		domain.PackageRepository{Location: filepath.Join(t.TempDir(), "repo")},
		t.TempDir(),
		"custom.db.tar.gz",
	)

	ctx := context.Background()
	err := p.Publish(ctx, "mypkg")
	if err == nil {
		t.Fatal("expected Publish to fail when no artifacts found")
	}
}

func TestPublish_UnconfirmedBuild(t *testing.T) {
	t.Parallel()

	buildID := domain.BuildID("test-build-004")
	build := &domain.Build{
		ID:        buildID,
		Confirmed: false,
	}

	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: nil},
		domain.PackageRepository{Location: filepath.Join(t.TempDir(), "repo")},
		t.TempDir(),
		"custom.db.tar.gz",
	)

	ctx := context.Background()
	err := p.Publish(ctx, "mypkg")
	if err == nil {
		t.Fatal("expected Publish to fail for unconfirmed build")
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "source.txt")
	dst := filepath.Join(tmp, "dest.txt")

	srcContent := []byte("hello copy world")
	if err := os.WriteFile(src, srcContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(data) != string(srcContent) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(srcContent))
	}
}

func TestCopyFile_SourceMissing(t *testing.T) {
	t.Parallel()

	if err := copyFile("/nonexistent/path/file.txt", "/tmp/out.txt"); err == nil {
		t.Fatal("expected copyFile to fail when source does not exist")
	}
}

// ---------------------------------------------------------------------------
// Missing test 7: ArtifactScoping — only artifacts in build.ArtifactIDs are published
// ---------------------------------------------------------------------------

func TestArtifactScoping(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	buildID := domain.BuildID("scoped-build-001")

	// Build references only art-1 and art-3.
	build := &domain.Build{
		ID:          buildID,
		Confirmed:   true,
		ArtifactIDs: []domain.ArtifactID{"art-1", "art-3"},
		ConfirmedAt: func() *time.Time {
			t := time.Now()
			return &t
		}(),
	}

	// Artifact store contains art-1, art-2, art-3 — art-2 should NOT be published.
	artifacts := []domain.Artifact{
		{
			ID:       domain.ArtifactID("art-1"),
			BuildID:  buildID,
			Filename: "mypkg-1.0-1-x86_64.pkg.tar.gz",
		},
		{
			ID:       domain.ArtifactID("art-2"),
			BuildID:  buildID,
			Filename: "should-not-exist-1.0-1-x86_64.pkg.tar.gz",
		},
		{
			ID:       domain.ArtifactID("art-3"),
			BuildID:  buildID,
			Filename: "mypkg-libs-1.0-1-x86_64.pkg.tar.gz",
		},
	}

	// Only create files for art-1 and art-3 (the ones in build.ArtifactIDs).
	// art-2 file does NOT exist — publishing should succeed because art-2 is
	// not referenced by build.ArtifactIDs.
	buildDir := filepath.Join(tmp, ".aur-curator", "builds", string(buildID))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, a := range artifacts {
		if a.ID == "art-2" {
			// Do NOT create the file for art-2 — it must NOT be copied.
			continue
		}
		pkginfo := "pkgname = mypkg\npkgver = 1.0\narch = x86_64\nbuildnum = " + string(a.ID[3])
		content, err := makeValidPkgTarGz(pkginfo)
		if err != nil {
			t.Fatalf("failed to create valid package for %s: %v", a.Filename, err)
		}
		if err := os.WriteFile(filepath.Join(buildDir, a.Filename), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repoDir := filepath.Join(tmp, "repo")

	p := NewPublisher(
		&fakeBuildStoreForPublish{build: build},
		&fakeArtifactStoreForPublish{artifacts: artifacts},
		domain.PackageRepository{Location: repoDir},
		tmp,
		"custom.db.tar.gz",
	)

	ctx := context.Background()
	if err := p.Publish(ctx, "mypkg"); err != nil {
		t.Fatalf("Publish returned unexpected error: %v", err)
	}

	// Verify art-1 and art-3 were copied.
	for _, a := range []domain.Artifact{artifacts[0], artifacts[2]} {
		copiedPath := filepath.Join(repoDir, a.Filename)
		if _, err := os.Stat(copiedPath); err != nil {
			t.Errorf("expected artifact %s to be in repo after Publish: %v", a.Filename, err)
		}
	}

	// Verify art-2 (not in build.ArtifactIDs) was NOT copied.
	notCopiedPath := filepath.Join(repoDir, "should-not-exist-1.0-1-x86_64.pkg.tar.gz")
	if _, err := os.Stat(notCopiedPath); err == nil {
		t.Error("artifact not referenced by build.ArtifactIDs should NOT be in repo")
	}
}
