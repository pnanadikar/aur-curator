// Package errorlog provides a lightweight, always-on persistent error log
// for operation failures and other app-level warnings.
//
// The log is an append-only text file at <RootPath>/.aur-curator/errors.log,
// using the same workspace-level convention as build logs. No log rotation
// or size management — the file grows unbounded (acceptable at this
// project's scale).
//
// This package is separate from internal/trace: trace is an opt-in
// developer diagnostic tool (gated by --trace); errorlog is always-on,
// user-facing, and records plain human-readable lines with no trace IDs.
package errorlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pramod/aur-curator/internal/trace"
)

const (
	// errorsDir is the workspace-scoped directory for aur-curator
	// auxiliary files (build logs, error log).
	errorsDir = ".aur-curator"

	// errorsFile is the append-only log file name.
	errorsFile = "errors.log"
)

// Log appends one timestamped line to <workspaceRoot>/.aur-curator/errors.log,
// creating the file and any missing parent directories if absent.
//
// source is a short label identifying the subsystem that generated the
// warning (e.g. "catalog", "fetch", "build"). message is the human-readable
// description. The entry format is:
//
//	2026-09-06T14:22:01Z [<source>] [<traceID>] <message>
//	2026-09-06T14:22:01Z [<source>] <message>
//
// The error return is best-effort: callers may ignore it since a failed
// append must not crash the application or block UI flow.
func Log(ctx context.Context, workspaceRoot, source, message string) error {
	dir := filepath.Join(workspaceRoot, errorsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	path := filepath.Join(dir, errorsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	tid, ok := trace.IDFromContext(ctx)
	if ok {
		_, err = fmt.Fprintf(f, "%s [%s] [%s] %s\n", ts, source, tid, message)
	} else {
		_, err = fmt.Fprintf(f, "%s [%s] %s\n", ts, source, message)
	}
	if err != nil {
		f.Close() //nolint:errcheck // best-effort; already returning write error
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}

	return nil
}

// Logf is like Log but accepts a format specifier.
func Logf(ctx context.Context, workspaceRoot, source, format string, args ...any) error {
	return Log(ctx, workspaceRoot, source, fmt.Sprintf(format, args...))
}
