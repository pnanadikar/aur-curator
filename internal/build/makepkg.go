// Package build provides a makepkg concrete Builder implementation.
package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pramod/aur-curator/internal/procgroup"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/trace"
)

// MakepkgBuilder executes packages using makepkg.
type MakepkgBuilder struct{}

// Build runs makepkg inside workspaceDir, capturing output to logPath
// and returning paths to produced .pkg.tar.* artifacts.
func (MakepkgBuilder) Build(ctx context.Context, workspaceDir string, logPath string) (BuildResult, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("create log directory %s: %w", filepath.Dir(logPath), err)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("create log file: %w", err)
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "makepkg", "--noconfirm", "--nocolor")
	cmd.Dir = workspaceDir
	cmd.Stdout = logFile
	cmd.Stderr = io.MultiWriter(logFile, &stderr)

	trace.Log(ctx, "handoff", "to", "subprocess", "from", "builder", "cmd", "makepkg")
	if err := procgroup.Run(ctx, cmd, "to", "subprocess", "from", "builder", "cmd", "makepkg"); err != nil {
		if closeErr := logFile.Close(); closeErr != nil {
			return BuildResult{}, fmt.Errorf("close log file: %w", closeErr)
		}
		if ctx.Err() != nil {
			return BuildResult{}, fmt.Errorf("makepkg cancelled: %w", ctx.Err())
		}
		return BuildResult{}, fmt.Errorf("makepkg failed: %w: %s", err, sanitize.ForMessage(stderr.String()))
	}

	if err := logFile.Close(); err != nil {
		return BuildResult{}, fmt.Errorf("close log file: %w", err)
	}

	// Collect artifacts produced in this directory.
	artifactPaths, err := findArtifacts(workspaceDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("discover artifacts: %w", err)
	}

	return BuildResult{
		LogLocation:   logPath,
		ArtifactPaths: artifactPaths,
	}, nil
}

// findArtifacts globs workspaceDir for .pkg.tar.* files and returns their
// absolute paths.
func findArtifacts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var artifacts []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if strings.HasSuffix(name, ".pkg.tar.zst") ||
			strings.HasSuffix(name, ".pkg.tar.xz") ||
			strings.HasSuffix(name, ".pkg.tar.lz") ||
			strings.HasSuffix(name, ".pkg.tar.bz2") ||
			strings.HasSuffix(name, ".pkg.tar.gz") ||
			strings.HasSuffix(name, ".pkg.tar.lzo") {
			abs, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				return nil, fmt.Errorf("resolve artifact path: %w", err)
			}
			artifacts = append(artifacts, abs)
		}
	}

	return artifacts, nil
}
