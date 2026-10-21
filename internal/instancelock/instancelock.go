// Package instancelock keeps a single aur-curator TUI per workspace.
//
// The lock is an exclusive, non-blocking flock(2) on a file under the
// workspace's .aur-curator directory. The kernel releases a flock when its
// process exits for any reason — quit, crash, kill -9, power loss — so a
// crashed instance can never leave a stale lock behind. That property is
// what makes startup recovery of interrupted builds safe (#311): while this
// lock is held, a build record still marked Running cannot belong to a
// live process.
//
// flock is Unix-only; aur-curator targets Arch Linux.
package instancelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrHeld reports that another process holds the lock.
var ErrHeld = errors.New("another aur-curator is running on this workspace")

// Acquire takes the lock at path, creating the file and its directory if
// needed, and records this process's PID in it for diagnostics. It never
// blocks: if the lock is held it returns an error wrapping ErrHeld, naming
// the holder's PID when known. The returned release function drops the
// lock; it is safe to skip, since the kernel releases the lock on exit.
func Acquire(path string) (release func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := readPID(f)
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			if holder != "" {
				return nil, fmt.Errorf("%w (pid %s; lock %s)", ErrHeld, holder, path)
			}
			return nil, fmt.Errorf("%w (lock %s)", ErrHeld, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	// Best-effort: the PID is only for the message another instance shows.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return f.Close, nil
}

func readPID(f *os.File) string {
	buf := make([]byte, 32)
	n, _ := f.ReadAt(buf, 0)
	return strings.TrimSpace(string(buf[:n]))
}
