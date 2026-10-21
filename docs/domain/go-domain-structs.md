# Go Domain Structs

## Purpose

This document defines the Go domain model corresponding to `domain.md`.

## Principles

- One Go struct per domain entity.
- Map **only Stored** attributes.
- Do **not** include derived/computed values.
- No persistence logic.
- No service logic.
- No UI concerns.
- Relationships are represented by strongly typed identifiers rather
    than object pointers where practical.
- SQLite is an implementation detail and must not appear in this
    layer.
- Decision is not a separate entity — `Review.Status`, `Review.DecisionTime`,
    and `Review.Rationale` carry the trust decision directly. See `domain.md`
    § Review, Open Questions.

## Package Layout

``` text
internal/domain/
    ids.go
    package.go
    review.go
    finding.go
    risk_assessment.go
    policy_config.go
    build.go
    artifact.go
    package_repository.go
    workspace.go
```

## Common IDs

``` go
// PackageName is the package's natural key — the package's actual name
// (e.g. "yay"). There is no separate surrogate PackageID; Package.Name
// is the sole identifier used everywhere a package is referenced.
// See domain-package-review.md DOM-001/DOM-002.
type PackageName string
type ReviewID string
type FindingID string
type RiskAssessmentID string
type BuildID string
type ArtifactID string
type WorkspaceID string
type GitRepositoryID string
type PackageRepositoryID string
type PolicyConfigID string
```

## Structs

### Package

``` go
type Package struct {
    Name            PackageName
    GitRepositoryID GitRepositoryID
    CurrentReviewID *ReviewID
    LatestBuildID   *BuildID
}
```

### Review

Captures a human review of upstream changes for one package. Only
irreducible information is stored; everything derivable from Git (diffs,
commit history, merge status) is excluded by design. `BaseCommit` and
`UpstreamCommit` delimit the reviewed range; the diff itself is derived
from them on demand. `BaseCommit` is stored because Merge moves `master`
past it, after which it can no longer be derived (domain.md § Reviewed
Range, #387). There is no separate
`Decision` struct — `Status`, `DecisionTime`, and `Rationale` below carry
the trust decision directly.

RiskAssessment is nested here rather than tracked by a separate store, since
Review is the only entity ReviewStore persists — it stays nil through
FindingsStatus transitions and is set once, at decision time
(see domain.md § RiskAssessment, "Ownership").

``` go
type Review struct {
    ID             ReviewID        `json:"id"`
    PackageName    PackageName     `json:"packageName"`
    UpstreamCommit string          `json:"upstreamCommit"`
    BaseCommit     string          `json:"baseCommit"` // start of the reviewed range; "" on a first review (Import) — see domain.md § Reviewed Range
    Status         ReviewStatus    `json:"status"` // Pending, Approved, Rejected
    DecisionTime   time.Time       `json:"decisionTime"`
    ReviewerNotes  string          `json:"reviewerNotes"`
    Rationale      string          `json:"rationale"` // optional, captured on Approve-without-analysis or Reject
    Findings       []Finding       `json:"findings"` // may be nil/empty — see FindingsStatus
    FindingsStatus FindingsStatus  `json:"findingsStatus"`
    RiskAssessment *RiskAssessment `json:"riskAssessment,omitempty"` // nil until RecordDecision computes it — see domain.md § RiskAssessment
}

type ReviewStatus string

const (
    ReviewPending  ReviewStatus = "pending"
    ReviewApproved ReviewStatus = "approved"
    ReviewRejected ReviewStatus = "rejected"
)

// FindingsStatus distinguishes "analyzers never ran" from "ran, zero
// findings" — these must never collapse into the same state in storage
// or in the UI.
type FindingsStatus string

const (
    FindingsNotRun FindingsStatus = "not_run"
    FindingsRun    FindingsStatus = "run"
)
```

#### ReviewStore

``` go
// ReviewStore persists Review records. Implementations are responsible
// only for storage; they must never derive or validate Git state, and
// must never compute status — that is Review Engine's job.
type ReviewStore interface {
    // Get returns the stored Review for a package, or ErrNotFound if
    // no review has ever been recorded for it.
    Get(name PackageName) (*Review, error)

    // Save upserts the Review for a package.
    Save(name PackageName, review *Review) error

    // Delete removes any stored Review for a package. Used rarely —
    // e.g. if a package is removed from the curated collection.
    Delete(name PackageName) error
}

var ErrNotFound = errors.New("review: not found")
```

`Review` is single, current-state per package — not historical.
`ReviewStore.Get(name)` correctly returns one `*Review`. `Package`
references it via `CurrentReviewID` only; there is no `Reviews []Review`
collection. When a new upstream commit is fetched, a superseded Review
(Pending or terminal) is overwritten by a new Pending review for the new
comparison — reviewer notes, findings, and rationale from the prior review
are not retained. See `domain.md` § Review, "Review Scope: Current, Not
Historical."

### Finding

``` go
type Finding struct {
    ID        FindingID
    ReviewID  ReviewID
    Analyzer  string
    Category  string
    Summary   string
    Details   json.RawMessage
    Location  string
}
```

### PolicyConfig

Stores only the user's overrides to the default category → severity
mapping. The default mapping itself (`risk-analysis.md`) is versioned
with the application, not persisted here — only deviations from it are
irreducible state. Named `PolicyConfig` rather than `PolicyEngine` to
avoid colliding with the Policy Engine *interface* (the service that
applies this config to classify Findings — see `interfaces.md`); this
struct is its persisted configuration, not the service itself.

``` go
type PolicyConfig struct {
    ID          PolicyConfigID
    WorkspaceID WorkspaceID
    Overrides   json.RawMessage // category -> severity override map
    Version     string          // referenced by RiskAssessment.PolicyVersion
    UpdatedAt   time.Time
}
```

### Severity

Shared enum used by `RiskAssessment.HighestSeverity` and
`RiskAssessment.CountBySeverity`. Values match the three tiers defined in
`risk-analysis.md`. `Finding` itself never stores a `Severity` — it is
computed on read by Policy Engine from `Finding.Category` (see `domain.md`
§ Finding) and only surfaces as a stored value here, frozen into a
`RiskAssessment` snapshot.

``` go
type Severity string

const (
    SeverityLow    Severity = "low"
    SeverityMedium Severity = "medium"
    SeverityHigh   Severity = "high"
)
```

### RiskAssessment

Policy Engine's aggregate rollup over a Review's findings — one per
Review, not one per Finding. Computed once and frozen as a decision-time
audit snapshot: it records what the reviewer saw and which policy
version produced it, so it stays accurate even if `PolicyConfig`
changes later.

``` go
type RiskAssessment struct {
    ID              RiskAssessmentID
    ReviewID        ReviewID
    HighestSeverity Severity
    CountBySeverity map[Severity]int
    PolicyVersion   string
    Rationale       string
}
```

### Build

`Build.Status` uses the `BuildStatus` enum below, matching the lifecycle
stated in `domain.md` § Build ("Pending → Running → Success or Failed").

``` go
type BuildStatus string

const (
    BuildPending BuildStatus = "pending"
    BuildRunning BuildStatus = "running"
    BuildSuccess BuildStatus = "success"
    BuildFailed  BuildStatus = "failed"
)
```

A package has two build records — the **latest attempt** and the **last
success** — both of type `Build` (`domain.md` § Build → Latest Attempt and
Last Success). The same struct serves both; the fields marked below are
meaningful only on one of them.

``` go
type Build struct {
    ID          BuildID
    PackageName PackageName
    Backend     string
    Status      BuildStatus
    StartTime   time.Time
    EndTime     *time.Time
    LogLocation string
    ArtifactIDs []ArtifactID

    // ReviewedCommit is the local master HEAD commit that was built.
    // Built/Confirmed/Published count only while it equals HEAD
    // (domain.md § Package → Lifecycle).
    ReviewedCommit string

    // Confirmed records the user's explicit acknowledgement gating
    // Confirmed -> Published. It performs no independent verification of
    // build output — a stored workflow gate, not a computed check.
    // Per-Build, not per-Artifact: one confirmation covers the whole
    // artifact set. Meaningful only on the last-success record; a new
    // successful build replaces the last success with Confirmed = false.
    Confirmed   bool
    ConfirmedAt *time.Time // nil until Confirmed is set true

    // Published is set when Publish succeeds. Meaningful only on the
    // last-success record. Stored for now; deriving it from the package
    // repository (checksum match) is a follow-up (#317) — see domain.md
    // § Artifact.
    Published   bool
    PublishedAt *time.Time // nil until Published is set true

    // LastSuccessBuildID / LastFailureBuildID point at the package's last
    // successful and last failed builds (their directories under
    // .aur-curator/builds/). Every record the engine saves carries both
    // (#308), so an interrupted build never loses them.
    LastSuccessBuildID BuildID
    LastFailureBuildID BuildID
}
```

#### BuildStore

`BuildStore` persists both records. `Get`/`Save` address the latest
attempt; `GetLastSuccess`/`SaveLastSuccess` address the last success.
Like `ReviewStore`, implementations only store — they never derive
lifecycle or validate Git state.

``` go
type BuildStore interface {
    // Get returns the package's latest build attempt, or ErrNotFound.
    Get(name PackageName) (*Build, error)

    // Save writes the package's latest build attempt.
    Save(name PackageName, build *Build) error

    // GetLastSuccess returns the package's last successful build, or
    // ErrNotFound if it has none. (#309)
    GetLastSuccess(name PackageName) (*Build, error)

    // SaveLastSuccess writes the package's last successful build.
    // Confirm and Publish update only this record. (#309)
    SaveLastSuccess(name PackageName, build *Build) error

    // ReadBuildLog returns the contents of a build's log.
    ReadBuildLog(buildID BuildID, logPath string) (string, error)

    // Delete removes the latest-attempt record only (used when a
    // cancelled first build leaves nothing to restore). The last success
    // is unaffected; Remove deletes the whole package store directory.
    Delete(name PackageName) error
}
```

> `BuildStore` lives in package `domain` today (placement question
> STO-001). The JSON implementation stores the two records as `build.json`
> and `last-success.json` in the package's store directory.

### Artifact

`PackageName` is a deliberate exception to "map only Stored attributes /
relationships via typed IDs" — it duplicates what's reachable via
`BuildID → Build.PackageName`. Kept because artifact retrieval is
queried directly by package (Package Details' artifact history uses `GetArtifactsByPackage`), and joining through `BuildStore` for every such lookup isn't worth the purity cost. Same category of exception as `RiskAssessment`. Decided 2026-09-08.

``` go
type Artifact struct {
    ID           ArtifactID
    BuildID      BuildID
    PackageName  PackageName // deliberate denormalization, see note above
    Filename     string
    Version      string
    Architecture string
    Checksum     string
    Size         int64
}
```

### GitRepository

**Retired — unimplemented by design, not a gap.** Every field this struct would hold is either a fixed convention (`origin`/`upstream` remote names) or a one-line derivation from `Workspace.RootPath + Package.Name`. No consumer needs these bundled as a stored or even derived unit. Same category of non-adoption as the removed `Workspace.PackageIDs` (DOM-004). If a real need appears later, it should be a `GitProvider` read accessor (derived on call), never a persisted struct. `GitRepositoryID` remains in `ids.go` only because `Package.GitRepositoryID` references it; treat that field as vestigial too.

### PackageRepository

Deliberately has no `ID` field — exactly one `PackageRepository` per workspace, constructed once from config, same pattern as `WorkspaceConfig`. No store, no service interface, no independent identity.

``` go
type PackageRepository struct {
    Location      string
    SigningStatus string // reserved: package signing not yet scoped, see roadmap.md Phase 3
}
```

### WorkspaceConfig

Loaded once at startup from a plain function (e.g.
`config.Load(path string) (WorkspaceConfig, error)`, package `internal/config`)
and passed as a constructor argument when building `Workspace` — no
service interface, no `ConfigurationProvider`, mirroring how `PolicyConfig`
is passed directly into Policy Engine's constructor. See `domain.md` §
WorkspaceConfig, Design Note.

Deliberately has no `ID` field and no back-reference to `Workspace` —
unlike `PolicyConfig`'s `WorkspaceID`, which in retrospect assumes
multi-workspace support the application never needed (see `domain.md` §
PolicyConfig note). This is a plain embedded value, not an independently
stored/queryable entity.

`GiteaToken` uses `Secret` rather than `string` — see `domain.md` §
Secret Handling: GiteaToken for the full policy (env-var precedence over
file, permission warning, no re-persistence). `Secret`'s redacted
`String()`/`GoString()` exist specifically so an accidental `%+v` of
`WorkspaceConfig` (including inside a wrapped error) can't leak the raw
token — deliberate access is always through `Secret.Value()`.

`config.Load` decodes the file directly into `WorkspaceConfig` via its
`json` tags — there is no intermediate struct to keep in sync. A new
field needs only a tag to be loaded; `internal/config`'s
`TestLoad392_EveryFieldLoads` fails if a field has no tag or no test
fixture entry. Unknown keys in the file are ignored. (#392: `AutoMerge`,
and earlier `AutoAnalyzeAfterImport` (#146), were silently never loaded
because a separate hand-copied file struct lacked them.)

``` go
// Secret must never be logged, printed, or included in error messages.
// String()/GoString() are redacted so fmt verbs (the most common
// accidental-leak path) can't expose the underlying value, and
// MarshalJSON is redacted because encoding/json ignores String().
// There is deliberately no UnmarshalJSON: decoding a JSON string into
// Secret works natively, which is how config.Load reads giteaToken.
type Secret string

func (s Secret) String() string   { return "REDACTED" }
func (s Secret) GoString() string { return "REDACTED" }
func (s Secret) Value() string    { return string(s) }

// MarshalJSON redacts the secret so encoding/json can never write the
// raw value.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"REDACTED"`), nil }
```

``` go
type WorkspaceConfig struct {
    // RootPath is the workspace root — the directory containing one
    // subdirectory per curated package (architecture.md § Repository
    // Layout). Mandatory — config.Load errors if absent or empty.
    RootPath string `json:"rootPath"`

    // RepositoryPath is the local pacman repository's filesystem path
    // (PackageRepository.Location's source). Mandatory — config.Load
    // errors if absent or empty.
    RepositoryPath string `json:"repositoryPath"`

    // BuilderBackend selects the Builder implementation, e.g. "makepkg".
    // Optional — defaults to "makepkg" if absent.
    BuilderBackend string `json:"builderBackend"`

    // RepoDBName is the filename of the local pacman repository database
    // archive used by repo-add, inside RepositoryPath. The part before
    // ".db" is the repository name used in pacman.conf. Optional —
    // defaults to "custom.db.tar.gz" if absent.
    RepoDBName string `json:"repoDBName"`

    // AnalyzerConfig is opaque; shape owned by Analyzer Manager, not yet
    // designed. Optional placeholder — genuinely absent is valid,
    // config.Load does not error. Not a final decision to keep this
    // untyped forever.
    AnalyzerConfig json.RawMessage `json:"analyzerConfig"`

    // UIPreferences is opaque; shape owned by the UI layer, not yet
    // designed. Optional placeholder — same status as AnalyzerConfig.
    UIPreferences json.RawMessage `json:"uiPreferences"`

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

    // GiteaBaseURL, GiteaOrg are used by Import. Mandatory —
    // config.Load errors if either is absent or empty. Decided
    // 2026-08-09, resolving workspace-config-spec.md § 3's previously
    // open question.
    GiteaBaseURL string `json:"giteaBaseURL"`
    GiteaOrg     string `json:"giteaOrg"`

    // GiteaToken is mandatory in a precedence-aware sense: config.Load
    // errors only if BOTH the AUR_CURATOR_GITEA_TOKEN environment
    // variable AND this file's giteaToken key are absent — not simply
    // "the file key must be set." This preserves the env-var precedence
    // in domain.md § Secret Handling: GiteaToken (env var always wins if
    // set) while still guaranteeing a resolved value exists by the time
    // Load returns, unlike GiteaBaseURL/GiteaOrg which have no
    // alternate source and are checked against the file alone.
    GiteaToken Secret `json:"giteaToken"` // see domain.md § Secret Handling: GiteaToken

    // ActivityLogPath is the filesystem path for the always-on,
    // append-only activity log — one line per lifecycle event
    // (import/approve/reject/merge/build/confirm/publish), success and
    // failure both recorded. Distinct from AnalyzerConfig/UIPreferences:
    // this is a defaulted string field (pattern matches BuilderBackend/
    // RepoDBName), not an opaque placeholder. Optional — defaults to
    // "<RootPath>/.aur-curator/activity.log" if absent.
    //
    // This field is separate from the two existing logging mechanisms:
    // internal/errorlog (always-on, failure-only, machine-parseable) and
    // internal/trace (opt-in, developer diagnostic). The activity log is
    // always-on, success and failure, human-readable, pacman-log-style.
    ActivityLogPath string `json:"activityLogPath"`
}
```

See `docs/domain/workspace-config-spec.md` for the full mandatory/
optional field table and `config.Load` behavior spec (DOM-003, DOM-006,
DOM-007 in `domain-package-review.md`).

### Workspace

`RootPath` is not a `Workspace` field — it lives in the embedded
`WorkspaceConfig` (see above, DOM-003). Because `Workspace` embeds
`WorkspaceConfig` by value, `workspace.Config.RootPath` still fulfills
any caller's expectation of `Workspace` owning/returning its root path.

`PackageIDs` has been removed (DOM-004). The curated package list is
always derivable from the filesystem — including immediately after
Import, since Import's clone step lands the package's directory before
Package Catalog registration ever runs (see `workflow.md` § Import
Mechanics). `Workspace` does not duplicate what Package Catalog
(`interfaces.md`) already discovers via a live filesystem scan.

``` go
type Workspace struct {
    ID                  WorkspaceID
    PackageRepositoryID PackageRepositoryID
    PolicyConfigID      PolicyConfigID
    Config              WorkspaceConfig
}
```

## Notes

- Derived values (lifecycle state, installed version, upstream
    version, published state, Finding.Severity, etc.) are intentionally
    absent — computed on read by the Service layer, not stored here.
- `Review.Status` is a stored/persisted attribute, not derived — it is
    set once on Approve/Reject and immutable thereafter. See `domain.md`
    § Review, Open Questions ("Is Review.status derived or persisted?").
- RiskAssessment is the one apparent exception among otherwise-derived
    values: its fields look derived but are actually stored, because
    it's a decision-time audit snapshot rather than a live-computed
    value — see `domain.md` § RiskAssessment.
- The Service layer combines persisted domain objects with Git, ALPM,
    Publisher and other providers to produce UI-facing view models.
- Persistence (SQLite) is accessed only through Store interfaces; the
    domain layer is storage-agnostic.
- `WorkspaceConfig` is the one of the two exceptions to "Persistence is 
    accessed only through Store interfaces": it has no `ID`, no corresponding 
    Store, and no service interface — it's loaded once by a plain function 
    and held in memory for the process lifetime. See `domain.md` § WorkspaceConfig,
    Design Note. (The other exception is Artifact: See `Artifact`§ Artifact)
