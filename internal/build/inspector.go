package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/inspect"
	"github.com/pramod/aur-curator/internal/procgroup"
	"github.com/pramod/aur-curator/internal/sandbox"
	"github.com/pramod/aur-curator/internal/sanitize"
)

// Inspector reads what a built package installs with special power, from
// the artifact file of a build (#525; domain.md § Build → Confirmed).
type Inspector interface {
	// Inspect lists the artifact's entries by category.
	Inspect(ctx context.Context, buildID domain.BuildID, filename string) (inspect.Report, error)
	// InstallScript returns the artifact's .INSTALL text.
	InstallScript(ctx context.Context, buildID domain.BuildID, filename string) (string, error)
}

// ArchiveInspector runs bsdtar on the artifact in the build sandbox: the
// build directory read-only, no network, the same hidden paths as a
// build. The archive is untrusted input to a C parser.
type ArchiveInspector struct {
	Paths SandboxPaths
}

// Output limits: an mtree line per entry, and a short script.
const (
	maxListing = 64 << 20
	maxInstall = 1 << 20
)

// Inspect implements Inspector.
func (a ArchiveInspector) Inspect(ctx context.Context, buildID domain.BuildID, filename string) (inspect.Report, error) {
	dir, file, err := a.artifact(buildID, filename)
	if err != nil {
		return inspect.Report{}, err
	}
	out, err := a.bsdtar(ctx, dir, maxListing, append(append([]string{}, inspect.MtreeArgs...), "@"+file)...)
	if err != nil {
		return inspect.Report{}, err
	}
	entries, err := inspect.ParseMtree(bytes.NewReader(out))
	if err != nil {
		return inspect.Report{}, fmt.Errorf("inspect %s: %w", filename, err)
	}
	return inspect.Classify(entries), nil
}

// InstallScript implements Inspector.
func (a ArchiveInspector) InstallScript(ctx context.Context, buildID domain.BuildID, filename string) (string, error) {
	dir, file, err := a.artifact(buildID, filename)
	if err != nil {
		return "", err
	}
	out, err := a.bsdtar(ctx, dir, maxInstall, "-xOf", file, ".INSTALL")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// artifact is the build directory and the artifact's path in it. The
// file name comes from the artifact store: a name that is not a plain
// file name is refused, as is a missing file.
func (a ArchiveInspector) artifact(buildID domain.BuildID, filename string) (dir, file string, err error) {
	if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
		return "", "", fmt.Errorf("inspect: %q is not a file name", filename)
	}
	id := string(buildID)
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return "", "", fmt.Errorf("inspect: %q is not a build ID", id)
	}
	dir = filepath.Join(a.Paths.RootPath, ".aur-curator", "builds", id)
	file = filepath.Join(dir, filename)
	info, err := os.Stat(file)
	if err != nil {
		return "", "", fmt.Errorf("inspect %s: %w", filename, err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("inspect %s: not a regular file", filename)
	}
	return dir, file, nil
}

// bsdtar runs bsdtar with args in the sandbox over dir and returns its
// stdout, at most limit bytes.
func (a ArchiveInspector) bsdtar(ctx context.Context, dir string, limit int64, args ...string) ([]byte, error) {
	p := sandbox.Policy{
		Workspace: dir,
		Home:      a.Paths.Home,
		Masks: []sandbox.Mask{
			{Path: a.Paths.ConfigFile},
			{Path: a.Paths.ActivityLog},
			{Path: a.Paths.RepositoryPath, Dir: true},
		},
		Helper: a.Paths.Helper,
		Env:    []string{"PATH=" + os.Getenv("PATH"), "HOME=" + a.Paths.Home, "LC_ALL=C"},
	}
	cmd, err := sandbox.Command(ctx, p, "bsdtar", args...)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}
	var stdout limitBuffer
	stdout.limit = limit
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := procgroup.Run(ctx, cmd, "to", "subprocess", "from", "inspector", "cmd", "bsdtar"); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("inspect cancelled: %w", ctx.Err())
		}
		if stdout.over {
			return nil, fmt.Errorf("inspect: bsdtar output over %d bytes", limit)
		}
		return nil, fmt.Errorf("bsdtar failed: %w: %s", err, sanitize.ForMessage(strings.TrimSpace(stderr.String())))
	}
	if stdout.over {
		return nil, fmt.Errorf("inspect: bsdtar output over %d bytes", limit)
	}
	return stdout.buf.Bytes(), nil
}

// limitBuffer keeps up to limit bytes and then fails the write, so a
// runaway listing stops bsdtar instead of filling memory.
type limitBuffer struct {
	buf   bytes.Buffer
	limit int64
	over  bool
}

var errOverLimit = errors.New("output over limit")

func (l *limitBuffer) Write(p []byte) (int, error) {
	if int64(l.buf.Len()+len(p)) > l.limit {
		l.over = true
		return 0, errOverLimit
	}
	return l.buf.Write(p)
}

var _ io.Writer = (*limitBuffer)(nil)
