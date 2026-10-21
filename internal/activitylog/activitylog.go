package activitylog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// mu serializes writes to the activity log file. Most callers are
// serialized already by the Operations Queue (only one queued operation
// executes at a time — see internal/ui/ops.go), but Approve/Reject's
// activity-log write happens outside the queue (RecordDecision is
// handled separately from OperationsQueue, per
// sprint-plan-v2-highlevel.md Increment 4), so a write there could in
// principle race with a queued operation's write. The mutex is a cheap
// guard against that gap rather than a response to routine concurrency.
var mu sync.Mutex

// maxCommentLen is the maximum length, in runes, of a "comment" value
// before truncation.
const maxCommentLen = 200

// Log appends one timestamped line to the activity log at activityLogPath,
// creating the file and any missing parent directories if absent.
//
// event is the lifecycle event name (e.g. "import", "approve", "reject",
// "merge", "build", "confirm", "publish"). pkgName is the package name.
// kv is optional key/value pairs for additional context (e.g. "status",
// "failed", "error", err.Error(), or "comment", reviewerNotes). A "comment"
// value longer than 200 runes is truncated on a rune boundary with a
// trailing "...".
//
// Entry format:
//
//	2026-09-09T14:22:01+05:30 [approve] package=pacman comment="LGTM"
//
// The error return is best-effort: callers may ignore it (prefix with
// "_ = ") since a failed append must not crash the application or block
// UI flow.
func Log(ctx context.Context, activityLogPath, event, pkgName string, kv ...string) error {
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(activityLogPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("activitylog: mkdir %s: %w", dir, err)
	}

	f, err := os.OpenFile(activityLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("activitylog: open %s: %w", activityLogPath, err)
	}

	ts := time.Now().Format(time.RFC3339)
	parts := []string{fmt.Sprintf("[%s] package=%s", event, pkgName)}
	for i := 0; i+1 < len(kv); i += 2 {
		k, v := kv[i], kv[i+1]
		if k == "comment" {
			v = truncateRunes(v, maxCommentLen)
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}

	if _, err := fmt.Fprintf(f, "%s %s\n", ts, strings.Join(parts, " ")); err != nil {
		f.Close() //nolint:errcheck // best-effort; already returning write error
		return fmt.Errorf("activitylog: write %s: %w", activityLogPath, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("activitylog: close %s: %w", activityLogPath, err)
	}

	return nil
}

// truncateRunes truncates s to at most n runes, appending "..." if
// truncated. Truncation happens on a rune boundary so multibyte UTF-8
// characters are never split.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}
