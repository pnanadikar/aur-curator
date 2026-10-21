package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// failingSaver is a build store whose Save always fails.
type failingSaver struct{}

func (failingSaver) Save(domain.PackageName, *domain.Build) error { return errors.New("disk full") }

func idsEqual(a, b []domain.ArtifactID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRemoveDanglingArtifactRefs(t *testing.T) {
	t.Parallel()

	t.Run("nothing dangling", func(t *testing.T) {
		t.Parallel()
		dir, _, bs, as := sweepStores(t)
		mustSaveArtifact(t, as, "a1", "b1", "foo")
		b := &domain.Build{ID: "b1", ArtifactIDs: []domain.ArtifactID{"a1"}}
		mustSaveBuild(t, bs, "foo", b)

		removed, findings := removeDanglingArtifactRefs(context.Background(), filepath.Join(dir, "activity.log"), bs, as, "foo", b)
		if removed != 0 || len(findings) != 0 {
			t.Fatalf("removed=%d findings=%v, want 0 and none", removed, findings)
		}
	})

	t.Run("missing record removed and saved", func(t *testing.T) {
		t.Parallel()
		dir, _, bs, as := sweepStores(t)
		mustSaveArtifact(t, as, "a1", "b1", "foo")
		b := &domain.Build{ID: "b1", ArtifactIDs: []domain.ArtifactID{"a1", "gone"}}
		mustSaveBuild(t, bs, "foo", b)

		removed, findings := removeDanglingArtifactRefs(context.Background(), filepath.Join(dir, "activity.log"), bs, as, "foo", b)
		if removed != 1 || len(findings) != 0 {
			t.Fatalf("removed=%d findings=%v, want 1 and none", removed, findings)
		}
		saved, err := bs.Get("foo")
		if err != nil {
			t.Fatal(err)
		}
		if !idsEqual(saved.ArtifactIDs, []domain.ArtifactID{"a1"}) {
			t.Fatalf("saved ArtifactIDs = %v, want [a1]", saved.ArtifactIDs)
		}
	})

	t.Run("unreadable record kept and reported", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("running as root: permissions are not enforced")
		}
		dir, paths, bs, as := sweepStores(t)
		mustSaveArtifact(t, as, "a1", "b1", "foo")
		mustSaveArtifact(t, as, "bad", "b1", "foo")
		badPath := paths.ArtifactPath("bad")
		if err := os.Chmod(badPath, 0o000); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(badPath, 0o644) }()
		b := &domain.Build{ID: "b1", ArtifactIDs: []domain.ArtifactID{"a1", "bad"}}
		mustSaveBuild(t, bs, "foo", b)

		removed, findings := removeDanglingArtifactRefs(context.Background(), filepath.Join(dir, "activity.log"), bs, as, "foo", b)
		if removed != 0 || len(findings) != 1 || !strings.Contains(findings[0], "unreadable, reference kept") {
			t.Fatalf("removed=%d findings=%v, want 0 and one 'reference kept' finding", removed, findings)
		}
		saved, err := bs.Get("foo")
		if err != nil {
			t.Fatal(err)
		}
		if !idsEqual(saved.ArtifactIDs, []domain.ArtifactID{"a1", "bad"}) {
			t.Fatalf("saved ArtifactIDs = %v, want [a1 bad] unchanged", saved.ArtifactIDs)
		}
	})

	t.Run("missing removed, unreadable kept", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("running as root: permissions are not enforced")
		}
		dir, paths, bs, as := sweepStores(t)
		mustSaveArtifact(t, as, "bad", "b1", "foo")
		badPath := paths.ArtifactPath("bad")
		if err := os.Chmod(badPath, 0o000); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(badPath, 0o644) }()
		b := &domain.Build{ID: "b1", ArtifactIDs: []domain.ArtifactID{"gone", "bad"}}
		mustSaveBuild(t, bs, "foo", b)

		removed, findings := removeDanglingArtifactRefs(context.Background(), filepath.Join(dir, "activity.log"), bs, as, "foo", b)
		if removed != 1 || len(findings) != 1 {
			t.Fatalf("removed=%d findings=%v, want 1 and one finding", removed, findings)
		}
		saved, err := bs.Get("foo")
		if err != nil {
			t.Fatal(err)
		}
		if !idsEqual(saved.ArtifactIDs, []domain.ArtifactID{"bad"}) {
			t.Fatalf("saved ArtifactIDs = %v, want [bad]", saved.ArtifactIDs)
		}
	})

	t.Run("save failure reported, nothing counted", func(t *testing.T) {
		t.Parallel()
		dir, _, _, as := sweepStores(t)
		mustSaveArtifact(t, as, "a1", "b1", "foo")
		orig := []domain.ArtifactID{"a1", "gone"}
		b := &domain.Build{ID: "b1", ArtifactIDs: append([]domain.ArtifactID(nil), orig...)}

		removed, findings := removeDanglingArtifactRefs(context.Background(), filepath.Join(dir, "activity.log"), failingSaver{}, as, "foo", b)
		if removed != 0 || len(findings) != 1 || !strings.Contains(findings[0], "could not remove 1 dangling") {
			t.Fatalf("removed=%d findings=%v, want 0 and one save-failure finding", removed, findings)
		}
		if !idsEqual(b.ArtifactIDs, orig) {
			t.Fatalf("build.ArtifactIDs = %v, want restored %v", b.ArtifactIDs, orig)
		}
	})
}
