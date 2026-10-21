package instancelock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".aur-curator", "tui.lock") // dir created on demand

	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	// flock locks per open file description, so a second Acquire in the
	// same process conflicts exactly like another process would.
	_, err = Acquire(path)
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("second Acquire: got %v, want ErrHeld", err)
	}
	if !strings.Contains(err.Error(), "pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("error should name the holder's pid: %v", err)
	}

	if err := release(); err != nil {
		t.Fatal(err)
	}
	release2, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	_ = release2()
}

// TestLockReleasedWhenHolderDies: a holder that exits without releasing
// (as in a crash) must not leave the lock held.
func TestLockReleasedWhenHolderDies(t *testing.T) {
	if os.Getenv("INSTANCELOCK_HOLDER") != "" {
		if _, err := Acquire(os.Getenv("INSTANCELOCK_HOLDER")); err != nil {
			os.Exit(3)
		}
		os.Exit(0) // exit without calling release
	}
	path := filepath.Join(t.TempDir(), "tui.lock")
	cmd := exec.Command(os.Args[0], "-test.run=TestLockReleasedWhenHolderDies")
	cmd.Env = append(os.Environ(), "INSTANCELOCK_HOLDER="+path)
	if err := cmd.Run(); err != nil {
		t.Fatalf("holder process: %v", err)
	}
	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("lock still held after its holder exited: %v", err)
	}
	_ = release()
}
