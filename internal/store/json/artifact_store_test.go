package jsonstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

func TestArtifactStoreListAllAllGood(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}
	artB := &domain.Artifact{
		ID:           "art-b",
		PackageName:  "pkg-b",
		Filename:     "pkg-b-2.0.tar.gz",
		Version:      "2.0",
		Architecture: "x86_64",
		BuildID:      "build-2",
		Size:         200,
	}

	if err := as.Save("art-a", artA); err != nil {
		t.Fatalf("save art-a: %v", err)
	}
	if err := as.Save("art-b", artB); err != nil {
		t.Fatalf("save art-b: %v", err)
	}

	arts, err := as.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("len(arts) = %d, want 2", len(arts))
	}
}

func TestArtifactStoreListAllNoArtifactsDir(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	arts, err := as.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if arts != nil {
		t.Fatalf("arts = %v, want nil", arts)
	}
}

func TestArtifactStoreListAllCorruptJSON(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}

	if err := as.Save("art-a", artA); err != nil {
		t.Fatalf("save art-a: %v", err)
	}

	corruptPath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-b", "artifact.json")
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatalf("create corrupt dir: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	arts, err := as.ListAll()
	if err == nil {
		t.Fatalf("ListAll: expected error, got nil")
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	if arts[0].ID != "art-a" {
		t.Fatalf("arts[0].ID = %q, want %q", arts[0].ID, "art-a")
	}
	if !strings.Contains(err.Error(), "art-b") {
		t.Fatalf("error does not mention art-b: %v", err)
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error does not contain 'decode': %v", err)
	}
}

func TestArtifactStoreListAllMissingArtifactJSON(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}

	if err := as.Save("art-a", artA); err != nil {
		t.Fatalf("save art-a: %v", err)
	}

	emptyDir := filepath.Join(workspace, ".aur-curator", "artifacts", "art-b")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("create empty dir: %v", err)
	}

	arts, err := as.ListAll()
	if err == nil {
		t.Fatalf("ListAll: expected error, got nil")
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	if !strings.Contains(err.Error(), "has no artifact.json") {
		t.Fatalf("error does not mention 'has no artifact.json': %v", err)
	}
}

func TestArtifactStoreListAllUnreadableFile(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("skipping: running as root")
	}

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}

	if err := as.Save("art-a", artA); err != nil {
		t.Fatalf("save art-a: %v", err)
	}

	unreadablePath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-b", "artifact.json")
	if err := os.MkdirAll(filepath.Dir(unreadablePath), 0o755); err != nil {
		t.Fatalf("create unreadable dir: %v", err)
	}
	if err := os.WriteFile(unreadablePath, []byte(`{"id":"art-b"}`), 0o644); err != nil {
		t.Fatalf("write unreadable: %v", err)
	}
	if err := os.Chmod(unreadablePath, 0o000); err != nil {
		t.Fatalf("chmod 000: %v", err)
	}
	defer func() { _ = os.Chmod(unreadablePath, 0o644) }()

	arts, err := as.ListAll()
	if err == nil {
		t.Fatalf("ListAll: expected error, got nil")
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	if arts[0].ID != "art-a" {
		t.Fatalf("arts[0].ID = %q, want %q", arts[0].ID, "art-a")
	}
	if !strings.Contains(err.Error(), "art-b") {
		t.Fatalf("error does not mention art-b: %v", err)
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("error does not contain 'read': %v", err)
	}
}

func TestArtifactStoreListAllTwoBadRecords(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}

	if err := as.Save("art-a", artA); err != nil {
		t.Fatalf("save art-a: %v", err)
	}

	// Corrupt record
	corruptPath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-b", "artifact.json")
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o755); err != nil {
		t.Fatalf("create corrupt dir: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{bad"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	// Empty record dir
	emptyDir := filepath.Join(workspace, ".aur-curator", "artifacts", "art-c")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("create empty dir: %v", err)
	}

	arts, err := as.ListAll()
	if err == nil {
		t.Fatalf("ListAll: expected error, got nil")
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	if arts[0].ID != "art-a" {
		t.Fatalf("arts[0].ID = %q, want %q", arts[0].ID, "art-a")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "art-b") {
		t.Fatalf("error does not mention art-b: %v", err)
	}
	if !strings.Contains(errStr, "art-c") {
		t.Fatalf("error does not mention art-c: %v", err)
	}
}

func TestArtifactStoreGetArtifactsByPackageWithCorrupt(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	artA1 := &domain.Artifact{
		ID:           "art-a-1",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}
	artA2 := &domain.Artifact{
		ID:           "art-a-2",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-2.0.tar.gz",
		Version:      "2.0",
		Architecture: "x86_64",
		BuildID:      "build-2",
		Size:         200,
	}
	artB := &domain.Artifact{
		ID:           "art-b-1",
		PackageName:  "pkg-b",
		Filename:     "pkg-b-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-3",
		Size:         300,
	}

	if err := as.Save("art-a-1", artA1); err != nil {
		t.Fatalf("save art-a-1: %v", err)
	}
	if err := as.Save("art-a-2", artA2); err != nil {
		t.Fatalf("save art-a-2: %v", err)
	}
	if err := as.Save("art-b-1", artB); err != nil {
		t.Fatalf("save art-b-1: %v", err)
	}

	// Corrupt a pkg-a record
	corruptPath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-a-2", "artifact.json")
	if err := os.WriteFile(corruptPath, []byte("{bad"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	artsA, errA := as.GetArtifactsByPackage("pkg-a")
	if errA == nil {
		t.Fatalf("GetArtifactsByPackage(pkg-a): expected error, got nil")
	}
	if len(artsA) != 1 {
		t.Fatalf("len(artsA) = %d, want 1", len(artsA))
	}
	if artsA[0].ID != "art-a-1" {
		t.Fatalf("artsA[0].ID = %q, want %q", artsA[0].ID, "art-a-1")
	}

	artsB, errB := as.GetArtifactsByPackage("pkg-b")
	if errB == nil {
		t.Fatalf("GetArtifactsByPackage(pkg-b): expected error, got nil")
	}
	if len(artsB) != 1 {
		t.Fatalf("len(artsB) = %d, want 1", len(artsB))
	}
	if artsB[0].ID != "art-b-1" {
		t.Fatalf("artsB[0].ID = %q, want %q", artsB[0].ID, "art-b-1")
	}
}

func TestArtifactStoreDeleteDirNotRemovable(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	art := &domain.Artifact{
		ID:           "art-a",
		PackageName:  "pkg-a",
		Filename:     "pkg-a-1.0.tar.gz",
		Version:      "1.0",
		Architecture: "x86_64",
		BuildID:      "build-1",
		Size:         100,
	}

	if err := as.Save("art-a", art); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Write an extra file into the artifact dir so the dir is not removable
	extraPath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-a", "extra.txt")
	if err := os.WriteFile(extraPath, []byte("extra"), 0o644); err != nil {
		t.Fatalf("write extra file: %v", err)
	}

	err := as.Delete("art-a")
	if err == nil {
		t.Fatalf("Delete: expected error, got nil")
	}

	// artifact.json should be gone
	artPath := filepath.Join(workspace, ".aur-curator", "artifacts", "art-a", "artifact.json")
	if _, statErr := os.Stat(artPath); !os.IsNotExist(statErr) {
		t.Fatalf("artifact.json still exists after Delete")
	}

	// extra.txt should still be there (dir not cleaned up)
	if _, statErr := os.Stat(extraPath); os.IsNotExist(statErr) {
		t.Fatalf("extra.txt was removed (it shouldn't be)")
	}
}

func TestArtifactStoreDeleteNonexistent(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	as := NewArtifactStore(store.NewStorePaths(workspace))

	err := as.Delete("nonexistent")
	if err != nil {
		t.Fatalf("Delete(nonexistent): err = %v, want nil", err)
	}
}
