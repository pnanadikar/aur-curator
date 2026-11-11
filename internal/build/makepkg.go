// Package build provides a makepkg concrete Builder implementation.
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

	"github.com/pramod/aur-curator/internal/procgroup"
	"github.com/pramod/aur-curator/internal/sandbox"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/trace"
)

// scratchDir is a writable directory inside every sandbox (an empty
// tmpfs), where the --printsrcinfo run's destinations point.
const scratchDir = "/tmp"

// Backend is recorded in every Build: makepkg in the bubblewrap sandbox
// (#496). Builds before #496 recorded "makepkg".
const Backend = "makepkg+bwrap"

// SandboxPaths are the host paths a build's sandbox is made from
// (domain.md § Build → Build Environment).
type SandboxPaths struct {
	// RootPath and RepositoryPath are absent inside, apart from the
	// path down to the workspace; SelfTest proves it.
	RootPath       string
	RepositoryPath string
	// ConfigFile and ActivityLog must not be readable inside.
	ConfigFile  string
	ActivityLog string
	// Helper is the aur-curator binary, which starts the command
	// inside (sandbox.ExecArg).
	Helper string
	// Home is the user's home directory: an empty tmpfs inside, with
	// the makepkg configuration file and the keybox bound in.
	Home string
}

// MakepkgBuilder builds with makepkg inside the bubblewrap sandbox.
// There is no unsandboxed path: a sandbox that cannot be set up fails
// the build (#496).
type MakepkgBuilder struct {
	Paths SandboxPaths
}

// NewMakepkgBuilder returns a MakepkgBuilder for paths.
func NewMakepkgBuilder(paths SandboxPaths) MakepkgBuilder {
	return MakepkgBuilder{Paths: paths}
}

// Build runs makepkg in workspaceDir inside the sandbox, capturing
// output to logPath, and returns the produced .pkg.tar.* artifacts.
// The sequence is domain.md § Build Environment's: self-test, read
// validpgpkeys offline, export the keybox on the host, build.
func (b MakepkgBuilder) Build(ctx context.Context, workspaceDir string, logPath string) (BuildResult, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return BuildResult{}, fmt.Errorf("create log directory %s: %w", filepath.Dir(logPath), err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return BuildResult{}, fmt.Errorf("create log file: %w", err)
	}
	result, buildErr := b.build(ctx, workspaceDir, logPath, logFile)
	if err := logFile.Close(); err != nil && buildErr == nil {
		return BuildResult{}, fmt.Errorf("close log file: %w", err)
	}
	return result, buildErr
}

// build is Build after the log is open; Build closes it.
func (b MakepkgBuilder) build(ctx context.Context, workspaceDir, logPath string, logFile io.Writer) (BuildResult, error) {
	if b.Paths.Helper == "" {
		writeLog(logFile, "aur-curator: "+errNoHelper.Error())
		return BuildResult{}, errNoHelper
	}
	if b.Paths.Home == "" {
		writeLog(logFile, "aur-curator: "+errNoHome.Error())
		return BuildResult{}, errNoHome
	}
	env, names := buildEnv(os.Environ(), b.Paths.Home, workspaceDir)
	// The log's first line names what the build gets, never the values.
	if _, err := fmt.Fprintln(logFile, envLogLine(names)); err != nil {
		return BuildResult{}, fmt.Errorf("write log file: %w", err)
	}

	policy := b.policy(workspaceDir, env)

	// 1. Self-test, under the build's own policy.
	rep, err := sandbox.SelfTest(ctx, policy, b.targets())
	for _, info := range rep.Info {
		writeLog(logFile, "aur-curator: sandbox: "+info)
	}
	for _, f := range rep.Failed() {
		writeLog(logFile, fmt.Sprintf("aur-curator: sandbox self-test failed: %s: %s", f.Check, f.Detail))
	}
	if err != nil {
		if ctx.Err() != nil {
			return BuildResult{}, fmt.Errorf("makepkg cancelled: %w", ctx.Err())
		}
		return BuildResult{}, err
	}

	// 2. validpgpkeys, from a read-only workspace. The network stays on:
	// sourcing a PKGBUILD runs its top-level code, which may fetch
	// (tor-browser-bin reads its checksums from upstream with curl), and
	// the build itself has the network until P6 anyway.
	// makepkg refuses to start, even for --printsrcinfo, unless
	// $BUILDDIR is writable. The workspace is not, here, so the
	// destinations point at the sandbox's own /tmp: an empty tmpfs,
	// discarded with the run.
	srcinfo := policy
	srcinfo.WorkspaceWritable = false
	srcinfo.Env, _ = buildEnv(os.Environ(), b.Paths.Home, scratchDir)
	var out, stderr bytes.Buffer
	if err := b.run(ctx, srcinfo, &out, io.MultiWriter(logFile, &stderr), "makepkg", "--printsrcinfo"); err != nil {
		if ctx.Err() != nil {
			return BuildResult{}, fmt.Errorf("makepkg cancelled: %w", ctx.Err())
		}
		return BuildResult{}, fmt.Errorf("makepkg --printsrcinfo failed: %w: %s", err, sanitize.ForMessage(stderr.String()))
	}
	keys, err := validPGPKeys(out.String())
	if err != nil {
		writeLog(logFile, "aur-curator: "+err.Error())
		return BuildResult{}, err
	}

	// 3. The keybox, on the host, outside the workspace.
	kbx, err := exportKeys(ctx, keys, filepath.Join(filepath.Dir(logPath), "keyring"))
	if err != nil {
		writeLog(logFile, "aur-curator: "+err.Error())
		return BuildResult{}, err
	}
	if kbx != "" {
		policy.ReadOnly = append(policy.ReadOnly, sandbox.Bind{Src: kbx, Dest: filepath.Join(b.Paths.Home, ".gnupg", keyboxName)})
		writeLog(logFile, "aur-curator: validpgpkeys: "+strings.Join(keys, ", "))
	}

	// 4. The build.
	stderr.Reset()
	trace.Log(ctx, "handoff", "to", "subprocess", "from", "builder", "cmd", "makepkg", "backend", Backend, "env", strings.Join(names, ","))
	if err := b.run(ctx, policy, logFile, io.MultiWriter(logFile, &stderr), "makepkg", "--noconfirm", "--nocolor"); err != nil {
		if ctx.Err() != nil {
			return BuildResult{}, fmt.Errorf("makepkg cancelled: %w", ctx.Err())
		}
		return BuildResult{}, fmt.Errorf("makepkg failed: %w: %s", err, sanitize.ForMessage(stderr.String()))
	}

	artifactPaths, err := findArtifacts(workspaceDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("discover artifacts: %w", err)
	}
	return BuildResult{LogLocation: logPath, ArtifactPaths: artifactPaths}, nil
}

// run runs name with args in a sandbox built from p, in its own
// process group so cancel ends it.
func (b MakepkgBuilder) run(ctx context.Context, p sandbox.Policy, stdout, stderr io.Writer, name string, args ...string) error {
	cmd, err := sandbox.Command(ctx, p, name, args...)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return procgroup.Run(ctx, cmd, "to", "subprocess", "from", "builder", "cmd", name)
}

// policy is the sandbox for a build in workspace with env: network on,
// workspace writable. The --printsrcinfo run narrows it.
func (b MakepkgBuilder) policy(workspace string, env []string) sandbox.Policy {
	p := sandbox.Policy{
		Workspace:         workspace,
		WorkspaceWritable: true,
		Home:              b.Paths.Home,
		Masks: []sandbox.Mask{
			{Path: b.Paths.ConfigFile},
			{Path: b.Paths.ActivityLog},
			{Path: b.Paths.RepositoryPath, Dir: true},
		},
		Helper:  b.Paths.Helper,
		Env:     env,
		Network: true,
	}
	if conf, dest := userMakepkgConf(os.Getenv("XDG_CONFIG_HOME"), b.Paths.Home); conf != "" {
		p.ReadOnly = append(p.ReadOnly, sandbox.Bind{Src: conf, Dest: dest})
	}
	if target := resolvConfInRun("/etc/resolv.conf"); target != "" {
		p.ReadOnly = append(p.ReadOnly, sandbox.Bind{Src: target, Dest: target})
	}
	return p
}

// targets are what the self-test proves hidden.
func (b MakepkgBuilder) targets() sandbox.Targets {
	return sandbox.Targets{
		RootPath:       b.Paths.RootPath,
		RepositoryPath: b.Paths.RepositoryPath,
		Hidden:         []string{b.Paths.ConfigFile, b.Paths.ActivityLog},
	}
}

// userMakepkgConf returns the user's makepkg configuration file, if
// any, and where makepkg looks for it inside the sandbox. makepkg reads
// the first readable of ${XDG_CONFIG_HOME:-$HOME/.config}/pacman/
// makepkg.conf and $HOME/.makepkg.conf; inside, XDG_CONFIG_HOME is not
// set, so the first lands at $HOME/.config/pacman/makepkg.conf.
func userMakepkgConf(xdgConfigHome, home string) (src, dest string) {
	if xdgConfigHome == "" {
		xdgConfigHome = filepath.Join(home, ".config")
	}
	if p := filepath.Join(xdgConfigHome, "pacman", "makepkg.conf"); readable(p) {
		return p, filepath.Join(home, ".config", "pacman", "makepkg.conf")
	}
	if p := filepath.Join(home, ".makepkg.conf"); readable(p) {
		return p, p
	}
	return "", ""
}

// resolvConfInRun returns the target of path when it is a symlink into
// /run (systemd-resolved's stub), which the sandbox's empty /run would
// otherwise leave dangling.
func resolvConfInRun(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil || target == path || !strings.HasPrefix(target, "/run/") {
		return ""
	}
	return target
}

func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// writeLog appends one line to the build log. A failed write is not
// worth failing the build over; makepkg's own output would fail the
// same way and be reported.
func writeLog(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

// errNoHelper is returned when the aur-curator binary cannot be found
// to start the sandbox.
var errNoHelper = errors.New("the aur-curator binary could not be located, so the build sandbox cannot start")

// errNoHome is returned when the home directory is unknown: the sandbox
// puts an empty tmpfs there.
var errNoHome = errors.New("the home directory is unknown, so the build sandbox cannot start")

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

// CheckSandbox runs the self-test a build runs first, with the same
// paths and environment, in a throwaway workspace under dir (check 4,
// workflow.md § Check (Consistency)). The workspace is removed
// afterwards. The error names every failing check.
func CheckSandbox(ctx context.Context, paths SandboxPaths, dir string) (sandbox.Report, error) {
	b := NewMakepkgBuilder(paths)
	if paths.Helper == "" {
		return sandbox.Report{}, errNoHelper
	}
	if paths.Home == "" {
		return sandbox.Report{}, errNoHome
	}
	tmp, err := os.MkdirTemp(dir, "sandbox-check-")
	if err != nil {
		return sandbox.Report{}, fmt.Errorf("create the self-test workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	ws := filepath.Join(tmp, "work", "src")
	if err := os.MkdirAll(ws, 0o750); err != nil {
		return sandbox.Report{}, fmt.Errorf("create the self-test workspace: %w", err)
	}
	env, _ := buildEnv(os.Environ(), paths.Home, ws)
	return sandbox.SelfTest(ctx, b.policy(ws, env), b.targets())
}
