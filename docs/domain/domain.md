# Domain Model: aur-curator

> Last updated: 2026-10-19
> Status: Active
> Companion docs: architecture.md, interfaces.md, go-domain-structs.md, workflow.md, risk-analysis.md
>
> First draft. This document captures the core domain entities and their responsibilities. The model is expected to evolve as interfaces and persistence are refined.

## Design Principles

- Git owns repository state whenever possible.
- Persist only irreducible information.
- Relationships express ownership, not implementation.
- Entities represent the problem domain rather than UI or storage concerns.

---

1. [Package](#package)
2. [Review](#review)
3. [Finding](#finding)
4. [RiskAssessment](#riskassessment)
5. [Build](#build)
6. [Artifact](#artifact)
7. [GitRepository](#gitrepository)
8. [PackageRepository](#packagerepository)
9. [PolicyConfig](policyconfig)
10. [WorkspaceConfig](#workspaceconfig)
11. [Workspace](#workspace)

## Package

### Purpose

Represents one curated AUR package managed by aur-curator.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| name | Canonical package name | Stored |
| lifecycleState | Current workflow state | Derived |
| upstreamVersion | Latest upstream version | Derived |
| originRepository | Trusted Git repository | Relationship — one facet of GitRepositoryID → GitRepository.OriginRemote |
| upstreamRepository | AUR repository | Relationship — one facet of GitRepositoryID → GitRepository.UpstreamRemote |
| currentReview | Active review (if any) | Relationship — see CurrentReviewID |
| latestBuild | Most recent build attempt, any status | Relationship — see LatestBuildID |
| lastSuccessfulBuild | Most recent successful build — the basis of Built / Confirmed / Published | Relationship — see Build § Latest Attempt and Last Success |
| publishedArtifact | Currently published package | Derived |

### Ownership

Owns the lifecycle of a single curated package.

### Lifecycle

```text
Review Pending → Approved → Merged → Built → Confirmed → Published
      └──────→ Rejected
```

Lifecycle state is **derived, never stored**. Transitions, their guards,
and their failure outcomes are defined in `workflow.md` § State Transitions
and Action Availability (the transition matrix) and are not repeated here.
This section defines what each state means and how it is derived:

1. **No review** → `Not Reviewed`.
2. **Review Pending / Rejected** → that state, from `Review.status`. The
   review's status takes precedence over everything below.
3. **Approved, reviewed commit not yet merged** into local `master` →
   `Approved`. "Merged" means `Review.upstreamCommit` is contained in local
   `master` (an ancestor of HEAD); with fast-forward-only Merge it is HEAD
   itself (`workflow.md` § Merge).
4. **Approved and merged** → derived from the package's **last successful
   build**, and only if that build is of the current local `master` HEAD
   (`Build.reviewedCommit == HEAD`; if HEAD is unknown, no build counts):
   - no such build → `Merged`
   - built, not confirmed → `Built`
   - confirmed, not published → `Confirmed`
   - confirmed and published → `Published`

`Published` is the final state. Whether a package is installed on a given
machine is pacman's concern, not lifecycle: aur-curator never installs or
uninstalls packages (`workflow.md` § Installation, #351).

Consequences:

- A build of an earlier commit never counts. After a new upstream commit is
  merged, the package derives as `Merged` until that commit itself is built,
  even though the previous version's artifacts may still be published.
- The **latest build attempt** never affects lifecycle. A running, failed,
  or cancelled build leaves the state exactly where the last success put
  it; the attempt's outcome is shown separately (build status, Failed card).
- `Confirmed` is a distinct state, not a flag within `Built`.

`Unknown` and `Fetched` are not states: Import creates a `Review` record
synchronously (`workflow.md` § Import, #156), so a package's first
observable state is `Review Pending`. There is no `Build Pending` state
either — a `domain.Build` holds `BuildPending` only briefly as an internal
initialization marker before becoming `BuildRunning`.

`Confirmed` is a workflow gate, not a content check — it applies
package-wide to the artifact(s) of the last successful build as a single
unit, requiring explicit user acknowledgement before Publish. It performs no
independent verification.

> **Implementation status (2026-10-08):** rules 1–4 are implemented
> (#312, #309).

### Relationships

- has one GitRepository
- has one current Review (see Review § Current, Not Historical)
- has many Builds
- has many Artifacts
- belongs to one Workspace

### Design Note: Package Identity (DOM-001 / DOM-002)

`name` is the package's sole identifier — there is no separate surrogate
ID. This was previously ambiguous: `go-domain-structs.md` carried both a
typed `PackageID` and a redundant `Name string` field, while this
Attributes table only ever listed `name`. Resolved: the Go type is
`PackageName` (not `PackageID`), used everywhere a package is
referenced — `Package.Name`, `Review.PackageName`, `ReviewStore`'s
parameters, `Build.PackageName`. See `domain-package-review.md`
DOM-001/DOM-002 for the full history of this inconsistency.

---

## Review

### Purpose

Captures the current human review of a specific upstream repository state. A Review brings together the review findings, the policy-derived risk assessment, and the reviewer's 
decision as a single auditable unit.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Unique identifier, deterministic from packageName@upstreamCommit — see § Review Identity | Stored |
| packageName | Reviewed package | Stored |
| upstreamCommit | Commit reviewed — the end of the reviewed range | Stored |
| baseCommit | Start of the reviewed range: the merge base of local `master` and `upstream/master` when the review was created; empty on a first review (Import), meaning "nothing reviewed before" — see § Reviewed Range | Stored |
| status | Pending / Approved / Rejected | Stored |
| decisionTime | Approval or rejection timestamp | Stored |
| reviewerNotes | Optional notes | Stored |
| rationale | Optional explanation, captured on approval-without-analysis or rejection | Stored |
| findings | Findings produced during review (may be empty; see findingsStatus) | Stored |
| findingsStatus | Not Run / Run — distinguishes "never analyzed" from "analyzed, zero findings" | Stored |
| riskAssessment | Policy Engine's aggregate risk rollup, computed once at RecordDecision — see § RiskAssessment | Stored (nil until decided) |

### Ownership

Owns review decisions and metadata that cannot be reconstructed from Git.
The trust decision itself (outcome, rationale, timestamp) is not a separate
entity — it is carried directly by `status`, `rationale`, and `decisionTime`
above.

### Lifecycle

Pending → Approved or Rejected. Once decided, status and decisionTime are
immutable.

### Review Scope: Current, Not Historical

A Review is anchored to a specific fetched upstream commit
(`upstreamCommit`), compared against local `master` at that moment — it is
an event record answering "should this specific incoming commit be
trusted?", not a live status of local state. The exact range it covers is
stored (`baseCommit` → `upstreamCommit`, § Reviewed Range), so the review
shows the same changes before and after Merge.

`ReviewStore` holds a single, current Review per package
(`Package.currentReview`) — not a history of all past reviews. When a new
upstream commit is fetched:

- If the existing stored Review is still Pending, it is replaced by the
  new comparison (the old Pending review was never decided, so nothing is
  lost).
- If the existing stored Review is terminal (Approved or Rejected), it is
  **overwritten** by a new Pending review for the new comparison. The
  superseded review's reviewerNotes, findings, and rationale are not
  retained past this point.

What survives regardless: Git history still shows which commit was
reviewed, merged, and built (see `architecture.md` § Git is an Audit
Trail) — this model only forgoes retaining the *reviewer's* notes/findings/
rationale for superseded reviews, not the fact that a review and merge
occurred. This was a deliberate simplicity tradeoff over a fully historical
Review log, made explicitly rather than left as a gap.

### Review Identity

`Review.ID` is derived deterministically from `PackageName` and
`UpstreamCommit` (e.g. `"yay@9f8e7d6"`), not a randomly generated
identifier. This follows directly from Review Scope above: because a
package has exactly one current Review, tied to a specific
`(PackageName, UpstreamCommit)` pair, that pair is already a natural key
— generating a separate random ID would only obscure the audit
relationship Git already encodes. A deterministic ID also has a useful
side effect: if review initiation is ever invoked twice for a package at
the same upstream commit, it produces the same ID rather than two IDs for
what the domain model treats as one review.

### Relationships

- belongs to one Package
- contains many Findings

### Reviewed Range

The changes a Review covers are the range `baseCommit` → `upstreamCommit`:
upstream's commits and file changes since `baseCommit`. Both ends are
stored, and everything that shows or inspects the review uses them — the
Review screen's commits, file diffs and comparison summary, and the
analyzers. None of them read the live `master...upstream/master`.

`baseCommit` is set once, when the review is created (Import, Fetch, or
the first opening of a package that has no review), and never changes:

- **Normal case:** the merge base of local `master` and `upstream/master`
  at that moment — the last upstream commit already in `master`. If
  `master` carries local commits that are not on upstream (#316), the base
  is still that upstream commit, not the local tip; local commits are
  never part of the reviewed range.
- **First review (Import):** empty. Import's placeholder `master` has no
  content and no history shared with upstream, so there is no base: the
  range is the whole of `upstream/master`'s history, and every file is an
  addition (diffed against the empty tree, `workflow.md` § Import).

**Why it is stored (#387).** Earlier this section argued that the local
side of the comparison was determined by the package and the upstream
commit, so it needed no field. That holds for identity — `(packageName,
upstreamCommit)` is still the natural key (§ Review Identity) — but not for
showing the review. Merge moves `master` to `upstreamCommit` itself
(fast-forward, or a reset on the first merge after Import), after which the
live `master...upstream/master` is empty and the start of the reviewed
range can no longer be derived from Git. The reflog was considered and
rejected: it mixes in manual moves of `master`, records the first
post-Import merge as a reset, is local to one clone, and expires. The base
is therefore irreducible state and is stored ("Store Only Irreducible
State", `architecture.md`).

**If a stored commit is missing** from the repository (possible only after
a manual history rewrite and garbage collection), the Review screen shows
an error naming the missing commit instead of a diff. It never falls back
to the live comparison, which would show something other than what was
reviewed.

Rule (2) of the old baseline invariant still matters: a new Fetch while a
Review is Pending replaces that Review (with a new base) rather than
reconciling two live comparisons (§ Review Scope: Current, Not Historical).
Rule (1) — local `master` only advances via Merge — is no longer needed for
correctness, since the base is recorded rather than recomputed. Local
`master` is an ordinary git repository and commits can reach it from
outside the app; such commits are unreviewed, and are detected and flagged
rather than blocked (`workflow.md` § Local Commits Not on Upstream, #316).

---

## Finding

### Purpose

Represents a single observation produced by an analyzer.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Identifier | Stored |
| analyzer | Producing analyzer | Stored |
| category | Checksum, URL, PKGBUILD, etc. | Stored |
| severity | Low / Medium / High | Derived — computed by Policy Engine from `category` via default/user-override mapping, not assigned by the analyzer |
| summary | Short description | Stored |
| details | Supporting evidence | Stored |
| location | Optional file/line reference | Stored |

### Ownership

Owned by a Review. Note: `severity` is not owned by Finding itself — it is computed on read by Policy Engine from `category`, so two identical categories always resolve to the same severity at a given point in time, but that resolution can change if the user's policy overrides change.

### Lifecycle

Created during analysis → Presented → Archived with Review

### Relationships

- belongs to one Review

---

## RiskAssessment

### Purpose

Represents the Policy Engine's objective assessment of a Review's findings. Computed as an aggregate rollup of the severities of the Review's Findings, it records the evaluated risk independently of the human review decision.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Identifier | Stored |
| review | Assessed review | Stored |
| highestSeverity | Highest severity among the review's findings | Stored (snapshot) |
| countBySeverity | Count of findings per severity | Stored (snapshot) |
| policyVersion | Policy Engine mapping version in effect when computed | Stored |
| rationale | Optional explanation | Stored |

### Ownership

Owned by a Review. Computed once by Policy Engine and frozen — unlike Finding.severity, which is derived live and can change if policy overrides change, RiskAssessment is a point-in-time audit snapshot: it records what the reviewer actually saw at decision time, tagged with the policy version that produced it. This mirrors "Git is an audit trail," extended to the review record itself.

### Lifecycle

Computed at review time → immutable thereafter

### Relationships

- belongs to one Review

---

## Build

### Purpose

Represent a single build execution for an approved commit. It provides the audit trail linking the reviewed source revision to the produced artifacts.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Build identifier | Stored |
| packageName | Owning package | Relationship — see PackageName |
| backend | makepkg, chroot, etc. | Stored |
| status | Pending / Running / Success / Failed | Stored |
| startTime | Build start | Stored |
| endTime | Build completion | Stored |
| logLocation | Build log reference | Stored |
| reviewedCommit | Local `master` HEAD commit that was built (the merged, reviewed commit; see § Local Commits Not on Upstream in `workflow.md` for the exception) | Stored |
| artifacts | Produced artifacts | Relationship — see ArtifactIDs |
| confirmed | User's explicit acknowledgement gating Confirmed → Published. Meaningful only on the last-success record | Stored |
| confirmedAt | Confirmation timestamp (nil until confirmed) | Stored |
| published | Set when Publish succeeds. Meaningful only on the last-success record — see Artifact `published` | Stored |
| publishedAt | Publication timestamp (nil until published) | Stored |
| lastSuccessBuildID | ID of the package's last successful build | Stored |
| lastFailureBuildID | ID of the package's last failed build (its directory, log and workspace are kept for inspection) | Stored |

### Ownership

Owns build execution history.

### Lifecycle

Pending → Running → Success or Failed. A successful build becomes the
package's last success; `confirmed` and then `published` are set on it by
the manual Confirm and Publish steps.

### Latest Attempt and Last Success

A package has two build records, each with one job:

- **Latest attempt** — the most recent build, any status (Pending, Running,
  Success, Failed). It answers "how did the most recent build go?" and
  drives build-status displays. It never affects lifecycle.
- **Last success** — a full copy of the most recent *successful* build,
  including its commit, artifacts, `confirmed`, and `published`. It answers
  "what is built and publishable?" and is the sole basis of Built /
  Confirmed / Published (§ Package → Lifecycle).

A successful build is written as both. A running, failed, or cancelled
build touches only the latest attempt; the last success is unchanged. The
`confirmed` / `published` fields are meaningful only on the last success —
Confirm and Publish write only there, and readers never take them from the
latest attempt.

Only the last success's artifacts are kept: the previous success's
artifact records and directory are deleted once the new success is saved
(§ Artifact → Lifecycle).

Migration: packages that predate this split have a single record. If it
holds a success, it becomes the last success on first read. If it holds a
failure, the previous success's `confirmed` / `published` flags were
already overwritten; that package has no last success (derives `Merged`)
until its next successful build.

### Cancellation

A cancelled build is not recorded. Cancelling produces no artifacts and no
outcome, so there is nothing irreducible to store ("Store Only Irreducible
State"); the fact that the user cancelled is recorded in the activity log
instead. Concretely, the build engine restores the package's previous latest-attempt
record (or removes it if this was the first build), deletes the cancelled
build's own directory (log and workspace included), and leaves the previous failed build's directory and
`LastFailureBuildID` untouched. The last-success record is never touched by
a cancel. A cancelled rebuild therefore leaves the
package's derived lifecycle state exactly as it was before the build started.

### Interruption

A build whose process ends without finishing — aur-curator quit while it
ran, crashed, was killed, or the machine lost power — leaves its
latest-attempt record at Pending or Running, because the build engine never
got to finish it. Unlike a cancel, nothing restores the previous record at
that moment.

At the next TUI start, before any operation runs, every latest-attempt
record still at Pending or Running is marked failed, with the same effect as
an ordinary failure: it becomes the package's last failure (its log and
workspace stay inspectable, and its directory is referenced and purged by
the next failure), and the previous failed build's directory is purged. A closing
line is appended to its log saying it was interrupted, and the activity log
records it (`build <pkg> status interrupted`). The last-success record is
never touched, so an interrupted rebuild leaves the derived lifecycle state
as it was. Recovery is idempotent.

Recovery is only safe because the TUI holds a single-instance lock on the
workspace (an exclusive `flock`, released by the kernel when the process
exits for any reason): while it is held, a Running record cannot belong to
a live build. The CLI never builds and never recovers.

A build runs in a workspace inside its own build directory,
`.aur-curator/builds/<build-id>/work/` (the package is cloned into
`work/src`), never in the system temp directory (#307). Successful and
cancelled builds remove their workspace. When a build fails after its
pending record is saved, the workspace is kept for inspection, and its path
is appended to the build log and written to the error log. It stays until
the package's next failed build purges the previous failure's directory,
workspace included, or until `remove` deletes the package's build
directories. An interrupted build's workspace follows the same rule, since
recovery makes it the last failure. If the build fails before its pending
record is saved (the clone, or that save itself), no record names it, so
its directory is removed at once; if the process is killed in that window,
the unnamed directory is removed by `check`'s orphaned-build-directory
sweep. Any failure
after makepkg succeeds — copying an artifact, computing its checksum,
recording it, or saving the build record — records the build as failed.
An artifact is never recorded with an empty checksum: `checksum` is what
`check` matches against the package repository (`workflow.md` § Published:
Stored Flag, Its Limits, and Drift Detection).

Not covered: an interrupted build's makepkg process group may keep running
after a hard kill. Its workspace is kept with the interrupted build (above),
so recovery deletes nothing under it.

### Confirmed

`Build.Confirmed` is a workflow gate, not a content check. It records the 
user's explicit acknowledgement that the produced artifacts are ready to 
publish. It performs no independent verification of build output. `Confirmed` 
is per-Build, not per-Artifact: multiple assets from one build are treated as 
a single Package-level unit, so one confirmation covers the whole artifact set. 
Every new successful build replaces the last success with `Confirmed = false` 
(and `ConfirmedAt` nil) — a new artifact set always requires a fresh confirmation.

**Commit guard.** Confirm and Publish act only on the last success whose `reviewedCommit` 
equals local `master` HEAD. If it does not match — e.g. a merge has moved HEAD past 
it — the operation refuses with a clear message, records nothing, and logs the refusal. 
The UI disables these actions in that case too, but the guard is enforced in the 
operation itself (`workflow.md` § State Transitions and Action Availability, note 9).

### Relationships

- belongs to one Package
- produces one or more Artifacts

---

## Artifact

### Purpose

Represents a built package.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Artifact identifier | Stored |
| buildID | Producing build | Relationship — see BuildID |
| filename | Package filename | Stored |
| version | Built version | Stored |
| architecture | Target architecture | Stored |
| checksum | Integrity verification | Stored |
| size | Package size | Stored |
| published | Publication status | Not a field on Artifact. Stored once per build as `Build.published` on the last-success record — a deliberate trade-off over deriving it from the package repository (#317, deferred). Drift between the flag and the repository is detected by `check`, not prevented; see `workflow.md` § Published: Stored Flag, Its Limits, and Drift Detection |

### Ownership

Owned by a Build.

### Lifecycle

Created → Published → Deleted

Only the last successful build's artifacts are kept. When a new build
succeeds, the previous success's artifact records are deleted first, and
then its build directory; if any record delete fails, the directory is
kept (#352). Remove deletes the package's artifact records too. Copies
already published to the package repository are not affected.

### Relationships

- produced by one Build
- published into one PackageRepository

---

## GitRepository

### Purpose

Represents the Git repository associated with a curated package. It encompasses both the local working repository and its configured remotes (typically origin and upstream), providing the domain identity for repository-related information and long-lived configuration. Transient repository state—such as the working tree status, current branch, commit relationships, and diffs—remains authoritative in Git and is obtained through the Git Provider rather than being persisted in the domain model. As a core abstraction, it is used throughout the import, review, synchronization, and merge workflows.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Repository identifier | Stored |
| location | Filesystem path | Stored |
| origin | Personal Gitea remote (`OriginRemote` in go-domain-structs.md) | Stored |
| upstream | AUR remote (`UpstreamRemote` in go-domain-structs.md) | Stored |
| status | Health/status | Derived — no Status field in the Go struct; computed live from Git |

### Ownership

Owns Git repository metadata, not package lifecycle.

### Lifecycle

Configured → Active → Archived

### Relationships

- belongs to one Package

---

## PackageRepository

### Purpose

Represents the local pacman repository that published packages are installed from.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Repository identifier | Stored |
| location | Filesystem path | Stored |
| status | Health/status | Derived — no Status field in the Go struct; computed live |
| signingStatus | Reserved for future package signing — see roadmap.md Phase 3 | Stored (reserved, unused) |

### Ownership

Owns package repository metadata, not package lifecycle.

### Lifecycle

Configured → Active → Archived

### Relationships

- belongs to one Workspace
- stores many Artifacts

### Design Note: `location`'s Source (DOM-007)

`location` is populated at startup from `WorkspaceConfig.RepositoryPath`
(see § WorkspaceConfig) — there is no separate `PackageRepositoryStore`;
this entity is constructed from the single config file the same way
`Workspace` itself is, not independently persisted. Previously
undocumented; see `domain-package-review.md` DOM-007.

---

## PolicyConfig

### Purpose

Stores the user's overrides to the default category → severity mapping defined in `risk-analysis.md`. The default mapping itself is not persisted here — it's versioned with the application, not user data — only deviations from it are irreducible state.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| id | Identifier | Stored |
| overrides | Category → severity override map | Stored |
| version | Config version, referenced by RiskAssessment.policyVersion | Stored |
| updatedAt | Last modification time | Stored |

### Ownership

Owned by a Workspace. Consumed by Policy Engine when classifying Findings — see `interfaces.md`.

### Lifecycle

Configured → Updated

### Relationships

- belongs to one Workspace

> **Note on `WorkspaceID`:** `PolicyConfig` carries an explicit
> `WorkspaceID` (see `go-domain-structs.md`), which in retrospect assumes
> multi-workspace support the application has never actually needed —
> aur-curator manages exactly one workspace. This wasn't a deliberate
> design choice so much as an unexamined default, and it's called out
> here rather than quietly left as precedent: `WorkspaceConfig` below is
> introduced *without* an equivalent ID/back-reference, and that
> asymmetry is intentional, not an oversight. Whether `PolicyConfig.WorkspaceID`
> itself should eventually be dropped is a separate, not-yet-decided
> question — left for whenever `PolicyConfig` is next touched, rather
> than folded into this change.

---

## WorkspaceConfig

### Purpose

Bootstraps the application's workspace-scoped settings — workspace
location, builder backend selection, analyzer configuration, UI
preferences, and Gitea connection details (see `interfaces.md`,
`prd.md` § Configuration). Loaded once at startup and passed into
`Workspace`'s constructor, the same way `PolicyConfig` is passed into
Policy Engine's constructor — there is no service interface standing
between the config file and its consumer (see Design Note below).

### Attributes

| Attribute | Purpose | Kind | Required |
| ----------- | --------- | --------- | --------- |
| rootPath | Workspace root — one subdirectory per curated package | Stored | **Mandatory** — `config.Load` errors if absent/empty |
| repositoryPath | Local pacman repository path — source of `PackageRepository.location` | Stored | **Mandatory** — `config.Load` errors if absent/empty |
| builderBackend | Selected Builder implementation (e.g. `makepkg`) | Stored | Optional — defaults to `"makepkg"` |
| analyzerConfig | Opaque analyzer settings, shape owned by Analyzer Manager, not yet designed (Sprint 5) | Stored | Optional placeholder — absence is valid, not an error |
| uiPreferences | Opaque UI preferences, shape owned by the UI layer, not yet designed (Sprint 6+) | Stored | Optional placeholder — absence is valid, not an error |
| giteaBaseURL | Gitea instance base URL, used by Import | Stored | **Mandatory** — `config.Load` errors if absent/empty |
| giteaOrg | Gitea organization managed packages are registered under | Stored | **Mandatory** — `config.Load` errors if absent/empty |
| giteaToken | Gitea API token, used by Import | Stored | **Mandatory, precedence-aware** — `config.Load` errors only if both `AUR_CURATOR_GITEA_TOKEN` and this key are absent; see § Secret Handling |

`rootPath` and `repositoryPath` were previously undocumented here —
`rootPath` lived as a `Workspace` field despite this section's own
Purpose text already describing WorkspaceConfig as bootstrapping
"workspace location," and `repositoryPath` had no home anywhere in the
domain model. Both resolved as part of the same freeze; full
mandatory/optional rationale and `config.Load` behavior spec in
`docs/domain/workspace-config-spec.md`. See `domain-package-review.md`
DOM-003, DOM-006, DOM-007.

**Resolved (2026-08-09):** `giteaBaseURL`/`giteaOrg`/`giteaToken` are
mandatory — `config.Load` errors if `giteaBaseURL` or `giteaOrg` is
absent, and errors on `giteaToken` only if neither the
`AUR_CURATOR_GITEA_TOKEN` environment variable nor the file key supplies
a value (precedence-aware mandatory, not "the file key must be set" —
see § Secret Handling and `go-domain-structs.md`'s `WorkspaceConfig`
comment for the distinction). See `domain-package-review.md` DOM-015.

### Ownership

Owned by `Workspace` as a plain embedded value — not a separately
identified, independently stored entity. Unlike `PolicyConfig`, it has
no `ID` and no back-reference; see the note under PolicyConfig above.

### Lifecycle

Loaded at startup → held for the process lifetime → re-loaded and
swapped in whole on an explicit Reload action (no partial/field-level
update).

### Relationships

- belongs to one Workspace (embedded, not referenced by ID)

### Design Note: No Configuration Provider Interface

An earlier draft of this design introduced a `ConfigurationProvider`
service interface (see `interfaces.md`'s prior revision). That interface
is removed: there is exactly one config source (a YAML file on disk),
nothing else has ever needed to implement an alternative, and introducing
the interface added a layer of indirection without a second consumer to
justify it — the same YAGNI reasoning already applied to removing
`History()` from `GitProvider` (see `go-domain-structs.md`). Loading is a
plain function (`config.Load(path string) (WorkspaceConfig, error)`),
mirroring how `PolicyConfig` has no corresponding "Policy Config
Provider" — it's simply passed as a constructor argument.

### Secret Handling: GiteaToken

Resolved (previously flagged as open):

1. **Precedence:** `AUR_CURATOR_GITEA_TOKEN`, if set, overrides the
   YAML file's `giteaToken` key outright — a user who wants no secret on
   disk never needs to set the YAML key at all. `config.Load` checks the
   env var first.

2. **File permissions:** if the token is sourced from the YAML file (not
   the env var), `config.Load` stats the file and warns — toast/log, not
   a refusal to start — if it's group- or world-readable. This is a soft
   guard rail ("highlight rather than block"), consistent with how
   `risk-analysis.md` and the Review screen's Approve-without-analysis
   modal already treat lower-certainty risk signals in this application.

3. **Type:** `GiteaToken` is `Secret`, not `string` (see
   `go-domain-structs.md`) — its `String()`/`GoString()` are redacted so
   an accidental `%+v` of `WorkspaceConfig` (e.g. inside a wrapped error)
   can't leak it. Deliberate access goes through `Secret.Value()`.
   Secret also implements MarshalJSON, returning "REDACTED", since 
   encoding/json does not use String().

4. **No re-persistence.** `Reload` re-resolves from env/file each time;
   the resolved value is never cached or written back out anywhere else.

---

## Workspace

### Purpose

Represents the user's managed aur-curator environment.

### Attributes

| Attribute | Purpose | Kind |
| ----------- | --------- | --------- |
| packageRepository | Configured local pacman repository | Relationship — see PackageRepositoryID |
| policyConfig | User's severity override configuration | Relationship — see PolicyConfigID |
| config | Workspace-scoped bootstrap settings | Relationship — embedded WorkspaceConfig, see § WorkspaceConfig |

`rootPath` moved to `WorkspaceConfig` (DOM-003) — `Workspace` still
answers "where do my files live" via `workspace.Config.RootPath` since
`WorkspaceConfig` is embedded by value, not by reference.

`packages` (previously `Relationship — see PackageIDs`) has been
removed (DOM-004). The curated package list is always derivable from the
filesystem — including immediately after Import, since Import's clone
step (`workflow.md` § Import Mechanics, step 2) puts the package's
directory on disk. `Workspace` does not duplicate what Package Catalog
already discovers with a live filesystem scan on every call
(`interfaces.md` § Package Catalog). "Contains many Packages" below is
still a true relationship conceptually — it's just not a stored one.

(This table's separator row previously had only two dash-columns against
three header columns — corrected as part of this edit, unrelated to
DOM-001–007.)

### Ownership

Root aggregate for the managed collection.

### Lifecycle

Configured → Active

### Relationships

- contains many Packages
- contains one PackageRepository
- contains one PolicyConfig
- embeds one WorkspaceConfig

---

## Aggregate Relationships

```text
Workspace
 ├── Packages
 │     ├── GitRepository
 │     ├── Review (current only — see Review § Current, Not Historical)
 │     │      ├── Findings
 │     │      └── RiskAssessment
 │     └── Builds (latest attempt + last success)
 │            └── Artifacts
 ├── PackageRepository
 │     └── Published Artifacts
 ├── PolicyConfig
 └── WorkspaceConfig (embedded, not a separate store)
```

### Open Questions

- ~~Should Git Repository and Package Repository become separate entities?~~ **Resolved:** Yes — split into GitRepository and PackageRepository.
- ~~Should Decision remain embedded within Review?~~ **Resolved:** Yes — Decision is not a separate entity. `Review.status`, `Review.decisionTime`, and `Review.rationale` carry the trust decision directly.
- ~~Is Review historical (many per package) or current-state (one per package)?~~ **Resolved:** Current-state only. See Review § "Review Scope: Current, Not Historical."
- Should Artifact distinguish build output from published packages?
- ~~Is lifecycle state derived or persisted?~~ **Resolved:** Derived — from the review and the last successful build of local HEAD. See § Package → Lifecycle.
- ~~Should Build reference the exact reviewed commit for reproducibility?~~ **Resolved:** Yes — `Build.ReviewedCommit` (see § Build attributes).
- ~~Is Finding.severity derived or persisted?~~ **Resolved:** Derived, via Policy Engine.
- ~~Is Review.status derived or persisted?~~ **Resolved:** Persisted directly
  on Review. (Previously derived from a separate Decision entity's presence;
  no longer applicable now that Decision is embedded in Review — see above.)
- Should RiskAssessment support re-computation (e.g. a "what would this look like under current policy" view), or does its audit-snapshot nature mean it's strictly write-once?
- Does PolicyConfig need its own version history, or is `updatedAt` sufficient given RiskAssessment already freezes `policyVersion` at the point of use?
- ~~Should Configuration Provider be a service interface, or a plain load function?~~ **Resolved:** Plain function. `WorkspaceConfig` is loaded once at startup and passed as a constructor argument, matching `PolicyConfig`'s relationship to Policy Engine — no interface, no `ConfigurationProvider`, no mockable seam, since there's exactly one config source. See § WorkspaceConfig, Design Note.
- Should `PolicyConfig.WorkspaceID` be dropped now that `WorkspaceConfig` establishes the precedent of not carrying one? **Not decided** — flagged under § PolicyConfig rather than resolved as part of this change; `PolicyConfig`'s multi-workspace-shaped `WorkspaceID` is left alone until it's next touched directly.
