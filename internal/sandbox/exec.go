package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// Exec is sandbox-exec, the first thing that runs inside the sandbox.
// args are "--", the command and its arguments. It joins a new, empty
// session keyring, then replaces itself with the command. It returns
// only on failure.
//
// The kernel's session keyring is shared into a user namespace (the
// plan's finding 7), so without the join a build could read keys the
// user's session holds.
func Exec(args []string) error {
	if len(args) < 2 || args[0] != "--" {
		return errors.New("usage: sandbox-exec -- <command> [args...]")
	}
	name, argv := args[1], args[1:]

	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("find %s: %w", name, err)
	}

	// Credentials, the session keyring among them, belong to a thread.
	// The join and the exec must happen on the same one, or the
	// command starts with the old keyring.
	runtime.LockOSThread()
	if err := joinNewSessionKeyring(); err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ()) //nolint:gosec // the command the sandbox was started for; bwrap already confines it
}

// joinNewSessionKeyring makes the calling thread join a new anonymous
// session keyring: KEYCTL_JOIN_SESSION_KEYRING with a NULL name.
func joinNewSessionKeyring() error {
	if _, err := unix.KeyctlInt(unix.KEYCTL_JOIN_SESSION_KEYRING, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("join a new session keyring: %w", err)
	}
	return nil
}
