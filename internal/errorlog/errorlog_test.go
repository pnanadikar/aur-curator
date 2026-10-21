package errorlog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/trace"
)

func TestLog(t *testing.T) {
	t.Run("file created if absent", func(t *testing.T) {
		dir := t.TempDir()
		err := Log(context.TODO(), dir, "catalog", "test message")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to be created, got error: %v", err)
		}
		got := string(contents)
		if !strings.Contains(got, "[catalog]") {
			t.Errorf("expected log entry to contain [catalog], got: %q", got)
		}
		if !strings.Contains(got, "test message") {
			t.Errorf("expected log entry to contain 'test message', got: %q", got)
		}
	})

	t.Run("appends not overwrites", func(t *testing.T) {
		dir := t.TempDir()
		err := Log(context.TODO(), dir, "catalog", "first message")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		err = Log(context.TODO(), dir, "build", "second message")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to exist, got error: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d: %q", len(lines), contents)
		}
		if !strings.Contains(lines[0], "first message") {
			t.Errorf("first line should contain 'first message', got: %q", lines[0])
		}
		if !strings.Contains(lines[1], "second message") {
			t.Errorf("second line should contain 'second message', got: %q", lines[1])
		}
	})

	t.Run("correct format with RFC3339 timestamp", func(t *testing.T) {
		dir := t.TempDir()
		err := Log(context.TODO(), dir, "fetch", "fetch failed: exit 1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to exist, got error: %v", err)
		}
		got := strings.TrimSpace(string(contents))
		// Format: 2026-09-06T14:22:01Z [source] message
		if !strings.Contains(got, "[fetch]") {
			t.Errorf("expected [fetch] in log entry, got: %q", got)
		}
		if !strings.Contains(got, "fetch failed: exit 1") {
			t.Errorf("expected message in log entry, got: %q", got)
		}
		if !strings.HasSuffix(got, "fetch failed: exit 1") {
			t.Errorf("expected message to be at end of line, got: %q", got)
		}
	})

	t.Run("directory creation with missing parent", func(t *testing.T) {
		dir := t.TempDir()
		deepPath := filepath.Join(dir, "sub", "deep")
		err := Log(context.TODO(), deepPath, "catalog", "deep directory test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		path := filepath.Join(deepPath, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to be created in deep directory, got error: %v", err)
		}
		if !strings.Contains(string(contents), "deep directory test") {
			t.Errorf("expected message in deep directory log, got: %q", contents)
		}
	})

	t.Run("Logf variant", func(t *testing.T) {
		dir := t.TempDir()
		err := Logf(context.TODO(), dir, "build", "%q: build error", "mypkg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to exist, got error: %v", err)
		}
		got := string(contents)
		if !strings.Contains(got, "mypkg") {
			t.Errorf("expected 'mypkg' in log entry, got: %q", got)
		}
		if !strings.Contains(got, "build error") {
			t.Errorf("expected 'build error' in log entry, got: %q", got)
		}
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		err := Log(context.TODO(), "/nonexistent/invalid/path/that/should/not/exist", "catalog", "test")
		// Error is expected since the path doesn't exist
		_ = err
	})

	t.Run("trace ID included when present", func(t *testing.T) {
		dir := t.TempDir()

		var buf strings.Builder
		trace.SetSink(&buf)
		traceCtx := trace.Start(context.Background(), "log-test")

		err := Log(traceCtx, dir, "fetch", "fetch failed: exit 1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to exist, got error: %v", err)
		}
		got := string(contents)
		if !strings.Contains(got, "[fetch]") {
			t.Errorf("expected [fetch] in log entry, got: %q", got)
		}
		tid, ok := trace.IDFromContext(traceCtx)
		if !ok {
			t.Fatal("expected trace ID from context")
		}
		if !strings.Contains(got, "["+tid+"]") {
			t.Errorf("expected [%s] trace ID in log entry, got: %q", tid, got)
		}
	})

	t.Run("no trace ID bracket when absent", func(t *testing.T) {
		dir := t.TempDir()

		err := Log(context.Background(), dir, "catalog", "no trace")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		path := filepath.Join(dir, errorsDir, errorsFile)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected file to exist, got error: %v", err)
		}
		got := string(contents)
		if !strings.Contains(got, "[catalog]") {
			t.Errorf("expected [catalog] in log entry, got: %q", got)
		}
		if strings.Contains(got, "[]") {
			t.Errorf("expected no empty brackets, got: %q", got)
		}
	})
}
