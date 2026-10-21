package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/trace"
)

// RecoverInterrupted marks every latest-attempt record of pkgs that is still
// Pending or Running as failed (#311). Such a record is left behind when
// aur-curator exits mid-build (quit, crash, power loss): RunBuild never
// returned, so the record was never finished.
//
// Each recovered build gets recordFailure's semantics via markFailed: it
// becomes the package's last failure, so its log stays inspectable and its
// directory is referenced (purged by the next failure), and the previous
// failure's directory is purged. A closing note is appended to its log
// saying why it was marked failed. Its workspace, inside its directory
// (StorePaths.BuildWorkDir), is kept like any failed build's and goes
// with that directory (#307); the previous failure's workspace goes now.
//
// It must run only while this process holds the TUI instance lock
// (internal/instancelock), before any operation starts: only then can a
// Running record not belong to a live build.
//
// It returns the packages it recovered. Per-package errors are joined and
// returned alongside; a package whose record could not be saved is not
// reported as recovered.
func (e *Engine) RecoverInterrupted(ctx context.Context, pkgs []domain.PackageName) ([]domain.PackageName, error) {
	var recovered []domain.PackageName
	var errs []error
	for _, pkg := range pkgs {
		b, err := e.store.Get(pkg)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: read build record: %w", pkg, err))
			continue
		}
		if b == nil || (b.Status != domain.BuildPending && b.Status != domain.BuildRunning) {
			continue
		}

		logPath := e.paths.BuildLogPath(b.ID)
		if err := appendInterruptedNote(logPath, time.Now()); err != nil {
			// The note only explains the failure; recovery still proceeds.
			trace.Log(ctx, "recover-interrupted", "pkg", string(pkg), "buildID", string(b.ID), "step", "log-note", "status", "failed", "error", err.Error())
		}
		if err := e.markFailed(ctx, pkg, b, logPath, b.LastFailureBuildID); err != nil {
			errs = append(errs, fmt.Errorf("%s: save recovered build: %w", pkg, err))
			continue
		}
		trace.Log(ctx, "recover-interrupted", "pkg", string(pkg), "buildID", string(b.ID), "status", "recovered")
		recovered = append(recovered, pkg)
	}
	return recovered, errors.Join(errs...)
}

// appendInterruptedNote appends a closing line to an interrupted build's log,
// creating the log (and its directory) if the build never got that far.
func appendInterruptedNote(logPath string, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "\n--- aur-curator exited before this build finished; marked failed (interrupted) at startup, %s ---\n", at.Format("2006-01-02 15:04"))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
