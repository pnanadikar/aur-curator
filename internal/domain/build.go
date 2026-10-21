package domain

import "time"

// BuildStatus represents the current state of a build.
type BuildStatus string

const (
	// BuildPending indicates the build has been queued but not yet started.
	BuildPending BuildStatus = "pending"
	// BuildRunning indicates the build is actively executing.
	BuildRunning BuildStatus = "running"
	// BuildSuccess indicates the build completed successfully.
	BuildSuccess BuildStatus = "success"
	// BuildFailed indicates the build failed.
	BuildFailed BuildStatus = "failed"
)

// Build represents one package build attempt.
type Build struct {
	ID                 BuildID
	PackageName        PackageName
	Backend            string
	Status             BuildStatus
	StartTime          time.Time
	EndTime            *time.Time
	LogLocation        string
	ArtifactIDs        []ArtifactID
	ReviewedCommit     string
	Confirmed          bool
	ConfirmedAt        *time.Time
	Published          bool
	PublishedAt        *time.Time
	LastSuccessBuildID BuildID
	LastFailureBuildID BuildID
}

// BuildStore persists build records.
type BuildStore interface {
	Get(name PackageName) (*Build, error)
	Save(name PackageName, build *Build) error
	GetLastSuccess(name PackageName) (*Build, error)
	SaveLastSuccess(name PackageName, build *Build) error
	ReadBuildLog(buildID BuildID, logPath string) (string, error)
	// Delete removes the latest attempt record for the package. It does NOT
	// remove the last-success record; that file is preserved across purges.
	Delete(name PackageName) error
}
