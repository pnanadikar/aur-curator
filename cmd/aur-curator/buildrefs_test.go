package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

func TestPackageBuildRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore)
		expectIDs   []domain.BuildID
		expectErr   bool
		errContains string
	}{
		{
			name:      "no records",
			setup:     func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {},
			expectIDs: nil,
			expectErr: false,
		},
		{
			name: "last-success only",
			setup: func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {
				ls := &domain.Build{ID: "b2"}
				if err := bs.SaveLastSuccess("foo", ls); err != nil {
					t.Fatal(err)
				}
			},
			expectIDs: []domain.BuildID{"b2"},
			expectErr: false,
		},
		{
			name: "latest only",
			setup: func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {
				b := &domain.Build{ID: "b1", PackageName: "foo", Status: domain.BuildSuccess}
				if err := bs.Save("foo", b); err != nil {
					t.Fatal(err)
				}
			},
			expectIDs: []domain.BuildID{"b1"},
			expectErr: false,
		},
		{
			name: "all three plus last-success",
			setup: func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {
				b := &domain.Build{
					ID: "b3", PackageName: "foo", Status: domain.BuildSuccess,
					LastSuccessBuildID: "b2", LastFailureBuildID: "b3",
				}
				if err := bs.Save("foo", b); err != nil {
					t.Fatal(err)
				}
				ls := &domain.Build{ID: "b2"}
				if err := bs.SaveLastSuccess("foo", ls); err != nil {
					t.Fatal(err)
				}
			},
			expectIDs: []domain.BuildID{"b3", "b2"},
			expectErr: false,
		},
		{
			name: "corrupt build.json",
			setup: func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {
				pkgDir := paths.PackageDir("foo")
				if err := os.MkdirAll(pkgDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.BuildPath("foo"), []byte("{bad"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			expectIDs:   nil,
			expectErr:   true,
			errContains: "read build record",
		},
		{
			name: "corrupt last-success.json",
			setup: func(dir string, paths *store.StorePaths, bs *jsonstore.BuildStore) {
				b := &domain.Build{ID: "b1", PackageName: "foo", Status: domain.BuildFailed}
				if err := bs.Save("foo", b); err != nil {
					t.Fatal(err)
				}
				pkgDir := paths.PackageDir("foo")
				if err := os.MkdirAll(pkgDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.LastSuccessBuildPath("foo"), []byte("{bad"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			expectIDs:   nil,
			expectErr:   true,
			errContains: "read last-success record",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			paths := store.NewStorePaths(dir)
			bs := jsonstore.NewBuildStore(paths)

			tt.setup(dir, paths, bs)

			ids, err := packageBuildRefs(bs, "foo")
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(ids) != len(tt.expectIDs) {
				t.Fatalf("expected %d IDs, got %d: %v", len(tt.expectIDs), len(ids), ids)
			}
			for i, id := range tt.expectIDs {
				if ids[i] != id {
					t.Fatalf("expected ids[%d]=%s, got %s", i, id, ids[i])
				}
			}
		})
	}
}

// sweepStores returns a temp workspace with real JSON build and artifact
// stores rooted in it.
func sweepStores(t *testing.T) (string, *store.StorePaths, *jsonstore.BuildStore, *jsonstore.ArtifactStore) {
	t.Helper()
	dir := t.TempDir()
	paths := store.NewStorePaths(dir)
	return dir, paths, jsonstore.NewBuildStore(paths), jsonstore.NewArtifactStore(paths)
}

func mustSaveBuild(t *testing.T, bs *jsonstore.BuildStore, pkg domain.PackageName, b *domain.Build) {
	t.Helper()
	if err := bs.Save(pkg, b); err != nil {
		t.Fatal(err)
	}
}

func mustSaveArtifact(t *testing.T, as *jsonstore.ArtifactStore, id domain.ArtifactID, buildID domain.BuildID, pkg domain.PackageName) {
	t.Helper()
	if err := as.Save(id, &domain.Artifact{ID: id, BuildID: buildID, PackageName: pkg}); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanBuildDirs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dirs []string
		refs map[domain.BuildID]bool
		want []string
	}{
		{"some referenced", []string{"a", "b", "c"}, map[domain.BuildID]bool{"a": true, "c": true}, []string{"b"}},
		{"no refs", []string{"a", "b"}, map[domain.BuildID]bool{}, []string{"a", "b"}},
		{"no dirs", nil, map[domain.BuildID]bool{"a": true}, nil},
		{"empty ref matches nothing", []string{"a"}, map[domain.BuildID]bool{"": true}, []string{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := orphanBuildDirs(tt.dirs, tt.refs)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("orphanBuildDirs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollectBuildRefs(t *testing.T) {
	t.Parallel()

	t.Run("build records and artifacts", func(t *testing.T) {
		t.Parallel()
		_, _, bs, as := sweepStores(t)
		mustSaveBuild(t, bs, "foo", &domain.Build{ID: "f2", Status: domain.BuildFailed, LastSuccessBuildID: "f1", LastFailureBuildID: "f2"})
		mustSaveBuild(t, bs, "bar", &domain.Build{ID: "r1", Status: domain.BuildSuccess, LastSuccessBuildID: "r1"})
		mustSaveArtifact(t, as, "art-x", "bx", "foo")

		refs, err := collectBuildRefs(bs, as, []domain.PackageName{"foo", "bar", "none"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, id := range []domain.BuildID{"f1", "f2", "r1", "bx"} {
			if !refs[id] {
				t.Fatalf("refs missing %q: %v", id, refs)
			}
		}
		if len(refs) != 4 {
			t.Fatalf("refs = %v, want exactly f1, f2, r1, bx", refs)
		}
	})

	t.Run("corrupt build record", func(t *testing.T) {
		t.Parallel()
		_, paths, bs, as := sweepStores(t)
		mustWrite(t, paths.BuildPath("foo"), "{bad")

		_, err := collectBuildRefs(bs, as, []domain.PackageName{"foo"})
		if err == nil || !strings.Contains(err.Error(), "package foo") {
			t.Fatalf("err = %v, want error naming package foo", err)
		}
	})

	t.Run("corrupt artifact record", func(t *testing.T) {
		t.Parallel()
		_, paths, bs, as := sweepStores(t)
		mustSaveArtifact(t, as, "good", "b1", "foo")
		mustWrite(t, paths.ArtifactPath("bad"), "{bad")

		_, err := collectBuildRefs(bs, as, []domain.PackageName{"foo"})
		if err == nil || !strings.Contains(err.Error(), "artifact list") {
			t.Fatalf("err = %v, want artifact list error", err)
		}
	})
}

func TestSweepOrphanBuildDirs(t *testing.T) {
	t.Parallel()

	t.Run("removes only unreferenced dirs", func(t *testing.T) {
		t.Parallel()
		dir, paths, bs, as := sweepStores(t)
		mustSaveBuild(t, bs, "foo", &domain.Build{ID: "b1", Status: domain.BuildSuccess, LastSuccessBuildID: "b1"})
		mustSaveArtifact(t, as, "art-2", "b2", "foo")
		for _, name := range []string{"b1", "b2", "orphan"} {
			mustWrite(t, filepath.Join(paths.BuildArtifactsDir(domain.BuildID(name)), "build.log"), "log")
		}
		stray := filepath.Join(paths.BuildsDir(), "stray.txt")
		mustWrite(t, stray, "x")

		removed, findings := sweepOrphanBuildDirs(context.Background(), filepath.Join(dir, "activity.log"), paths, bs, as, []domain.PackageName{"foo"})
		if removed != 1 || len(findings) != 0 {
			t.Fatalf("removed=%d findings=%v, want 1 and none", removed, findings)
		}
		if _, err := os.Stat(paths.BuildArtifactsDir("orphan")); !os.IsNotExist(err) {
			t.Fatalf("orphan dir still exists (err=%v)", err)
		}
		for _, keep := range []string{paths.BuildArtifactsDir("b1"), paths.BuildArtifactsDir("b2"), stray} {
			if _, err := os.Stat(keep); err != nil {
				t.Fatalf("%s should still exist: %v", keep, err)
			}
		}
	})

	t.Run("unreadable record deletes nothing", func(t *testing.T) {
		t.Parallel()
		dir, paths, bs, as := sweepStores(t)
		mustWrite(t, paths.BuildPath("foo"), "{bad")
		mustWrite(t, filepath.Join(paths.BuildArtifactsDir("orphan"), "build.log"), "log")

		removed, findings := sweepOrphanBuildDirs(context.Background(), filepath.Join(dir, "activity.log"), paths, bs, as, []domain.PackageName{"foo"})
		if removed != 0 || len(findings) != 1 || !strings.Contains(findings[0], "build directory sweep skipped") {
			t.Fatalf("removed=%d findings=%v, want 0 and one skip finding", removed, findings)
		}
		if _, err := os.Stat(paths.BuildArtifactsDir("orphan")); err != nil {
			t.Fatalf("orphan dir should still exist: %v", err)
		}
	})

	t.Run("no builds dir", func(t *testing.T) {
		t.Parallel()
		dir, paths, bs, as := sweepStores(t)
		removed, findings := sweepOrphanBuildDirs(context.Background(), filepath.Join(dir, "activity.log"), paths, bs, as, nil)
		if removed != 0 || findings != nil {
			t.Fatalf("removed=%d findings=%v, want 0 and nil", removed, findings)
		}
	})
}
