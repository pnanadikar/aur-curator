package jsonstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

func TestBuildStoreSaveLastSuccessAndGet(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := NewBuildStore(store.NewStorePaths(workspace))

	now := time.Now()
	latest := &domain.Build{
		ID:        "build-1",
		Status:    domain.BuildSuccess,
		StartTime: now,
	}

	if err := bstore.Save("yay", latest); err != nil {
		t.Fatalf("save latest: %v", err)
	}

	if err := bstore.SaveLastSuccess("yay", latest); err != nil {
		t.Fatalf("save last success: %v", err)
	}

	ls, err := bstore.GetLastSuccess("yay")
	if err != nil {
		t.Fatalf("get last success: %v", err)
	}
	if ls.ID != "build-1" {
		t.Fatalf("ID = %q, want %q", ls.ID, "build-1")
	}

	path := filepath.Join(workspace, ".aur-curator", "packages", "yay", "last-success.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("last-success.json should exist: %v", err)
	}
}

func TestBuildStoreGetLastSuccessFallbackOnSuccess(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := NewBuildStore(store.NewStorePaths(workspace))

	now := time.Now()
	latest := &domain.Build{
		ID:        "build-2",
		Status:    domain.BuildSuccess,
		StartTime: now,
	}

	if err := bstore.Save("yay", latest); err != nil {
		t.Fatalf("save latest: %v", err)
	}

	// No last-success file written yet.
	ls, err := bstore.GetLastSuccess("yay")
	if err != nil {
		t.Fatalf("get last success (fallback): %v", err)
	}
	if ls.ID != "build-2" {
		t.Fatalf("ID = %q, want %q", ls.ID, "build-2")
	}

	// Ensure no file was created by the fallback.
	path := filepath.Join(workspace, ".aur-curator", "packages", "yay", "last-success.json")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("last-success.json should NOT exist after fallback read")
	}
}

func TestBuildStoreGetLastSuccessNoFallbackOnFailure(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := NewBuildStore(store.NewStorePaths(workspace))

	now := time.Now()
	latest := &domain.Build{
		ID:        "build-3",
		Status:    domain.BuildFailed,
		StartTime: now,
	}

	if err := bstore.Save("yay", latest); err != nil {
		t.Fatalf("save latest: %v", err)
	}

	_, err := bstore.GetLastSuccess("yay")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestBuildStoreGetLastSuccessReadPermissionDenied(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := NewBuildStore(paths)

	lastSuccessDir := filepath.Dir(paths.LastSuccessBuildPath("yay"))
	if err := os.MkdirAll(lastSuccessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	lastSuccessPath := paths.LastSuccessBuildPath("yay")
	if err := os.WriteFile(lastSuccessPath, []byte(`{"id":"abc"}`), 0o000); err != nil {
		t.Fatal(err)
	}

	latest := &domain.Build{
		ID:     "latest-1",
		Status: domain.BuildSuccess,
	}
	if err := bstore.Save("yay", latest); err != nil {
		t.Fatal(err)
	}

	_, err := bstore.GetLastSuccess("yay")
	if err == nil {
		t.Fatal("expected error for unreadable last-success.json, got nil")
	}
	if errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expected read error, got ErrNotFound (fallback was used)")
	}
}

func TestBuildStoreDeleteOnlyRemovesLatest(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := NewBuildStore(store.NewStorePaths(workspace))

	now := time.Now()
	latest := &domain.Build{
		ID:        "build-4",
		Status:    domain.BuildSuccess,
		StartTime: now,
	}

	if err := bstore.Save("yay", latest); err != nil {
		t.Fatalf("save latest: %v", err)
	}
	if err := bstore.SaveLastSuccess("yay", latest); err != nil {
		t.Fatalf("save last success: %v", err)
	}

	if err := bstore.Delete("yay"); err != nil {
		t.Fatalf("delete latest: %v", err)
	}

	// Latest attempt should be gone.
	_, err := bstore.Get("yay")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("latest build still exists after delete")
	}

	// Last success should still be there.
	ls, err := bstore.GetLastSuccess("yay")
	if err != nil {
		t.Fatalf("last success should still exist: %v", err)
	}
	if ls.ID != "build-4" {
		t.Fatalf("ID = %q, want %q", ls.ID, "build-4")
	}
}
