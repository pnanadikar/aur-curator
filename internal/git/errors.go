package git

import "errors"

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
