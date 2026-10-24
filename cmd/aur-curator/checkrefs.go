package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/domain"
)

// buildSaver is the part of domain.BuildStore that
// removeDanglingArtifactRefs needs.
type buildSaver interface {
	Save(pkg domain.PackageName, b *domain.Build) error
}

// removeDanglingArtifactRefs removes from build.ArtifactIDs every ID whose
// artifact record does not exist, then saves the build (check 2, #359).
// A record that exists but cannot be read is not missing: its reference is
// kept and reported. Removals are counted and logged as auto-fixed only
// after the save succeeds; a failed save is logged and reported instead,
// and build.ArtifactIDs is left as it was. It returns the number of
// references removed and the findings to report.
func removeDanglingArtifactRefs(ctx context.Context, activityLogPath string, bs buildSaver, as domain.ArtifactStore,
	pkg domain.PackageName, build *domain.Build) (int, []string) {

	var kept, dangling []domain.ArtifactID
	var findings []string
	for _, artID := range build.ArtifactIDs {
		_, err := as.Get(artID)
		switch {
		case err == nil:
			kept = append(kept, artID)
		case errors.Is(err, domain.ErrNotFound):
			dangling = append(dangling, artID)
		default:
			// Unreadable, not missing: keep the reference and say so.
			kept = append(kept, artID)
			findings = append(findings, fmt.Sprintf("package %q: artifact %q unreadable, reference kept: %v", pkg, artID, err))
		}
	}
	if len(dangling) == 0 {
		return 0, findings
	}

	original := build.ArtifactIDs
	build.ArtifactIDs = kept
	if err := bs.Save(pkg, build); err != nil {
		build.ArtifactIDs = original
		_ = aurlog.Log(ctx, activityLogPath, "check", string(pkg), "check2", "dangling_artifact_ref", "status", "failed", "error", err.Error())
		findings = append(findings, fmt.Sprintf("package %q: could not remove %d dangling artifact ref(s): %v", pkg, len(dangling), err))
		return 0, findings
	}
	for _, artID := range dangling {
		_ = aurlog.Log(ctx, activityLogPath, "check", string(pkg), "check2", "dangling_artifact_ref", "artifact_id", string(artID), "status", "auto_fixed")
		fmt.Fprintf(os.Stderr, "check 2: removed dangling artifact ref %q from build of %q\n", artID, pkg)
	}
	return len(dangling), findings
}
