// Package sandbox runs commands inside a bubblewrap (bwrap) sandbox. It
// is the only package that knows bwrap (#496).
//
// The rules the sandbox enforces are domain.md § Build → Build
// Environment; what they defend against is threat-model.md § The
// Sandbox. In short: the root is empty and only the system trees are
// added, read-only; $HOME, /tmp, /var/tmp and /run are empty tmpfs
// mounts; the workspace is the only writable host path; the
// environment is exactly the policy's; the command runs in its own
// namespaces with no nested user namespaces and dies with its parent.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Bwrap is the bubblewrap binary, looked up on PATH.
const Bwrap = "bwrap"

// The helper's hidden first arguments (cmd/aur-curator dispatches them
// before loading any config).
const (
	// ExecArg joins a new, empty session keyring and execs the command
	// that follows "--", from inside the sandbox.
	ExecArg = "sandbox-exec"
	// ProbeArg runs the in-sandbox half of the self-test.
	ProbeArg = "sandbox-probe"
)

// systemTrees are the host trees a build sees, read-only. Everything
// not listed here, in symlinks or in a Policy is absent: the
// filesystem is an allowlist (domain.md § Build Environment).
var systemTrees = []string{"/usr", "/etc", "/opt", "/sys", "/var/lib/pacman"}

// symlinks recreate Arch's merged /usr: link name → target.
var symlinks = [][2]string{
	{"/bin", "usr/bin"},
	{"/lib", "usr/lib"},
	{"/lib64", "usr/lib"},
	{"/sbin", "usr/bin"},
}

// scratch are the empty tmpfs mounts every sandbox gets, besides Home.
var scratch = []string{"/tmp", "/var/tmp", "/run"}

// Bind is a host path made visible inside the sandbox, read-only, at
// Dest.
type Bind struct {
	Src  string
	Dest string
}

// Mask is a path that must not be readable inside the sandbox. It
// matters only when the path lies in a system tree, which is otherwise
// visible; anywhere else it is absent already. Dir says whether it is
// a directory (covered by an empty tmpfs) or a file (covered by
// /dev/null).
type Mask struct {
	Path string
	Dir  bool
}

// Policy is everything that differs between two sandboxes. It is a
// plain value; Argv turns it into bwrap's arguments.
type Policy struct {
	// Workspace is the build's directory, at the same path inside.
	Workspace string
	// WorkspaceWritable is false for runs that only read the
	// workspace (makepkg --printsrcinfo).
	WorkspaceWritable bool
	// Home is the user's home path. Inside it is an empty tmpfs.
	Home string
	// Masks cover paths in system trees that must stay unreadable:
	// the config file, activityLogPath, repositoryPath.
	Masks []Mask
	// ReadOnly are extra read-only binds: the makepkg configuration
	// file, the keybox, a resolv.conf target under /run.
	ReadOnly []Bind
	// Helper is the aur-curator binary on the host. It is bound
	// read-only at the same path and starts the command inside.
	Helper string
	// Env is the whole environment inside, as NAME=value entries, in
	// order. Nothing else is passed.
	Env []string
	// Network shares the host's network namespace. Off, the sandbox
	// has none.
	Network bool
}

// Validate reports the first problem that would make Argv's result
// wrong: a relative or unclean path, an empty one, or a malformed
// environment entry.
func (p Policy) Validate() error {
	var errs []error
	check := func(what, path string) {
		switch {
		case path == "":
			errs = append(errs, fmt.Errorf("%s is empty", what))
		case !filepath.IsAbs(path):
			errs = append(errs, fmt.Errorf("%s %q is not absolute", what, path))
		case filepath.Clean(path) != path:
			errs = append(errs, fmt.Errorf("%s %q is not clean", what, path))
		case path == "/":
			errs = append(errs, fmt.Errorf("%s is /", what))
		}
	}
	check("workspace", p.Workspace)
	check("home", p.Home)
	check("helper", p.Helper)
	for _, m := range p.Masks {
		check("mask", m.Path)
	}
	for _, b := range p.ReadOnly {
		check("bind source", b.Src)
		check("bind destination", b.Dest)
	}
	for _, e := range p.Env {
		name, _, ok := strings.Cut(e, "=")
		if !ok || name == "" {
			errs = append(errs, fmt.Errorf("environment entry %q is not NAME=value", e))
		}
	}
	return errors.Join(errs...)
}

// Argv returns bwrap's arguments for running name with args under p,
// through the helper (ExecArg). It does not validate p (Validate) and
// touches nothing: the argument list is the whole sandbox, so tests
// pin it.
//
// Order matters to bwrap, which applies mounts as it reads them: the
// system trees, the tmpfs mounts, then masks and binds that land inside
// them, then the workspace. Namespaces, session and environment follow
// (ADR #496 § Decision).
func Argv(p Policy, name string, args ...string) []string {
	var a []string
	for _, t := range systemTrees {
		a = append(a, "--ro-bind", t, t)
	}
	for _, s := range symlinks {
		a = append(a, "--symlink", s[1], s[0])
	}
	a = append(a, "--dev", "/dev", "--proc", "/proc")
	for _, t := range scratch {
		a = append(a, "--tmpfs", t)
	}
	a = append(a, "--tmpfs", p.Home)

	for _, m := range p.Masks {
		if !inSystemTree(m.Path) {
			continue
		}
		if m.Dir {
			a = append(a, "--tmpfs", m.Path)
		} else {
			a = append(a, "--ro-bind", "/dev/null", m.Path)
		}
	}
	for _, b := range p.ReadOnly {
		a = append(a, "--ro-bind", b.Src, b.Dest)
	}
	a = append(a, "--ro-bind", p.Helper, p.Helper)

	if p.WorkspaceWritable {
		a = append(a, "--bind", p.Workspace, p.Workspace)
	} else {
		a = append(a, "--ro-bind", p.Workspace, p.Workspace)
	}
	a = append(a, "--chdir", p.Workspace)

	// --unshare-all only tries the user namespace; --unshare-user makes
	// it required, which --disable-userns needs.
	a = append(a, "--unshare-all", "--unshare-user", "--disable-userns")
	if p.Network {
		a = append(a, "--share-net")
	}
	// --new-session: no TIOCSTI into aur-curator's terminal.
	// --die-with-parent: cancel signals bwrap's process group, which
	// --new-session leaves; without it the build outlives the cancel.
	a = append(a, "--new-session", "--die-with-parent")

	a = append(a, "--clearenv")
	for _, e := range p.Env {
		k, v, _ := strings.Cut(e, "=")
		a = append(a, "--setenv", k, v)
	}

	a = append(a, "--", p.Helper, ExecArg, "--", name)
	return append(a, args...)
}

// Command returns the command that runs name with args under p. bwrap
// itself gets an empty environment: everything the sandbox sees comes
// from p.Env.
func Command(ctx context.Context, p Policy, name string, args ...string) (*exec.Cmd, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("sandbox policy: %w", err)
	}
	if name == "" {
		return nil, errors.New("sandbox: no command")
	}
	cmd := exec.CommandContext(ctx, Bwrap, Argv(p, name, args...)...) //nolint:gosec // bwrap itself; Argv builds the arguments from a validated Policy
	cmd.Env = []string{}
	return cmd, nil
}

// inSystemTree reports whether path is one of the system trees or lies
// inside one.
func inSystemTree(path string) bool {
	for _, t := range systemTrees {
		if path == t || strings.HasPrefix(path, t+"/") {
			return true
		}
	}
	return false
}
