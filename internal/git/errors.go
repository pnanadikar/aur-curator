package git

import (
	"errors"
	"os/exec"
)

// CannotStart reports whether err says git itself could not be started:
// not found on PATH, or not executable. Unlike git exiting with an error,
// this fails every git call, not one repository's (#513).
func CannotStart(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

// Sentinel errors returned by Import and Remove.
var (
	ErrCreateRepository  = errors.New("create repository")
	ErrCloneRepository   = errors.New("clone repository")
	ErrConfigureRemotes  = errors.New("configure remotes")
	ErrCreateMaster      = errors.New("create orphan master")
	ErrResolveRepository = errors.New("resolve repository path")
	ErrPackageNotFound   = errors.New("package not found on AUR")
	ErrDeleteRepository  = errors.New("delete repository")
)
