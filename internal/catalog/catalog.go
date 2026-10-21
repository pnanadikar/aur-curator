package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// PackageCatalog discovers the curated package collection by scanning
// the filesystem directly on every call. There is deliberately no
// cache — see FilesystemCatalog's doc comment for why one was
// considered and rejected at this project's scale.
type PackageCatalog interface {
	// Packages returns the current curated package set: every direct
	// subdirectory of the workspace root. It does NOT validate that
	// each directory is a genuine two-remote Git repository — a
	// directory existing is sufficient to be listed. A malformed
	// directory surfaces later, as a real Git error, the first time
	// something performs a Git operation on it (Fetch/Diff/Merge) —
	// this is an accepted trade-off, not an oversight.
	Packages(ctx context.Context) ([]domain.PackageName, error)

	// RepositoryPath resolves the local filesystem path for a
	// package's repository directory under the workspace root. Pure
	// path computation — does not require the directory to exist, and
	// does not check the package is currently curated (callers needing
	// that should check membership in Packages() first). Errors only
	// for a structurally invalid name.
	RepositoryPath(name domain.PackageName) (string, error)
}

// FilesystemCatalog is the concrete PackageCatalog implementation.
type FilesystemCatalog struct {
	rootPath string
}

// NewFilesystemCatalog constructs a FilesystemCatalog rooted at
// rootPath (WorkspaceConfig.RootPath).
func NewFilesystemCatalog(rootPath string) *FilesystemCatalog {
	return &FilesystemCatalog{rootPath: rootPath}
}

var _ PackageCatalog = (*FilesystemCatalog)(nil)

func (c *FilesystemCatalog) Packages(ctx context.Context) ([]domain.PackageName, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(c.rootPath)
	if err != nil {
		return nil, fmt.Errorf("catalog: read workspace root %q: %w", c.rootPath, err)
	}

	var packages []domain.PackageName
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		packages = append(packages, domain.PackageName(entry.Name()))
	}
	return packages, nil
}

func (c *FilesystemCatalog) RepositoryPath(name domain.PackageName) (string, error) {
	raw := string(name)
	if raw == "" {
		return "", fmt.Errorf("catalog: package name is empty")
	}
	if strings.ContainsRune(raw, filepath.Separator) || strings.Contains(raw, "..") {
		return "", fmt.Errorf("catalog: invalid package name %q", raw)
	}
	return filepath.Join(c.rootPath, raw), nil
}
