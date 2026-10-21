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

func TestReviewStoreRoundTripAndOverwrite(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	rstore := NewReviewStore(store.NewStorePaths(workspace))

	first := &domain.Review{
		ID:             "review-1",
		PackageName:    "yay",
		UpstreamCommit: "abc123",
		Status:         domain.ReviewApproved,
		DecisionTime:   time.Unix(100, 0).UTC(),
		ReviewerNotes:  "looks fine",
		FindingsStatus: domain.FindingsNotRun,
	}

	if err := rstore.Save("yay", first); err != nil {
		t.Fatalf("save first review: %v", err)
	}

	got, err := rstore.Get("yay")
	if err != nil {
		t.Fatalf("get first review: %v", err)
	}
	if got.Status != domain.ReviewApproved {
		t.Fatalf("status = %q, want %q", got.Status, domain.ReviewApproved)
	}
	if got.FindingsStatus != domain.FindingsNotRun {
		t.Fatalf("findings status = %q, want %q", got.FindingsStatus, domain.FindingsNotRun)
	}
	if got.ReviewerNotes != "looks fine" {
		t.Fatalf("reviewer notes = %q", got.ReviewerNotes)
	}

	second := &domain.Review{
		ID:             "review-2",
		PackageName:    "yay",
		UpstreamCommit: "def456",
		Status:         domain.ReviewRejected,
		DecisionTime:   time.Unix(200, 0).UTC(),
		Rationale:      "",
		FindingsStatus: domain.FindingsNotRun,
	}
	if err := rstore.Save("yay", second); err != nil {
		t.Fatalf("save second review: %v", err)
	}

	got, err = rstore.Get("yay")
	if err != nil {
		t.Fatalf("get second review: %v", err)
	}
	if got.ID != "review-2" {
		t.Fatalf("id = %q, want review-2", got.ID)
	}
	if got.UpstreamCommit != "def456" {
		t.Fatalf("upstream commit = %q, want def456", got.UpstreamCommit)
	}
	if got.Rationale != "" {
		t.Fatalf("rationale = %q, want empty string", got.Rationale)
	}

	path := filepath.Join(workspace, ".aur-curator", "packages", "yay", "review.json")
	if _, err := rstore.Get("yay"); err != nil {
		t.Fatalf("round-trip after write at %s: %v", path, err)
	}
}

func TestReviewStoreMissingAndDelete(t *testing.T) {
	t.Parallel()

	rstore := NewReviewStore(store.NewStorePaths(t.TempDir()))

	review, err := rstore.Get("missing")
	if review != nil {
		t.Fatalf("review = %#v, want nil", review)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	if err := rstore.Delete("missing"); err != nil {
		t.Fatalf("delete missing review: %v", err)
	}
}

func TestReviewStoreMultiplePackages(t *testing.T) {
	t.Parallel()

	rstore := NewReviewStore(store.NewStorePaths(t.TempDir()))

	reviews := []*domain.Review{
		{
			ID:             "yay@abc",
			PackageName:    "yay",
			UpstreamCommit: "abc",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
		{
			ID:             "paru@def",
			PackageName:    "paru",
			UpstreamCommit: "def",
			Status:         domain.ReviewApproved,
			FindingsStatus: domain.FindingsNotRun,
		},
	}

	pkgs := []domain.PackageName{"yay", "paru"}

	for i := range pkgs {
		if err := rstore.Save(pkgs[i], reviews[i]); err != nil {
			t.Fatalf("save %s: %v", pkgs[i], err)
		}
	}

	for i := range pkgs {
		got, err := rstore.Get(pkgs[i])
		if err != nil {
			t.Fatalf("get %s: %v", pkgs[i], err)
		}

		if got.PackageName != pkgs[i] {
			t.Fatalf("package=%q want %q", got.PackageName, pkgs[i])
		}

		if got.UpstreamCommit != reviews[i].UpstreamCommit {
			t.Fatalf("commit=%q want %q",
				got.UpstreamCommit,
				reviews[i].UpstreamCommit)
		}
	}
}

func TestReviewStoreSaveCreatesDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rstore := NewReviewStore(store.NewStorePaths(root))

	r := &domain.Review{
		ID:             "yay@abc",
		PackageName:    "yay",
		UpstreamCommit: "abc",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsNotRun,
	}

	if err := rstore.Save("yay", r); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(
		root,
		".aur-curator",
		"packages",
		"yay",
		"review.json",
	)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("review.json not created: %v", err)
	}
}

func TestReviewStoreRejectsEmptyPackageName(t *testing.T) {
	t.Parallel()

	rstore := NewReviewStore(store.NewStorePaths(t.TempDir()))

	r := &domain.Review{}

	if err := rstore.Save("", r); err == nil {
		t.Fatal("expected error")
	}

	if _, err := rstore.Get(""); err == nil {
		t.Fatal("expected error")
	}

	if err := rstore.Delete(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewStoreRejectsNilReview(t *testing.T) {
	t.Parallel()

	rstore := NewReviewStore(store.NewStorePaths(t.TempDir()))

	if err := rstore.Save("yay", nil); err == nil {
		t.Fatal("expected error")
	}
}
