package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/repodb"
)

// lastSuccessGetter is the part of domain.BuildStore checkRepository needs.
type lastSuccessGetter interface {
	GetLastSuccess(pkg domain.PackageName) (*domain.Build, error)
}

// artifactGetter is the part of store.ArtifactStore checkRepository needs.
type artifactGetter interface {
	Get(id domain.ArtifactID) (*domain.Artifact, error)
}

// repoCheckResult counts the problems checkRepository found, one counter
// per case in workflow.md § Check (Consistency), check 3.
type repoCheckResult struct {
	fileProblems int // a database entry's file is missing or does not match
	notMarked    int // in the repository, last success not marked Published
	notInRepo    int // last success marked Published, not (all) in the repository
	findings     []string
}

// checkRepository compares the local pacman repository with its own
// database, and the packages' last-success records with the database
// (check 3, #378; workflow.md § Published: Stored Flag, Its Limits, and
// Drift Detection). It is report-only: it never changes a record, the
// database, or a repository file.
//
// A database that does not exist means nothing is published. A database
// that cannot be read skips every comparison, with one finding. A file or
// record that cannot be read skips only the comparisons that need it, with
// a finding; it is never counted as a match or a mismatch.
func checkRepository(ctx context.Context, activityLogPath, repoDir, dbName string,
	bs lastSuccessGetter, as artifactGetter, pkgs []domain.PackageName) repoCheckResult {

	var r repoCheckResult
	snap, err := repodb.Read(filepath.Join(repoDir, dbName))
	if err != nil {
		_ = aurlog.Log(ctx, activityLogPath, "check", "repository", "check3", "repo_db", "status", "failed", "error", err.Error())
		r.findings = append(r.findings, "repository check skipped: "+sweepErrText(err))
		return r
	}

	// Repository file vs its database entry.
	inDB := make(map[string]bool, len(snap.Entries))
	for _, e := range snap.Entries {
		inDB[e.SHA256] = true
		checkRepoFile(ctx, activityLogPath, repoDir, e, &r)
	}

	// Last success vs database.
	for _, pkg := range pkgs {
		checkPublished(ctx, activityLogPath, bs, as, pkg, inDB, &r)
	}
	return r
}

// checkRepoFile checks that the file a database entry names exists in the
// repository directory and has the entry's SHA-256.
func checkRepoFile(ctx context.Context, activityLogPath, repoDir string, e repodb.Entry, r *repoCheckResult) {
	const remedy = "rebuild, Confirm, Publish (or re-run repo-add by hand with the right file)"
	if e.Filename != filepath.Base(e.Filename) || e.Filename == "." || e.Filename == ".." {
		r.fileProblems++
		_ = aurlog.Log(ctx, activityLogPath, "check", e.Name, "check3", "repo_file", "status", "bad_filename", "filename", e.Filename)
		r.findings = append(r.findings, fmt.Sprintf("repository entry %q: filename %q is not a plain file name", e.Name, e.Filename))
		return
	}
	path := filepath.Join(repoDir, e.Filename)
	sum, err := fileSHA256(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.fileProblems++
		_ = aurlog.Log(ctx, activityLogPath, "check", e.Name, "check3", "repo_file", "status", "missing", "path", path)
		r.findings = append(r.findings, fmt.Sprintf("repository entry %q (%s): file %q is missing; remedy: %s", e.Name, e.Version, e.Filename, remedy))
	case err != nil:
		_ = aurlog.Log(ctx, activityLogPath, "check", e.Name, "check3", "repo_file", "status", "unreadable", "path", path, "error", err.Error())
		r.findings = append(r.findings, fmt.Sprintf("repository entry %q (%s): file %q could not be read, comparison skipped: %v", e.Name, e.Version, e.Filename, err))
	case sum != e.SHA256:
		r.fileProblems++
		_ = aurlog.Log(ctx, activityLogPath, "check", e.Name, "check3", "repo_file", "status", "mismatch", "path", path, "file_sha256", sum, "db_sha256", e.SHA256)
		r.findings = append(r.findings, fmt.Sprintf("repository entry %q (%s): file %q does not match its database entry (SHA-256 differs); remedy: %s", e.Name, e.Version, e.Filename, remedy))
	}
}

// checkPublished compares a package's last success with the database: its
// Published flag against whether its artifacts' checksums are registered.
// An artifact with an empty checksum is never registered.
func checkPublished(ctx context.Context, activityLogPath string, bs lastSuccessGetter, as artifactGetter,
	pkg domain.PackageName, inDB map[string]bool, r *repoCheckResult) {

	ls, err := bs.GetLastSuccess(pkg)
	if errors.Is(err, domain.ErrNotFound) {
		return
	}
	if err != nil {
		r.findings = append(r.findings, fmt.Sprintf("package %q: last-success record unreadable, publication check skipped: %v", pkg, err))
		return
	}
	if ls == nil {
		return
	}

	registered := 0
	for _, id := range ls.ArtifactIDs {
		a, err := as.Get(id)
		if err != nil {
			r.findings = append(r.findings, fmt.Sprintf("package %q: artifact %q unreadable, publication check skipped: %v", pkg, id, err))
			return
		}
		if a.Checksum != "" && inDB[a.Checksum] {
			registered++
		}
	}
	total := len(ls.ArtifactIDs)

	switch {
	case ls.Published && registered < total:
		r.notInRepo++
		_ = aurlog.Log(ctx, activityLogPath, "check", string(pkg), "check3", "published_not_in_repo",
			"build_id", string(ls.ID), "registered", fmt.Sprint(registered), "artifacts", fmt.Sprint(total))
		r.findings = append(r.findings, fmt.Sprintf("package %q: marked Published, but %d of %d artifact(s) are not in the repository database; remedy: rebuild, Confirm, Publish (if the removal was intended, remove the package)",
			pkg, total-registered, total))
	case !ls.Published && registered > 0:
		r.notMarked++
		_ = aurlog.Log(ctx, activityLogPath, "check", string(pkg), "check3", "in_repo_not_marked",
			"build_id", string(ls.ID), "registered", fmt.Sprint(registered), "artifacts", fmt.Sprint(total))
		r.findings = append(r.findings, fmt.Sprintf("package %q: %d of %d artifact(s) are in the repository database, but the build is not marked Published; remedy: Confirm (if Built), then Publish",
			pkg, registered, total))
	}
}

// fileSHA256 returns the lowercase hex SHA-256 of the file at path,
// reading it in a stream (repository files can be large).
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
