package jsonstore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// TestReviewStoreReadsReviewWithoutAURFields: a review.json written before
// #495 loads with no AUR fields, which reads as "not recorded", never as a
// change (domain.md § AUR Record).
func TestReviewStoreReadsReviewWithoutAURFields(t *testing.T) {
	t.Parallel()

	paths := store.NewStorePaths(t.TempDir())
	dir := paths.PackageDir("yay")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"yay@abc","packageName":"yay","upstreamCommit":"abc","baseCommit":"",
"status":"approved","decisionTime":"2026-11-01T10:00:00Z","reviewerNotes":"","rationale":"",
"findings":[],"findingsStatus":"run"}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := NewReviewStore(paths).Get("yay")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AUR != nil || got.AURInfo != nil || got.AURError != "" || got.PriorAUR != nil {
		t.Errorf("AUR fields = %+v %+v %q %+v, want none", got.AUR, got.AURInfo, got.AURError, got.PriorAUR)
	}
}

// TestReviewStoreRoundTripsAURFields: the four #495 fields survive a save
// and a read; an empty record keeps orphaned distinct from unset.
func TestReviewStoreRoundTripsAURFields(t *testing.T) {
	t.Parallel()

	rstore := NewReviewStore(store.NewStorePaths(t.TempDir()))
	want := &domain.Review{
		ID:             "yay@def",
		PackageName:    "yay",
		UpstreamCommit: "def",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		AUR: &domain.AURRecord{
			Maintainer:    "alice",
			CoMaintainers: []string{"bob", "carol"},
			ReadAt:        time.Date(2026, 11, 9, 5, 0, 0, 0, time.UTC),
			ReviewID:      "yay@def",
		},
		AURInfo:  &domain.AURInfo{FirstSubmitted: time.Date(2019, 1, 2, 0, 0, 0, 0, time.UTC), NumVotes: 7, Popularity: 0.25},
		AURError: "",
		PriorAUR: &domain.AURRecord{Orphaned: true, ReadAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ReviewID: "yay@abc"},
	}
	if err := rstore.Save("yay", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := rstore.Get("yay")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.AUR, want.AUR) || !reflect.DeepEqual(got.AURInfo, want.AURInfo) ||
		!reflect.DeepEqual(got.PriorAUR, want.PriorAUR) || got.AURError != want.AURError {
		t.Errorf("got AUR %+v, AURInfo %+v, PriorAUR %+v, AURError %q\nwant AUR %+v, AURInfo %+v, PriorAUR %+v, AURError %q",
			got.AUR, got.AURInfo, got.PriorAUR, got.AURError, want.AUR, want.AURInfo, want.PriorAUR, want.AURError)
	}
}
