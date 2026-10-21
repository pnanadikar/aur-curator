package build

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/store"
	"github.com/pramod/aur-curator/internal/trace"
)

// Engine coordinates building a package from a reviewed commit. It
// resolves the commit to build, clones it into a disposable workspace,
// delegates to a Builder, and persists the Build record (including
// artifacts) via a BuildStore.
type Engine struct {
	builder  Builder
	store    domain.BuildStore
	artStore store.ArtifactStore
	git      git.Provider
	paths    *store.StorePaths
}

// NewEngine constructs a Build Engine.
func NewEngine(
	builder Builder,
	buildStore domain.BuildStore,
	artStore store.ArtifactStore,
	gitProvider git.Provider,
	paths *store.StorePaths,
) *Engine {
	return &Engine{
		builder:  builder,
		store:    buildStore,
		artStore: artStore,
		git:      gitProvider,
		paths:    paths,
	}
}

// BuildStore returns the BuildStore used by this engine.
func (e *Engine) BuildStore() domain.BuildStore {
	return e.store
}

// ArtifactStore returns the ArtifactStore used by this engine.
func (e *Engine) ArtifactStore() store.ArtifactStore {
	return e.artStore
}

// RunBuild builds the latest merged commit for pkgName.
func (e *Engine) RunBuild(ctx context.Context, pkgName domain.PackageName) (*domain.Build, error) {
	// 1. Resolve the commit via LocalHeadCommit — local master HEAD is the
	// stable source of truth for "what was actually merged."
	// Wrap git-bound operations with a timeout to prevent indefinite
	// hangs on network issues (#215).  The timeout covers only git
	// operations (clone/fetch) — makepkg execution uses the caller's
	// ctx unmodified, per #211's cancel-only-no-timeout decision for
	// builds (legitimate builds can take longer than gitTimeout).
	gitCtx, gitCancel := git.GitTimeoutCtx(ctx)
	defer gitCancel()
	commit, err := e.git.LocalHeadCommit(gitCtx, pkgName)
	if err != nil {
		return nil, fmt.Errorf("localHeadCommit: %w", err)
	}

	// Read current build.json to capture last-success and last-failure
	// pointers before we overwrite it with this build's pending state.
	var oldSuccessID, oldFailureID domain.BuildID
	var previous *domain.Build
	if current, err := e.store.Get(pkgName); err == nil && current != nil {
		oldSuccessID = current.LastSuccessBuildID
		oldFailureID = current.LastFailureBuildID
		previous = current
	}

	// The Build ID is assigned first: the workspace lives inside this
	// build's own directory (#307).
	build := &domain.Build{
		ID:                 domain.BuildID(generateUUID()),
		PackageName:        pkgName,
		Backend:            "makepkg",
		Status:             domain.BuildPending,
		StartTime:          time.Now(),
		ReviewedCommit:     commit,
		LastSuccessBuildID: oldSuccessID,
		LastFailureBuildID: oldFailureID,
	}

	// 2. Create the workspace inside the build's directory,
	// .aur-curator/builds/<id>/work, not in the system temp directory.
	// Whatever removes the build directory (the next failure's purge,
	// remove, check's orphan sweep) removes the workspace with it, so a
	// workspace left by a hard kill is cleaned up like a kept one (#307).
	buildDir := e.paths.BuildArtifactsDir(build.ID)
	workDir := e.paths.BuildWorkDir(build.ID)
	workspaceDir := filepath.Join(workDir, "src")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		_ = os.RemoveAll(buildDir)
		return nil, fmt.Errorf("create build workspace: %w", err)
	}

	// 3. Clone at the resolved commit. Until the pending record is saved,
	// no record names this build, so a failure removes its whole
	// directory: there is nothing to inspect, and it would be an orphan.
	if err := e.git.CloneAt(gitCtx, pkgName, commit, workspaceDir); err != nil {
		_ = os.RemoveAll(buildDir)
		return nil, fmt.Errorf("cloneAt: %w", err)
	}

	// Create a persistent log path so the log survives workspace
	// cleanup.  The Builder writes directly to this location. It is
	// recorded from the first save so the Build Log tab can show a running
	// (or interrupted) build's output (#343).
	logPath := e.paths.BuildLogPath(build.ID)
	build.LogLocation = logPath

	// 4. Save a pending Build (so a UI reading BuildStore mid-build sees
	// something).
	if err := e.store.Save(pkgName, build); err != nil {
		_ = os.RemoveAll(buildDir)
		return nil, fmt.Errorf("save pending build: %w", err)
	}

	// Cleanup: remove the workspace on success or cancel. On any failure,
	// keep it for inspection and say where it is (#360); it stays until
	// the package's next failure purges this build's directory (#307).
	// success is set only once the build and all its records are saved.
	success := false
	cancelled := false
	defer func() {
		if success || cancelled {
			_ = os.RemoveAll(workDir)
			return
		}
		e.noteKeptWorkspace(ctx, pkgName, workDir, logPath)
	}()

	// 5. Update to running.
	build.Status = domain.BuildRunning
	if err := e.store.Save(pkgName, build); err != nil {
		return nil, fmt.Errorf("save running build: %w", err)
	}
	trace.Log(ctx, "build", "state", "started", "pkg", string(pkgName), "buildID", string(build.ID), "log", logPath)

	// 6. Execute the build.
	result, err := e.builder.Build(ctx, workspaceDir, logPath)
	if err != nil {
		// 7a. On cancel: restore previous record, clean up, do NOT touch failure state.
		if errors.Is(err, context.Canceled) {
			cancelled = true
			e.handleCancelled(ctx, pkgName, build, previous)
			return nil, fmt.Errorf("build cancelled: %w", err)
		}
		// 7b. On failure: mark failed, persist, purge old failure dir.
		return e.recordFailure(ctx, pkgName, build, logPath, result.LogLocation, oldFailureID, err)
	}

	// 7. On success: populate BuildSuccess, artifacts.
	now := time.Now()
	build.Status = domain.BuildSuccess
	build.EndTime = &now
	// Keep the start-time path unless the builder reports a different one.
	if result.LogLocation != "" {
		build.LogLocation = result.LogLocation
	}

	// Persist artifacts from the disposable workspace into the same
	// persistent directory already used for the build log so that the
	// files survive the deferred workspace cleanup.
	artifactPersistDir := e.paths.BuildArtifactsDir(build.ID)
	if err := os.MkdirAll(artifactPersistDir, 0o755); err != nil {
		return e.recordFailure(ctx, pkgName, build, logPath, "", oldFailureID, fmt.Errorf("create artifact persist dir %s: %w", artifactPersistDir, err))
	}

	var artifactIDs []domain.ArtifactID
	for _, artifactPath := range result.ArtifactPaths {
		persistedPath, err := copyArtifactTo(artifactPath, artifactPersistDir)
		if err != nil {
			e.purgePartialArtifacts(ctx, pkgName, build.ID, artifactIDs)
			return e.recordFailure(ctx, pkgName, build, logPath, "", oldFailureID, fmt.Errorf("copy artifact %s: %w", artifactPath, err))
		}
		artifact, err := extractArtifact(persistedPath)
		if err != nil {
			e.purgePartialArtifacts(ctx, pkgName, build.ID, artifactIDs)
			return e.recordFailure(ctx, pkgName, build, logPath, "", oldFailureID, fmt.Errorf("extractArtifact(%s): %w", persistedPath, err))
		}
		artifact.BuildID = build.ID
		artifact.PackageName = pkgName
		artID := domain.ArtifactID(generateUUID())
		artifact.ID = artID
		if err := e.artStore.Save(artID, artifact); err != nil {
			e.purgePartialArtifacts(ctx, pkgName, build.ID, artifactIDs)
			return e.recordFailure(ctx, pkgName, build, logPath, "", oldFailureID, fmt.Errorf("save artifact: %w", err))
		}
		artifactIDs = append(artifactIDs, artID)
	}
	build.ArtifactIDs = artifactIDs

	// 9. Save final Build (success).
	build.LastSuccessBuildID = build.ID
	if err := e.store.Save(pkgName, build); err != nil {
		return e.failAfterArtifacts(ctx, pkgName, build, logPath, oldSuccessID, oldFailureID, artifactIDs, fmt.Errorf("save success build: %w", err))
	}

	// Persist the last-success record before purge.
	if err := e.store.SaveLastSuccess(pkgName, build); err != nil {
		return e.failAfterArtifacts(ctx, pkgName, build, logPath, oldSuccessID, oldFailureID, artifactIDs, fmt.Errorf("save last success: %w", err))
	}

	// The build and all its records are saved: from here on the
	// workspace is removed (#360). Purging old builds below is
	// best-effort and does not make this build a failure.
	success = true

	// Purge old artifact records and build directory.
	if err := e.purgeOldArtifacts(ctx, pkgName, build.ID); err != nil {
		_ = errorlog.Log(ctx, e.paths.RootPath(), "build", fmt.Sprintf("purge old artifacts failed for %s: %v", pkgName, err))
		trace.Log(ctx, "purge", "pkg", string(pkgName), "status", "skipped", "reason", err.Error())
	} else {
		// Purge old success directory (best-effort) only if artifact purge succeeded.
		e.purgeBuildDir(ctx, pkgName, oldSuccessID, "new-success")
	}
	return build, nil
}

// copyArtifactTo copies an artifact file from the disposable workspace
// into the persistent builds directory and returns the new path.
func copyArtifactTo(src, dstDir string) (string, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("stat source: %w", err)
	}
	if srcInfo.IsDir() {
		return "", fmt.Errorf("source is a directory: %s", src)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read source %s: %w", src, err)
	}

	dst := filepath.Join(dstDir, srcInfo.Name())
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("write to %s: %w", dst, err)
	}

	return dst, nil
}

// artifactExtensions lists the makepkg PKEXT suffixes that produce
// valid .pkg.tar.* packages. Must match the set used by findArtifacts
// in makepkg.go.
var artifactExtensions = []string{
	".pkg.tar.zst",
	".pkg.tar.xz",
	".pkg.tar.lz",
	".pkg.tar.bz2",
	".pkg.tar.gz",
	".pkg.tar.lzo",
}

// parseArtifactFilename extracts Version (pkgver-pkgrel) and Architecture
// from a makepkg artifact filename. The filename follows the pacman convention
// <pkgname>-<pkgver>-<pkgrel>-<arch>.pkg.tar.<ext>. Parsing is done from
// the right to handle hyphenated package names: the last three hyphen-delimited
// segments are arch, pkgrel, pkgver (one segment each), and everything before
// those is pkgname.
func parseArtifactFilename(filename string) (version, arch string, err error) {
	var ext string
	for _, e := range artifactExtensions {
		if strings.HasSuffix(filename, e) {
			ext = e
			break
		}
	}
	if ext == "" {
		return "", "", fmt.Errorf("unrecognized artifact extension in %q", filename)
	}

	name := strings.TrimSuffix(filename, ext)
	parts := strings.Split(name, "-")
	if len(parts) < 4 {
		return "", "", fmt.Errorf("filename %q has fewer than 4 hyphen-separated segments", filename)
	}

	arch = parts[len(parts)-1]
	pkgrel := parts[len(parts)-2]
	pkgver := parts[len(parts)-3]

	return pkgver + "-" + pkgrel, arch, nil
}

// extractArtifact creates a domain.Artifact from a built package file path.
// The path is expected to be in a persistent directory so the file survives
// after the disposable build workspace is cleaned up.
func extractArtifact(path string) (*domain.Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	filename := filepath.Base(path)
	version, arch, err := parseArtifactFilename(filename)
	if err != nil {
		return nil, fmt.Errorf("parse filename %q: %w", filename, err)
	}

	checksum, err := sha256Hash(path)
	if err != nil {
		return nil, fmt.Errorf("checksum: %w", err)
	}

	return &domain.Artifact{
		ID:           "",
		BuildID:      "",
		Filename:     filename,
		Version:      version,
		Architecture: arch,
		Checksum:     checksum,
		Size:         info.Size(),
	}, nil
}

// sha256Hash computes the lowercase hex SHA-256 digest of the file at path.
// A read failure is returned as an error, never as an empty digest: an
// artifact's checksum is what check matches against the package repository
// (#378), so an artifact must never be recorded without one.
func sha256Hash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}

// generateUUID returns a v4 UUID string.
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// purgeOldArtifacts deletes artifact records belonging to old builds,
// then returns nil if all deletes succeeded. It returns an error listing the
// first delete failure; the caller should log it and skip purging the
// corresponding build directory.
//
// The invariant: after a successful build for package P, every ArtifactStore
// record with PackageName == P has BuildID == <new build ID>.
func (e *Engine) purgeOldArtifacts(ctx context.Context, pkgName domain.PackageName, newBuildID domain.BuildID) error {
	arts, err := e.artStore.GetArtifactsByPackage(pkgName)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	var deleted int
	var firstErr error
	for _, a := range arts {
		if a.BuildID == newBuildID {
			continue
		}
		if err := e.artStore.Delete(a.ID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("delete artifact %s: %w", a.ID, err)
			}
			_ = errorlog.Log(ctx, e.paths.RootPath(), "build", fmt.Sprintf("purge old artifact failed (id=%s, pkg=%s): %v", a.ID, pkgName, err))
			trace.Log(ctx, "purge-old-artifact", "pkg", string(pkgName), "artifactID", string(a.ID), "oldBuildID", string(a.BuildID), "status", "failed", "error", err.Error())
			continue
		}
		deleted++
		trace.Log(ctx, "purge-old-artifact", "pkg", string(pkgName), "artifactID", string(a.ID), "oldBuildID", string(a.BuildID), "status", "deleted")
	}

	if firstErr != nil {
		return firstErr
	}

	trace.Log(ctx, "purge-old-artifacts", "pkg", string(pkgName), "deleted", deleted, "total", len(arts))
	return nil
}

// purgeBuildDir removes the persistent build directory for oldBuildID if it's
// non-empty. It's best-effort: errors are logged via trace and discarded. The
// caller must have already saved the new build record so the field pointer
// doesn't dangle if this fails partway.
func (e *Engine) purgeBuildDir(ctx context.Context, pkg domain.PackageName, oldBuildID domain.BuildID, reason string) {
	if oldBuildID == "" {
		return
	}

	dir := e.paths.BuildArtifactsDir(oldBuildID)
	if err := os.RemoveAll(dir); err != nil {
		trace.Log(ctx, "purge", "pkg", string(pkg), "buildID", string(oldBuildID), "path", dir, "reason", reason, "status", "failed", "error", err.Error())
		return
	}

	trace.Log(ctx, "purge", "pkg", string(pkg), "buildID", string(oldBuildID), "path", dir, "reason", reason)
}

// recordFailure marks build failed, persists it with this build as the
// last failure, then purges the previous failure's directory.
func (e *Engine) recordFailure(ctx context.Context, pkgName domain.PackageName,
	build *domain.Build, logPath string, logLocation string,
	oldFailureID domain.BuildID, cause error) (*domain.Build, error) {

	if logLocation == "" {
		logLocation = logPath
	}
	if err := e.markFailed(ctx, pkgName, build, logLocation, oldFailureID); err != nil {
		trace.Log(ctx, "record-failure", "pkg", string(pkgName), "buildID", string(build.ID), "status", "save_failed", "error", err.Error())
	}

	return build, fmt.Errorf("build failed: %w", cause)
}

// failAfterArtifacts records a build as failed when it could not be fully
// saved after its artifacts were recorded (#360). It drops the artifact
// records this build saved, restores the previous last-success pointer
// (so the record never names this build as the last success), and then
// records the failure like any other.
func (e *Engine) failAfterArtifacts(ctx context.Context, pkgName domain.PackageName,
	build *domain.Build, logPath string, oldSuccessID, oldFailureID domain.BuildID,
	artifactIDs []domain.ArtifactID, cause error) (*domain.Build, error) {

	e.purgePartialArtifacts(ctx, pkgName, build.ID, artifactIDs)
	build.ArtifactIDs = nil
	build.LastSuccessBuildID = oldSuccessID
	return e.recordFailure(ctx, pkgName, build, logPath, "", oldFailureID, cause)
}

// noteKeptWorkspace records where a failed build's workspace was kept, so
// it can be inspected (#360): one line appended to the build log, which
// the Build Log tab shows, and one in the error log. Failing to append to
// the build log is traced; it never hides the error-log line. The
// workspace is inside the build's directory, so it stays until the
// package's next failed build purges that directory (#307).
func (e *Engine) noteKeptWorkspace(ctx context.Context, pkg domain.PackageName, dir, logPath string) {
	msg := "Build workspace kept for inspection: " + dir + " (removed by the next failed build)"
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, err = fmt.Fprintf(f, "\n==> %s\n", msg)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			trace.Log(ctx, "kept-workspace", "pkg", string(pkg), "path", dir, "status", "log_append_failed", "error", err.Error())
		}
	}
	if e.paths != nil {
		_ = errorlog.Log(ctx, e.paths.RootPath(), "build", fmt.Sprintf("%s: %s", pkg, msg))
	}
}

// markFailed marks build failed and persists it as the package's latest
// attempt and last failure, then purges the previous failure's directory.
// Shared by recordFailure and RecoverInterrupted (#311).
//
// The previous failure's directory is purged only after the save
// succeeded: if the save fails, the record on disk still points at it
// (purgeBuildDir's contract), so it must not be deleted.
func (e *Engine) markFailed(ctx context.Context, pkgName domain.PackageName,
	build *domain.Build, logLocation string, oldFailureID domain.BuildID) error {

	now := time.Now()
	build.Status = domain.BuildFailed
	build.EndTime = &now
	build.LogLocation = logLocation
	build.LastFailureBuildID = build.ID

	if err := e.store.Save(pkgName, build); err != nil {
		return err
	}
	if oldFailureID != build.ID {
		e.purgeBuildDir(ctx, pkgName, oldFailureID, "new-failure")
	}
	return nil
}

// purgePartialArtifacts deletes artifact records saved during a build that
// subsequently failed. It is best-effort: save failures are traced and
// discarded so they do not mask the real error.
func (e *Engine) purgePartialArtifacts(ctx context.Context, pkgName domain.PackageName, buildID domain.BuildID, ids []domain.ArtifactID) {
	for _, id := range ids {
		if err := e.artStore.Delete(id); err != nil {
			trace.Log(ctx, "purge-partial-artifact", "pkg", string(pkgName), "buildID", string(buildID), "artifactID", string(id), "status", "failed", "error", err.Error())
		}
	}
}

// handleCancelled restores the previous build record (or deletes it if there
// was none), removes the cancelled build's persistent directory, and
// explicitly avoids touching LastFailureBuildID or a previous failure
// directory.
func (e *Engine) handleCancelled(ctx context.Context, pkg domain.PackageName, build *domain.Build, previous *domain.Build) {
	// Use a detached context for cleanup I/O so a cancelled parent context
	// does not abort filesystem operations.
	cleanupCtx := context.Background()
	if ctx != nil {
		cleanupCtx = context.WithoutCancel(ctx)
	}

	// Cancelled builds never overwrite failure pointers or failure dirs.
	// Restore what was there before (or delete the stale build.json entirely
	// if this was the first build attempt).
	if previous != nil {
		if err := e.store.Save(pkg, previous); err != nil {
			trace.Log(cleanupCtx, "cancel-restore", "pkg", string(pkg), "status", "failed", "error", err.Error())
		} else {
			trace.Log(cleanupCtx, "cancel-restore", "pkg", string(pkg), "status", "restored")
		}
	} else {
		if err := e.store.Delete(pkg); err != nil {
			trace.Log(cleanupCtx, "cancel-delete", "pkg", string(pkg), "status", "failed", "error", err.Error())
		} else {
			trace.Log(cleanupCtx, "cancel-delete", "pkg", string(pkg), "status", "deleted")
		}
	}

	// Remove this cancelled build's whole directory: log, artifacts and
	// workspace (#307). RunBuild's deferred workspace removal is then a
	// no-op.
	e.purgeBuildDir(cleanupCtx, pkg, build.ID, "cancelled")
}
