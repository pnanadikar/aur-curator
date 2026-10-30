package domain

// Secret must never be logged, printed, or included in error messages.
// String()/GoString() are redacted so fmt verbs (the most common
// accidental-leak path) can't expose the underlying value.
type Secret string

func (s Secret) String() string   { return "REDACTED" }
func (s Secret) GoString() string { return "REDACTED" }
func (s Secret) Value() string    { return string(s) }

// MarshalJSON redacts the secret so encoding/json can never write the
// raw value (encoding/json ignores String()). There is deliberately no
// UnmarshalJSON: decoding a JSON string into Secret works natively.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"REDACTED"`), nil }

// WorkspaceConfig holds the application's workspace-scoped settings,
// loaded once at startup from a single JSONC config file and held for
// the process lifetime. There is exactly one config source, loaded by
// a plain function (internal/config.Load) — no service interface.
type WorkspaceConfig struct {
	// RootPath is the workspace root — the directory containing one
	// subdirectory per curated package. Mandatory.
	RootPath string `json:"rootPath"`

	// RepositoryPath is the local pacman repository's filesystem path.
	// Mandatory.
	RepositoryPath string `json:"repositoryPath"`

	// BuilderBackend selects the Builder implementation. Optional;
	// defaults to "makepkg", the only value config.Load accepts (#408).
	BuilderBackend string `json:"builderBackend"`

	// RepoDBName is the filename of the local pacman repository database
	// archive used by repo-add. Optional — defaults to "custom.db.tar.gz"
	// if absent.
	RepoDBName string `json:"repoDBName"`

	// AutoMerge, when true, triggers git.Provider.Merge automatically
	// as a follow-on to a successful Approve decision — no confirmation
	// prompt. When false (the default), the review stays Approved and
	// the user must press M Merge manually. Optional; defaults to false.
	AutoMerge bool `json:"autoMerge"`

	// AutoAnalyzeAfterImport, when true, automatically runs analyzers
	// immediately after a successful Import completes. Import's clone
	// already establishes upstream/master, so no separate Fetch is needed.
	// When false (the default), the user must trigger Analyze manually.
	// A failed auto-analyze does not undo Import — it is reported but
	// does not cause Import to fail. Optional; defaults to false.
	AutoAnalyzeAfterImport bool `json:"autoAnalyzeAfterImport"`

	// GiteaBaseURL, GiteaOrg are used by Import. Mandatory.
	GiteaBaseURL string `json:"giteaBaseURL"`
	GiteaOrg     string `json:"giteaOrg"`

	// GiteaToken is mandatory in a precedence-aware sense — see Load's
	// spec below. Env var always wins if set.
	GiteaToken Secret `json:"giteaToken"`

	// ActivityLogPath is the filesystem path for the always-on,
	// append-only activity log — one line per lifecycle event
	// (import/approve/reject/merge/build/confirm/publish), success and
	// failure both recorded. A defaulted string field, like
	// BuilderBackend and RepoDBName. Optional — defaults to
	// ".aur-curator/activity.log" relative to RootPath if absent.
	//
	// This field is separate from the two existing logging mechanisms:
	// internal/errorlog (always-on, failure-only, machine-parseable) and
	// internal/trace (opt-in, developer diagnostic). The activity log is
	// always-on, success and failure, human-readable, pacman-log-style.
	ActivityLogPath string `json:"activityLogPath"`
}

// Workspace represents the top-level collection of packages managed
// by aur-curator under a single directory. PackageIDs is intentionally
// absent — the curated package list is always derived live from the
// filesystem (see catalog.PackageCatalog), never duplicated here.
type Workspace struct {
	ID                  WorkspaceID
	PackageRepositoryID PackageRepositoryID
	PolicyConfigID      PolicyConfigID
	Config              WorkspaceConfig
}
