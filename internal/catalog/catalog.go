package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/pkgname"
)

// PackageCatalog discovers the curated package collection by scanning
// the filesystem directly on every call. There is deliberately no
// cache — see FilesystemCatalog's doc comment for why one was
// considered and rejected at this project's scale.
type PackageCatalog interface {
	// Packages returns the current curated package set: every direct
	// subdirectory of the workspace root whose name does not start with
	// ".". A directory whose name breaks the AUR's rule (pkgname.Check)
	// is not a package: it is returned in rejected, with the reason, for
	// the caller to report (#504). Packages does NOT validate that each
	// directory is a genuine two-remote Git repository — a directory
	// existing is sufficient to be listed. A malformed directory surfaces
	// later, as a real Git error, the first time something performs a Git
	// operation on it (Fetch/Diff/Merge) — this is an accepted trade-off,
	// not an oversight.
	Packages(ctx context.Context) (packages []domain.PackageName, rejected []Rejected, err error)

	// RepositoryPath resolves the local filesystem path for a
	// package's repository directory under the workspace root. Pure
	// path computation — does not require the directory to exist, and
	// does not check the package is currently curated (callers needing
	// that should check membership in Packages() first). Errors only
	// for a name that breaks the AUR's rule (pkgname.Check, #504).
	RepositoryPath(name domain.PackageName) (string, error)
}

// Rejected is a directory in the workspace root that Packages left out
// because its name is not a valid package name (#504).
type Rejected struct {
	// Name is the directory's name.
	Name string
	// Err is pkgname.Check's error: it names the directory and the rule.
	Err error
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

// Packages lists the curated packages; see PackageCatalog.
func (c *FilesystemCatalog) Packages(ctx context.Context) ([]domain.PackageName, []Rejected, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	entries, err := os.ReadDir(c.rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("catalog: read workspace root %q: %w", c.rootPath, err)
	}

	var (
		packages []domain.PackageName
		rejected []Rejected
	)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := pkgname.Check(entry.Name()); err != nil {
			rejected = append(rejected, Rejected{Name: entry.Name(), Err: err})
			continue
		}
		packages = append(packages, domain.PackageName(entry.Name()))
	}
	return packages, rejected, nil
}

func (c *FilesystemCatalog) RepositoryPath(name domain.PackageName) (string, error) {
	// The AUR's rule also rules out "", ".", ".." and separators: "." would
	// name rootPath itself, and remove would delete the workspace (#493).
	if err := pkgname.Check(string(name)); err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return filepath.Join(c.rootPath, string(name)), nil
}
