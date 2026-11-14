package jsonstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/pkgname"
	"github.com/pramod/aur-curator/internal/store"
)

// The stores refuse an invalid name and write nothing (#504).
func TestStoresRejectInvalidName(t *testing.T) {
	ws := t.TempDir()
	paths := store.NewStorePaths(ws)
	reviews := NewReviewStore(paths)
	builds := NewBuildStore(paths)
	const bad = domain.PackageName("../escape")

	errs := map[string]error{
		"ReviewStore.Save":           reviews.Save(bad, &domain.Review{}),
		"ReviewStore.Delete":         reviews.Delete(bad),
		"BuildStore.Save":            builds.Save(bad, &domain.Build{}),
		"BuildStore.SaveLastSuccess": builds.SaveLastSuccess(bad, &domain.Build{}),
		"BuildStore.Delete":          builds.Delete(bad),
	}
	_, errs["ReviewStore.Get"] = reviews.Get(bad)
	_, errs["BuildStore.Get"] = builds.Get(bad)
	_, errs["BuildStore.GetLastSuccess"] = builds.GetLastSuccess(bad)
	for op, err := range errs {
		if err == nil || !strings.Contains(err.Error(), pkgname.Rule) {
			t.Errorf("%s(%q) = %v; want an error naming the rule", op, bad, err)
		}
	}
	// Nothing was written anywhere under the workspace, nor beside it.
	entries, err := os.ReadDir(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("workspace not empty: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ws), "escape")); err == nil {
		t.Error("a file was written outside the workspace")
	}
}
