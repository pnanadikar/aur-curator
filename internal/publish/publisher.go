// Package publish provides the Publisher that adds built package
// artifacts to the local pacman repository via repo-add.
package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/trace"
)

// Publisher publishes a built package to the local pacman repository.
type Publisher interface {
	Publish(context.Context, string) error
}

// impl is the concrete Publisher implementation.
type impl struct {
	buildStore    domain.BuildStore
	artifactStore interface {
		Get(id domain.ArtifactID) (*domain.Artifact, error)
		GetArtifactsByPackage(domain.PackageName) ([]domain.Artifact, error)
	}
	repo         domain.PackageRepository
	workspaceDir string
	repoDBName   string
	repoAdd      string // command run to update the repo db; "repo-add" except in tests
}

// NewPublisher constructs a Publisher from the shared dependencies.
func NewPublisher(
	buildStore domain.BuildStore,
	artifactStore interface {
		Get(id domain.ArtifactID) (*domain.Artifact, error)
		GetArtifactsByPackage(domain.PackageName) ([]domain.Artifact, error)
	},
	repo domain.PackageRepository,
	workspaceDir string,
	repoDBName string,
) Publisher {
	return &impl{
		buildStore:    buildStore,
		artifactStore: artifactStore,
		repo:          repo,
		workspaceDir:  workspaceDir,
		repoDBName:    repoDBName,
		repoAdd:       "repo-add",
	}
}

// Publish resolves the last-success confirmed build for pkgName, collects
// its artifact binaries, and invokes repo-add to register them in the
// local pacman repository. Returns nil on success.
func (p *impl) Publish(ctx context.Context, pkgName string) error {
	trace.Log(ctx, "publish", "phase", "start", "pkg", pkgName)

	// Resolve the LastSuccess Build record.
	build, err := p.buildStore.GetLastSuccess(domain.PackageName(pkgName))
	if err != nil {
		return fmt.Errorf("publish: get last-success build: %w", err)
	}
	if build == nil {
		return fmt.Errorf("publish: no last-success build record for %s", pkgName)
	}
	trace.Log(ctx, "publish", "phase", "resolved", "id", string(build.ID), "status", string(build.Status), "confirmed", build.Confirmed)

	// Confirmed gate — reject unconfirmed builds.
	if !build.Confirmed {
		return fmt.Errorf("publish: last-success build %s not confirmed (status: %s)", string(build.ID), string(build.Status))
	}

	// Resolve artifact binaries scoped to this build.
	var artifactPaths []string
	for _, aid := range build.ArtifactIDs {
		a, err := p.artifactStore.Get(aid)
		if err != nil {
			return fmt.Errorf("publish: get artifact %s: %w", aid, err)
		}
		path := filepath.Join(p.workspaceDir, ".aur-curator", "builds", string(build.ID), a.Filename)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("publish: artifact file missing %s: %w", a.Filename, err)
		}
		artifactPaths = append(artifactPaths, path)
	}
	if len(artifactPaths) == 0 {
		return fmt.Errorf("publish: no confirmed build artifacts found for %s", pkgName)
	}
	trace.Log(ctx, "publish", "artifacts", "found", "count", len(artifactPaths))

	// Ensure repo.Location exists.
	repoPath := p.repo.Location
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return fmt.Errorf("publish: mkdir repo %s: %w", repoPath, err)
	}

	// Put each artifact into repo.Location before invoking repo-add.
	// repo-add indexes metadata but does not copy files; pacman resolves
	// package files relative to the repo's own location, so the binaries
	// must physically reside alongside the database for external installs
	// to work (issue #267).  Copy (not symlink) so the repo remains
	// self-sufficient when build-history retention prunes old build dirs.
	//
	// Every change to the repository directory is undone if any copy or
	// repo-add fails, so a failed publish never leaves a file that differs
	// from the db entry for it (#379).
	var files repoFiles
	var dstPaths []string
	for _, srcPath := range artifactPaths {
		filename := filepath.Base(srcPath)
		dstPath := filepath.Join(repoPath, filename)
		if err := files.place(srcPath, dstPath); err != nil {
			return p.undo(ctx, pkgName, &files, fmt.Errorf("publish: put %s into repo location: %w", filename, err))
		}
		dstPaths = append(dstPaths, dstPath)
	}
	for _, stale := range files.staleBackups {
		trace.Log(ctx, "publish", "phase", "stale-backup", "pkg", pkgName, "path", stale, "status", "removed")
		_ = errorlog.Log(ctx, p.workspaceDir, "publish", fmt.Sprintf("%s: removed %s, left by an earlier publish that did not finish", pkgName, stale))
	}
	trace.Log(ctx, "publish", "artifacts", "placed", "count", len(dstPaths), "changed", len(files.placed))

	// Resolve the repo database path (after ensuring dir exists).
	repoDB := filepath.Join(repoPath, p.repoDBName)
	trace.Log(ctx, "publish", "repo", "db", "path", repoDB)

	// Invoke repo-add against the paths inside repo.Location.
	cmd := exec.CommandContext(ctx, p.repoAdd, append([]string{repoDB}, dstPaths...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return p.undo(ctx, pkgName, &files, fmt.Errorf("publish: repo-add failed (%s): %s", cmd.String(), string(output)))
	}

	// The db now names the new files: the backups are no longer needed.
	for _, err := range files.commit() {
		trace.Log(ctx, "publish", "phase", "cleanup", "pkg", pkgName, "status", "failed", "error", err.Error())
		_ = errorlog.Log(ctx, p.workspaceDir, "publish", fmt.Sprintf("%s: %v", pkgName, err))
	}
	trace.Log(ctx, "publish", "phase", "done", "pkg", pkgName, "artifactCount", len(artifactPaths))
	return nil
}

// undo restores the repository directory after a failed publish and
// returns cause, extended with any file that could not be restored. A file
// that could not be restored may now differ from the db; `check` reports
// that (#378 case C).
func (p *impl) undo(ctx context.Context, pkgName string, files *repoFiles, cause error) error {
	errs := files.rollback()
	if len(errs) == 0 {
		trace.Log(ctx, "publish", "phase", "rollback", "pkg", pkgName, "status", "ok", "count", len(files.placed))
		return cause
	}
	for _, err := range errs {
		trace.Log(ctx, "publish", "phase", "rollback", "pkg", pkgName, "status", "failed", "error", err.Error())
	}
	return fmt.Errorf("%w; repository not fully restored: %w", cause, errors.Join(errs...))
}

// backupSuffix names the copy of a repository file kept while a publish
// replaces it, until repo-add has succeeded (#379).
const backupSuffix = ".aur-curator-backup"

// placement is one change publish made in the repository directory: dst
// was written, and backup holds the file dst replaced ("" if dst was new).
type placement struct {
	dst    string
	backup string
}

// repoFiles records the changes a publish makes in the repository
// directory, so they can all be undone if any later step fails.
type repoFiles struct {
	placed       []placement
	staleBackups []string // backups left by an earlier unfinished publish, removed by place
}

// place puts src into the repository at dst. A file already at dst with the
// same content is left as is: nothing changes, nothing to undo. A different
// file at dst is first renamed to dst+backupSuffix. The new content is
// copied to a temporary file in the same directory and renamed into place,
// so dst never holds a partial copy.
func (r *repoFiles) place(src, dst string) error {
	same, err := sameFileContent(src, dst)
	if err != nil {
		return err
	}
	if same {
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".aur-curator-publish-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := copyFile(src, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	pl := placement{dst: dst}
	if _, err := os.Lstat(dst); err == nil {
		pl.backup = dst + backupSuffix
		// A backup left by an earlier publish that never finished is
		// removed: this publish replaces the file and the db entry
		// anyway. If this publish fails too, the file it restores may not
		// be the one the db names; check reports that, and the remedy is
		// to publish again.
		if _, err := os.Lstat(pl.backup); err == nil {
			if err := os.Remove(pl.backup); err != nil {
				_ = os.Remove(tmpPath)
				return fmt.Errorf("remove %s, left by an earlier publish that did not finish: %w", pl.backup, err)
			}
			r.staleBackups = append(r.staleBackups, pl.backup)
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("stat %s: %w", pl.backup, err)
		}
		if err := os.Rename(dst, pl.backup); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("back up %s: %w", dst, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("stat %s: %w", dst, err)
	}

	// Recorded before the final rename: rollback is correct whether or not
	// it happens.
	r.placed = append(r.placed, pl)
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move into place %s: %w", dst, err)
	}
	return nil
}

// rollback undoes every placement, newest first: a replaced file is
// restored from its backup, a new file is removed. It returns the
// placements it could not undo.
func (r *repoFiles) rollback() []error {
	var errs []error
	for i := len(r.placed) - 1; i >= 0; i-- {
		pl := r.placed[i]
		if pl.backup != "" {
			if err := os.Rename(pl.backup, pl.dst); err != nil {
				errs = append(errs, fmt.Errorf("restore %s from %s: %w", pl.dst, pl.backup, err))
			}
			continue
		}
		if err := os.Remove(pl.dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", pl.dst, err))
		}
	}
	return errs
}

// commit deletes the backups once the db names the new files. It returns
// the backups it could not delete; the next publish of that file name
// removes a leftover backup.
func (r *repoFiles) commit() []error {
	var errs []error
	for _, pl := range r.placed {
		if pl.backup == "" {
			continue
		}
		if err := os.Remove(pl.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove backup %s: %w", pl.backup, err))
		}
	}
	return errs
}

// sameFileContent reports whether dst exists and has the same content as
// src. A missing dst is not an error. A dst that is not a regular file is.
func sameFileContent(src, dst string) (bool, error) {
	dstInfo, err := os.Lstat(dst)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", dst, err)
	}
	if !dstInfo.Mode().IsRegular() {
		return false, fmt.Errorf("%s exists and is not a regular file", dst)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", src, err)
	}
	if srcInfo.Size() != dstInfo.Size() {
		return false, nil
	}

	a, err := os.Open(src)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = a.Close() }()
	b, err := os.Open(dst)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", dst, err)
	}
	defer func() { _ = b.Close() }()

	bufA := make([]byte, 64*1024)
	bufB := make([]byte, 64*1024)
	for {
		na, errA := io.ReadFull(a, bufA)
		nb, errB := io.ReadFull(b, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		doneA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		doneB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		if errA != nil && !doneA {
			return false, fmt.Errorf("read %s: %w", src, errA)
		}
		if errB != nil && !doneB {
			return false, fmt.Errorf("read %s: %w", dst, errB)
		}
		if doneA || doneB {
			return doneA && doneB, nil
		}
	}
}

// copyFile copies the file at src to dst. Both paths must be in the same
// filesystem (no cross-device link handling — callers ensure this).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}
