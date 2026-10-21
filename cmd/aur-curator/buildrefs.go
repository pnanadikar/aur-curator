package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
)

// packageBuildRefs returns the build IDs whose directories a package's
// build records reference: build.json's ID, LastSuccessBuildID and
// LastFailureBuildID, plus last-success.json's ID. The result has empty
// IDs dropped and duplicates removed, in first-seen order. A package
// with no build records returns nil, nil. Any other read error is
// returned, because the caller cannot know which directories the
// package references.
func packageBuildRefs(bs domain.BuildStore, pkg domain.PackageName) ([]domain.BuildID, error) {
	var ids []domain.BuildID
	seen := make(map[domain.BuildID]bool)
	add := func(id domain.BuildID) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	b, err := bs.Get(pkg)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return nil, fmt.Errorf("read build record: %w", err)
	default:
		add(b.ID)
		add(b.LastSuccessBuildID)
		add(b.LastFailureBuildID)
	}

	ls, err := bs.GetLastSuccess(pkg)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return nil, fmt.Errorf("read last-success record: %w", err)
	default:
		add(ls.ID)
	}
	return ids, nil
}

// collectBuildRefs returns every build ID referenced by the given
// packages' build records (via packageBuildRefs) or by any artifact
// record's BuildID. It returns an error if any build record or the
// artifact list cannot be read, including a partial ListAll error:
// with unknown references, no build directory is safe to delete.
func collectBuildRefs(bs domain.BuildStore, as store.ArtifactStore, pkgs []domain.PackageName) (map[domain.BuildID]bool, error) {
	refs := make(map[domain.BuildID]bool)
	for _, pkg := range pkgs {
		ids, err := packageBuildRefs(bs, pkg)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", pkg, err)
		}
		for _, id := range ids {
			refs[id] = true
		}
	}

	arts, err := as.ListAll()
	if err != nil {
		return nil, fmt.Errorf("artifact list: %w", err)
	}
	for _, a := range arts {
		if a.BuildID != "" {
			refs[a.BuildID] = true
		}
	}
	return refs, nil
}

// orphanBuildDirs returns the names in dirs that are not in refs, in
// input order.
func orphanBuildDirs(dirs []string, refs map[domain.BuildID]bool) []string {
	var out []string
	for _, d := range dirs {
		if !refs[domain.BuildID(d)] {
			out = append(out, d)
		}
	}
	return out
}

// sweepOrphanBuildDirs deletes the directories under .aur-curator/builds/
// that no build record of pkgs and no artifact record references (check 2,
// #357). Non-directory entries are ignored. If any record cannot be read
// it deletes nothing and returns a single finding, because then no
// directory is known to be safe. It returns the number of directories
// removed and the findings to report.
func sweepOrphanBuildDirs(ctx context.Context, activityLogPath string, paths *store.StorePaths,
	bs domain.BuildStore, as store.ArtifactStore, pkgs []domain.PackageName) (int, []string) {

	entries, err := os.ReadDir(paths.BuildsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, []string{"build directory sweep skipped: read builds dir: " + sweepErrText(err)}
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		return 0, nil
	}

	refs, err := collectBuildRefs(bs, as, pkgs)
	if err != nil {
		return 0, []string{"build directory sweep skipped: " + sweepErrText(err)}
	}

	var removed int
	var findings []string
	for _, name := range orphanBuildDirs(dirs, refs) {
		dir := paths.BuildArtifactsDir(domain.BuildID(name))
		if err := os.RemoveAll(dir); err != nil {
			_ = aurlog.Log(ctx, activityLogPath, "check", name, "check2", "orphan_build_dir", "status", "failed", "error", err.Error())
			findings = append(findings, fmt.Sprintf("orphaned build dir %q: could not delete: %v", name, err))
			continue
		}
		removed++
		_ = aurlog.Log(ctx, activityLogPath, "check", name, "check2", "orphan_build_dir", "status", "success", "path", dir)
		fmt.Fprintf(os.Stderr, "check 2: removed orphaned build dir %q\n", name)
	}
	return removed, findings
}

// sweepErrText renders err on one line for a finding; a joined error
// contains newlines.
func sweepErrText(err error) string {
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
