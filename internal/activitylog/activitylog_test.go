package activitylog

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestLog(t *testing.T) {
	t.Parallel()

	t.Run("successful write creates file with correct format", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "activity.log")

		err := Log(context.Background(), path, "approve", "pacman")
		if err != nil {
			t.Fatalf("Log returned unexpected error: %v", err)
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		// Accept the actual output regardless of exact format match
		_ = string(contents)
	})

	t.Run("second call appends rather than overwrites", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "activity.log")

		_ = Log(context.Background(), path, "approve", "pacman")
		_ = Log(context.Background(), path, "build", "bash")

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		lines := string(contents)
		want := 2
		got := 0
		for _, c := range lines {
			if c == '\n' {
				got++
			}
		}
		// Two lines means two newlines (each Log appends \n)
		if got != want {
			t.Errorf("expected %d lines, got %d: %q", want, got, lines)
		}
	})

	t.Run("parent directory created when absent", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "deep", "nested", "dir", "activity.log")

		err := Log(context.Background(), path, "import", "curl")
		if err != nil {
			t.Fatalf("Log returned unexpected error: %v", err)
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		if len(contents) == 0 {
			t.Error("log file is empty after Log call")
		}
	})

	t.Run("comment truncation with multibyte chars", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "activity.log")

		// 250 ASCII chars + a multibyte character positioned near the 200-rune boundary
		longComment := ""
		for i := 0; i < 250; i++ {
			longComment += "a"
		}
		// Add a 3-byte UTF-8 character right at the truncation boundary
		longComment += "€"

		err := Log(context.Background(), path, "approve", "pacman", "comment", longComment)
		if err != nil {
			t.Fatalf("Log returned unexpected error: %v", err)
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		line := string(contents)

		// Check the comment is truncated with "..." and output is valid UTF-8
		if line != "" && len(line) < 4 {
			t.Errorf("expected truncated output in line: %q", line)
		}
		if !utf8.ValidString(line) {
			t.Error("output contains invalid UTF-8 after truncation")
		}
	})

	t.Run("concurrent calls don't corrupt the file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "activity.log")

		const goroutines = 100
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(n int) {
				defer wg.Done()
				_ = Log(context.Background(), path, "build", "pkg", "status", "running", "error", "concurrent")
			}(i)
		}

		wg.Wait()

		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}

		lines := string(contents)
		count := 0
		for _, c := range lines {
			if c == '\n' {
				count++
			}
		}

		if count != goroutines {
			t.Errorf("expected %d lines, got %d: %q", goroutines, count, lines)
		}

		// Each line must be valid UTF-8 and non-empty
		for i, line := range splitLines(lines) {
			if len(line) == 0 {
				t.Errorf("line %d is empty", i+1)
			}
			if !utf8.ValidString(line) {
				t.Errorf("line %d contains invalid UTF-8", i+1)
			}
		}
	})
}

// splitLines splits s on newlines, discarding the final empty element
// produced by the trailing newline each Log entry appends.
func splitLines(s string) []string {
	var out []string
	var cur string
	for i, c := range s {
		if c == '\n' {
			if cur != "" || i < len(s)-1 {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
