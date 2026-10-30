# Domain Model: aur-curator

> Last updated: 2026-10-26
> Status: Active
> Companion docs: architecture.md, interfaces.md, workflow.md, risk-analysis.md
> Code: `internal/domain`

This document defines aur-curator's domain entities: what each one
represents, which attributes are stored and which are derived, and the
invariants that hold. It does not copy the Go types — `internal/domain`
is the listing. Read the code for field names and this document for what
they mean and why they exist. (It replaces the former
`go-domain-structs.md`.)

## Design Principles

- Git owns repository state whenever possible.
- Persist only irreducible information.
- Relationships express ownership, not implementation.
- Entities represent the problem domain rather than UI or storage concerns.

---

1. [Go Mapping](#go-mapping)
2. [Package](#package)
3. [Review](#review)
4. [Finding](#finding)
5. [RiskAssessment](#riskassessment)
6. [Build](#build)
7. [Artifact](#artifact)
8. [GitRepository](#gitrepository)
9. [PackageRepository](#packagerepository)
10. [PolicyConfig](#policyconfig)
11. [WorkspaceConfig](#workspaceconfig)
12. [Workspace](#workspace)
13. [Aggregate Relationships](#aggregate-relationships)
14. [Open Questions](#open-questions)

## Go Mapping

### Rules

- `internal/domain` holds structs, enums, ID types and store interfaces
  only — **no logic**. Behaviour lives in dedicated packages: lifecycle
  derivation in `internal/lifecycle`, severity and risk in
  `internal/policy`, review workflow in `internal/review`.
- Only **stored** attributes become struct fields. Derived values
  (lifecycle state, local and upstream version, `Finding` severity,
  repository status) are never fields; they are computed on read.
- Relationships are expressed by typed IDs or by the package name, not by
  pointers between entities.
- Store interfaces are declared beside the struct they persist
  (`ReviewStore` in `review.go`, `BuildStore` in `build.go`,
  `ArtifactStore` in `artifact.go`; the last moved there in #394). Stores
  only store: they never derive lifecycle, compute status, or validate Git
  state. The implementations are in `internal/store/json` and write under
  `<rootPath>/.aur-curator/`, never inside a package's own git repository.
- Three entities have no store: `WorkspaceConfig` and `PackageRepository`
  are built from the config file at startup, and `PolicyConfig` is not
  persisted yet (§ PolicyConfig).
- The storage format is an implementation detail and never appears in
  `internal/domain`.

### Files

| File | Contents |
| --- | --- |
| `ids.go` | ID types |
| `id_generators.go` | `NewFindingID`, `NewRiskAssessmentID` |
| `package.go` | `Package` (unused — § Concepts Without a Live Go Type) |
| `review.go` | `Review`, `ReviewStatus`, `FindingsStatus`, `ReviewStore`, `ErrNotFound` |
| `finding.go` | `Finding` |
| `risk_assessment.go` | `Severity`, `RiskAssessment` |
| `build.go` | `BuildStatus`, `Build`, `BuildStore` |
| `artifact.go` | `Artifact`, `ArtifactStore` |
| `repository.go` | `PackageRepository` |
| `policy_config.go` | `PolicyConfig` |
| `workspace.go` | `Secret`, `WorkspaceConfig`, `Workspace` (unused — § Concepts Without a Live Go Type) |

### Identifiers

- **`PackageName`** is a package's sole identifier: its actual name
  (e.g. `yay`). There is no surrogate ID. It is used everywhere a package
  is referenced — `Review.PackageName`, `Build.PackageName`,
  `Artifact.PackageName`, and every store's key. (Earlier drafts had a
  `PackageID` plus a redundant `Name` field; resolved to `PackageName`
  alone.)
- **`ReviewID`** is deterministic: `<name>@<upstreamCommit>`
  (§ Review Identity).
- **`FindingID`** and **`RiskAssessmentID`** have no natural key and are
  random 128-bit hex values from `id_generators.go`. `ReviewID` and
  `PackageName` must never be generated this way.
- **`GitRepositoryID`**, **`WorkspaceID`**, **`PackageRepositoryID`** and
  **`PolicyConfigID`** exist only for fields that reference nothing
  persisted; outside `internal/domain` they appear only as filler values
  in test fixtures (§ GitRepository, § PackageRepository, § PolicyConfig,
  § Workspace; #407).

### Concepts Without a Live Go Type

**Package** and **Workspace** are real domain concepts, but their structs
(`package.go`, and `Workspace` in `workspace.go`) are not used anywhere
outside `internal/domain`. Packages are handled by `PackageName`
throughout, with each relationship resolved through a store, and the
application works with `WorkspaceConfig` directly. Both structs, and the ID
types only they use, are slated for removal (#407). Their sections below
describe the concepts.

---

## Package

### Purpose

Represents one curated AUR package managed by aur-curator. A package is
curated if and only if its directory exists under the workspace root —
Package Catalog discovers the set live (`interfaces.md` § Package
Catalog).

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| name | Canonical package name | Stored — `PackageName` |
| lifecycleState | Current workflow state | Derived — § Lifecycle, `internal/lifecycle` |
| localVersion | Version at the reviewed commit (`Review.UpstreamCommit`), whatever the review state | Derived — from a committed `PKGBUILD`, never the working tree (`workflow.md` § Local Version) |
| upstreamVersion | Latest upstream version | Derived |
| origin / upstream | Personal Gitea repository / AUR repository | Convention — the `origin` and `upstream` remotes of the clone at `<rootPath>/<name>`; not stored (§ GitRepository) |
| currentReview | Active review, if any | Relationship — `ReviewStore.Get(name)` |
| latestBuild | Most recent build attempt, any status | Relationship — `BuildStore.Get(name)` |
| lastSuccessfulBuild | Most recent successful build — the basis of Built / Confirmed / Published | Relationship — `BuildStore.GetLastSuccess(name)` (§ Build → Latest Attempt and Last Success) |
| artifacts | Artifacts of the last successful build | Relationship — `ArtifactStore.GetArtifactsByPackage(name)` |
| published | Whether the last success is published | Derived — the last success's `published` flag (§ Artifact) |

These relationships are resolved through the stores, keyed by name. The
`Package` struct, with its `GitRepositoryID`, `CurrentReviewID` and
`LatestBuildID` fields, is unused (§ Go Mapping → Concepts Without a Live
Go Type).

### Lifecycle

```text
Review Pending → Approved → Merged → Built → Confirmed → Published
      └──────→ Rejected
```

Lifecycle state is **derived, never stored**. Transitions, their guards,
and their failure outcomes are defined in `workflow.md` § State Transitions
and Action Availability (the transition matrix) and are not repeated here.
This section defines what each state means and how it is derived
(`internal/lifecycle`, #312, #309):

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
either — a build holds `BuildPending` only briefly as an internal
initialization marker before becoming `BuildRunning`.

`Confirmed` is a workflow gate, not a content check — it applies
package-wide to the artifact(s) of the last successful build as a single
unit, requiring explicit user acknowledgement before Publish. It performs no
independent verification.

### Relationships

- has one current Review (§ Review → Review Scope: Current, Not Historical)
- has a latest build attempt and at most one last successful build
- has the Artifacts of its last successful build
- belongs to one Workspace

---

## Review

### Purpose

Captures the current human review of a specific upstream repository state.
A Review brings together the review findings, the policy-derived risk
assessment, and the reviewer's decision as a single auditable unit.

Code: `review.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Unique identifier, deterministic from packageName@upstreamCommit — see § Review Identity | Stored |
| packageName | Reviewed package | Stored |
| upstreamCommit | Commit reviewed — the end of the reviewed range | Stored |
| baseCommit | Start of the reviewed range: the merge base of local `master` and `upstream/master` when the review was created; empty on a first review (Import), meaning "nothing reviewed before" — see § Reviewed Range | Stored |
| status | Pending / Approved / Rejected | Stored |
| decisionTime | Approval or rejection timestamp | Stored |
| reviewerNotes | Optional notes, captured on approval | Stored |
| rationale | Optional explanation, captured on rejection | Stored |
| findings | Findings produced during review (may be empty; see findingsStatus) | Stored |
| findingsStatus | Not Run / Run — distinguishes "never analyzed" from "analyzed, zero findings" | Stored |
| riskAssessment | Policy Engine's aggregate risk rollup, computed once when the decision is recorded — see § RiskAssessment | Stored — nil until decided, and stays nil if the review was decided without analyzers having run |

### Ownership

Owns review decisions and metadata that cannot be reconstructed from Git.
The trust decision itself (outcome, rationale, timestamp) is not a separate
entity — there is no `Decision` struct. It is carried directly by
`status`, `rationale`, and `decisionTime` above.

### Lifecycle

Pending → Approved or Rejected. Once decided, status and decisionTime are
immutable. `status` is **stored, not derived**: it is the decision record,
and is never widened with values such as "Merged" — whether the reviewed
commit is merged is a Git fact (§ Package → Lifecycle, rule 3).

### FindingsStatus

`findingsStatus` and `findings` together represent three distinct states.
They must never collapse into one another, in storage or in the UI:

| findingsStatus | findings | Meaning |
| --- | --- | --- |
| `not_run` | nil/empty | Analyzers were never run for this review |
| `run` | empty | Analyzers ran and found nothing |
| `run` | non-empty | Analyzers ran and found N issues |

The Review screen's Findings tab label (`Not Run` / `0 findings` /
`N findings`) depends on this distinction.

Only a complete analyzer run sets `run`. If any analyzer fails, or the run
is cancelled, nothing is saved: `findingsStatus` and `findings` keep their
previous values and the failure is reported, so a failed analyzer never
reads as one that found nothing (#410).

### Review Scope: Current, Not Historical

A Review is anchored to a specific fetched upstream commit
(`upstreamCommit`), compared against local `master` at that moment — it is
an event record answering "should this specific incoming commit be
trusted?", not a live status of local state. The exact range it covers is
stored (`baseCommit` → `upstreamCommit`, § Reviewed Range), so the review
shows the same changes before and after Merge.

`ReviewStore` holds a single, current Review per package — not a history
of all past reviews. When a new upstream commit is fetched:

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

The ID is also what keeps a decision attached to the review it was made
on. Analyses and decisions name the review they act on by its ID, and the
Review Engine refuses them (`ReviewChangedError`, nothing recorded) when
the stored review has a different one — when a Fetch replaced it while it
was on screen (#399; `workflow.md` § State Transitions, note 15).

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

A new Fetch while a Review is Pending replaces that Review (with a new
base) rather than reconciling two live comparisons (§ Review Scope:
Current, Not Historical). Local `master` is an ordinary git repository and
commits can reach it from outside the app; because the base is recorded
rather than recomputed, such commits do not corrupt the reviewed range.
They are unreviewed, and are detected and flagged rather than blocked
(`workflow.md` § Local Commits Not on Upstream, #316).

### ReviewStore

- `Get(name)` returns the package's one current Review, or `ErrNotFound`.
- `Save(name, review)` overwrites it.
- `Delete(name)` removes it; used when a package is removed from curation.

The JSON implementation writes
`<rootPath>/.aur-curator/packages/<name>/review.json`. `RiskAssessment` is
nested inside `Review` rather than stored separately, because `Review` is
the only record `ReviewStore` persists.

### Relationships

- belongs to one Package
- contains many Findings
- contains at most one RiskAssessment

---

## Finding

### Purpose

Represents a single observation produced by an analyzer.

Code: `finding.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Identifier (random, `NewFindingID`) | Stored |
| reviewID | Owning review | Stored |
| analyzer | Producing analyzer | Stored |
| category | Checksum, URL, PKGBUILD, etc. | Stored |
| severity | Low / Medium / High | Derived — computed by Policy Engine from `category` via default/user-override mapping, not assigned by the analyzer |
| summary | Short description | Stored |
| details | Supporting evidence (opaque JSON, shape owned by the analyzer) | Stored |
| location | Optional file/line reference | Stored |

### Ownership

Owned by a Review. `severity` is not owned by Finding itself and is never
a field — it is computed on read by Policy Engine from `category`, so two
identical categories always resolve to the same severity at a given point
in time, but that resolution can change if the user's policy overrides
change. Severity only becomes stored inside a `RiskAssessment` snapshot.

### Lifecycle

Created during analysis → Presented → Replaced with the Review

### Relationships

- belongs to one Review

---

## RiskAssessment

### Purpose

Represents the Policy Engine's objective assessment of a Review's findings.
Computed as an aggregate rollup of the severities of the Review's Findings
— one per Review, not one per Finding — it records the evaluated risk
independently of the human review decision.

Code: `risk_assessment.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Identifier (random, `NewRiskAssessmentID`) | Stored |
| reviewID | Assessed review | Stored |
| highestSeverity | Highest severity among the review's findings | Stored (snapshot) |
| countBySeverity | Count of findings per severity | Stored (snapshot) |
| policyVersion | Policy Engine mapping version in effect when computed | Stored |
| rationale | Optional explanation | Stored |

`Severity` has the three values defined in `risk-analysis.md`: `low`,
`medium`, `high`.

### Ownership

Owned by a Review. Computed once by Policy Engine and frozen — unlike
Finding.severity, which is derived live and can change if policy overrides
change, RiskAssessment is a point-in-time audit snapshot: it records what
the reviewer actually saw at decision time, tagged with the policy version
that produced it. This mirrors "Git is an audit trail," extended to the
review record itself.

Its fields look derived but are deliberately stored; this is the one
exception among values that are otherwise computed on read.

### Lifecycle

Computed when the decision is recorded → immutable thereafter

### Timing

A RiskAssessment is computed exactly once, in `RecordDecision`, at the
moment `status` changes from Pending to Approved **or** Rejected — not
when analysis finishes. It is an audit record of what the reviewer saw,
so it is made for both outcomes. Until then, finding severity is resolved
live (`risk-analysis.md` § Severity Over Time).

It is computed only if analyzers ran (`findingsStatus = run`). A review
decided without analysis has **no** RiskAssessment — nil, not an empty
one — so "never analyzed" stays distinguishable here too (§ Review →
FindingsStatus).

Two details of the stored values:

- With zero findings, `highestSeverity` is `low` and `countBySeverity` is
  empty. Read the counts, not `highestSeverity` alone, to tell "nothing
  found" from "only low findings".
- `policyVersion` comes from the active `PolicyConfig`, which is empty
  today (§ PolicyConfig), so it is currently an empty string.

### Relationships

- belongs to one Review

---

## Build

### Purpose

Represents a single build execution for an approved commit. It provides the
audit trail linking the reviewed source revision to the produced artifacts.

Code: `build.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Build identifier | Stored |
| packageName | Owning package | Stored |
| backend | Builder used (`makepkg`) | Stored |
| status | Pending / Running / Success / Failed | Stored |
| startTime | Build start | Stored |
| endTime | Build completion (nil while running) | Stored |
| logLocation | Build log reference | Stored |
| reviewedCommit | Local `master` HEAD commit that was built (the merged, reviewed commit; see § Local Commits Not on Upstream in `workflow.md` for the exception) | Stored |
| artifactIDs | Produced artifacts | Relationship — `ArtifactID`s |
| confirmed | User's explicit acknowledgement gating Confirmed → Published. Meaningful only on the last-success record | Stored |
| confirmedAt | Confirmation timestamp (nil until confirmed) | Stored |
| published | Set when Publish succeeds. Meaningful only on the last-success record — see Artifact `published` | Stored |
| publishedAt | Publication timestamp (nil until published) | Stored |
| lastSuccessBuildID | ID of the package's last successful build | Stored |
| lastFailureBuildID | ID of the package's last failed build (its directory, log and workspace are kept for inspection) | Stored |

Every record the build engine saves carries both `lastSuccessBuildID` and
`lastFailureBuildID` (#308), so an interrupted build never loses them.

### Lifecycle

Pending → Running → Success or Failed. A successful build becomes the
package's last success; `confirmed` and then `published` are set on it by
the manual Confirm and Publish steps.

### Latest Attempt and Last Success

A package has two build records, both of type `Build`, each with one job:

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

### BuildStore

- `Get` / `Save` address the latest attempt.
- `GetLastSuccess` / `SaveLastSuccess` address the last success (#309).
  Confirm and Publish update only this record.
- `ReadBuildLog` returns a build's log.
- `Delete` removes the latest-attempt record only (used when a cancelled
  first build leaves nothing to restore). The last success is unaffected;
  Remove deletes the whole package store directory.

The JSON implementation stores the two records as `build.json` and
`last-success.json` in the package's store directory.

### Cancellation

A cancelled build is not recorded. Cancelling produces no artifacts and no
outcome, so there is nothing irreducible to store ("Store Only Irreducible
State"); the fact that the user cancelled is recorded in the activity log
instead. Concretely, the build engine restores the package's previous
latest-attempt record (or removes it if this was the first build), deletes
the cancelled build's own directory (log and workspace included), and
leaves the previous failed build's directory and `lastFailureBuildID`
untouched. The last-success record is never touched by a cancel. A
cancelled rebuild therefore leaves the package's derived lifecycle state
exactly as it was before the build started.

### Interruption

A build whose process ends without finishing — aur-curator quit while it
ran, crashed, was killed, or the machine lost power — leaves its
latest-attempt record at Pending or Running, because the build engine never
got to finish it. Unlike a cancel, nothing restores the previous record at
that moment.

At the next TUI start, before any operation runs, every latest-attempt
record still at Pending or Running is marked failed, with the same effect
as an ordinary failure: it becomes the package's last failure (its log and
workspace stay inspectable, and its directory is referenced and purged by
the next failure), and the previous failed build's directory is purged. A
closing line is appended to its log saying it was interrupted, and the
activity log records it (`build <pkg> status interrupted`). The
last-success record is never touched, so an interrupted rebuild leaves the
derived lifecycle state as it was. Recovery is idempotent.

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
sweep. Any failure after makepkg succeeds — copying an artifact, computing
its checksum, recording it, or saving the build record — records the build
as failed. An artifact is never recorded with an empty checksum:
`checksum` is what `check` matches against the package repository
(`workflow.md` § Published: Stored Flag, Its Limits, and Drift Detection).

Not covered: an interrupted build's makepkg process group may keep running
after a hard kill. Its workspace is kept with the interrupted build
(above), so recovery deletes nothing under it.

### Confirmed

`confirmed` is a workflow gate, not a content check. It records the user's
explicit acknowledgement that the produced artifacts are ready to publish.
It performs no independent verification of build output — a stored gate,
like `Review.status`, not a computed check. `confirmed` is per-Build, not
per-Artifact: multiple assets from one build are treated as a single
Package-level unit, so one confirmation covers the whole artifact set.
Every new successful build replaces the last success with
`confirmed = false` (and `confirmedAt` nil) — a new artifact set always
requires a fresh confirmation.

**Commit guard.** Confirm and Publish act only on the last success whose
`reviewedCommit` equals local `master` HEAD. If it does not match — e.g. a
merge has moved HEAD past it — the operation refuses with a clear message,
records nothing, and logs the refusal. The UI disables these actions in
that case too, but the guard is enforced in the operation itself
(`workflow.md` § State Transitions and Action Availability, note 9).

### Relationships

- belongs to one Package
- produces one or more Artifacts

---

## Artifact

### Purpose

Represents a built package file.

Code: `artifact.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Artifact identifier | Stored |
| buildID | Producing build | Relationship — `BuildID` |
| packageName | Owning package | Stored — deliberate denormalization, see below |
| filename | Package filename | Stored |
| version | Built version | Stored |
| architecture | Target architecture | Stored |
| checksum | Integrity verification; never empty (§ Build → Interruption) | Stored |
| size | Package size | Stored |
| published | Publication status | Not a field on Artifact. Stored once per build as `Build.published` on the last-success record — a deliberate trade-off over deriving it from the package repository (#317, deferred). Drift between the flag and the repository is detected by `check`, not prevented; see `workflow.md` § Published: Stored Flag, Its Limits, and Drift Detection |

**`packageName` is a deliberate exception** to "relationships via typed
IDs": it duplicates what is reachable via `buildID → Build.packageName`.
It is kept because artifacts are queried directly by package
(`GetArtifactsByPackage`, e.g. Package Details' artifact history), and
joining through `BuildStore` for every such lookup isn't worth the purity
cost. Decided 2026-09-08.

### Ownership

Owned by a Build.

### Lifecycle

Created → Published → Deleted

Only the last successful build's artifacts are kept. When a new build
succeeds, the previous success's artifact records are deleted first, and
then its build directory; if any record delete fails, the directory is
kept (#352). Remove deletes the package's artifact records too. Copies
already published to the package repository are not affected.

### ArtifactStore

Records are keyed by `ArtifactID`. `GetArtifactsByPackage` and `ListAll`
can return a partial list **together with** a non-nil error: the error
names every record that could not be read. Because an unreadable record's
package is unknown, `GetArtifactsByPackage` reports unreadable records
from the whole store, not only the requested package. A caller that cannot
use partial data must treat any error as a failure.

### Relationships

- produced by one Build
- published into one PackageRepository

---

## GitRepository

**Not modelled, by design.** There is no `GitRepository` struct and no
store. Everything such an entity would hold is either a fixed convention
or a one-line derivation:

- location: `<rootPath>/<name>` — a normal clone, not a bare mirror
- remotes: `origin` (personal Gitea repository) and `upstream` (AUR)
- status, commits behind, merge status, diffs, history: live from Git
  through the Git Provider (`interfaces.md` § Git Provider)

No consumer needs these bundled as a stored or derived unit. If a real need
appears, it should be a Git Provider read accessor (derived on call), never
a persisted struct. `GitRepositoryID` remains in `ids.go` only because
`Package.GitRepositoryID` references it; both are vestigial (#407).

---

## PackageRepository

### Purpose

Represents the local pacman repository that published packages are
installed from.

Code: `repository.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| location | Filesystem path | Stored — from `WorkspaceConfig.repositoryPath` |
| status | Health/status | Derived — computed live by the Publisher; not a field |
| signingStatus | Reserved for future package signing (roadmap.md) | Stored (reserved, unused) |

The repository database's filename comes from
`WorkspaceConfig.repoDBName`, not from this entity.

### Ownership

Owns package repository metadata, not package lifecycle.

There is exactly one `PackageRepository` per workspace, built once from
config at startup. It deliberately has **no `id`**, no store and no
service interface. (`Workspace.PackageRepositoryID` therefore refers to
nothing persisted.)

### Relationships

- belongs to one Workspace
- stores the published Artifacts

---

## PolicyConfig

### Purpose

Holds the user's overrides to the default category → severity mapping
defined in `risk-analysis.md`. The default mapping itself is not stored
here — it is versioned with the application, not user data — only
deviations from it are irreducible state.

Named `PolicyConfig`, not `PolicyEngine`, to avoid colliding with the
Policy Engine service (`interfaces.md`) that applies it: this struct is the
engine's configuration, not the engine.

Code: `policy_config.go`.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Identifier | Stored |
| workspaceID | Owning workspace | Stored — set only by test fixtures; see note below |
| overrides | Category → severity override map (opaque JSON) | Stored |
| version | Config version, referenced by RiskAssessment.policyVersion | Stored |
| updatedAt | Last modification time | Stored |

### Current state

`PolicyConfig` is **not persisted or loaded yet**: there is no store and
no config key for it, and the application passes an empty `PolicyConfig`
to Policy Engine at startup, so only the default mapping applies. It is
passed as a constructor argument, the same pattern as `WorkspaceConfig`.

### Ownership

Owned by a Workspace. Consumed by Policy Engine when classifying Findings.

### Relationships

- belongs to one Workspace

> **Note on `workspaceID`:** it assumes multi-workspace support the
> application has never needed — aur-curator manages exactly one
> workspace. This was an unexamined default, not a design choice.
> `WorkspaceConfig` deliberately has no equivalent field. Whether to drop
> `workspaceID` is undecided (§ Open Questions).

---

## WorkspaceConfig

### Purpose

The application's workspace-scoped settings: workspace and repository
locations, Gitea connection details, automation switches, and log
location. Loaded once at startup and held for the process lifetime.

Code: `workspace.go`; loading in `internal/config`.

### Attributes

| Key | Purpose | Required |
| --- | --- | --- |
| rootPath | Workspace root — one subdirectory per curated package | **Mandatory** |
| repositoryPath | Local pacman repository path — source of `PackageRepository.location` | **Mandatory** |
| repoDBName | Filename of the repository database archive inside `repositoryPath`, used by `repo-add`; the part before `.db` is the repository name in `pacman.conf` | Optional — defaults to `custom.db.tar.gz` |
| giteaBaseURL | Gitea instance base URL | **Mandatory** |
| giteaOrg | Gitea organization curated packages live under | **Mandatory** |
| giteaToken | Gitea API token | **Mandatory, precedence-aware** — see § Secret Handling |
| autoMerge | Merge automatically after a successful Approve, with no prompt; otherwise the review stays Approved until the user merges | Optional — defaults to false |
| autoAnalyzeAfterImport | Run analyzers automatically after a successful Import (Import's clone already establishes `upstream/master`, so no Fetch is needed). A failed auto-analyze is reported but does not undo Import | Optional — defaults to false |
| activityLogPath | The always-on, append-only activity log | Optional — defaults to `<rootPath>/.aur-curator/activity.log` |
| builderBackend | Builder implementation. `makepkg` is the only one; the key is there for a future sandboxed backend | Optional — defaults to `makepkg`; any other value is an error (#408) |

The activity log (one human-readable line per lifecycle event — import,
approve, reject, merge, build, confirm, publish — success and failure
alike) is distinct from the error log (`internal/errorlog`: always on,
failures only, machine-parseable) and the trace (`internal/trace`: opt-in
developer diagnostics).

### Loading

`config.Load(path)` is a plain function returning a `WorkspaceConfig`; it
errors if a mandatory key is missing or empty and applies the defaults
above. There is no `ConfigurationProvider` interface: there is exactly one
config source, and an interface would add indirection with no second
implementation to justify it. `WorkspaceConfig` is passed as a constructor
argument, and `main` hands each dependency only the fields it needs
(`developer-guide.md`). Nothing re-reads the file while the application
runs — the Dashboard's `R` Refresh reloads package data, not config — so
a config change takes effect at the next start.

The config file is JSON with comments, decoded **directly into
`WorkspaceConfig` via its `json` tags** — there is no intermediate struct
to keep in sync. The tags are load-bearing: a field without one is never
loaded. A new field needs only a tag;
`TestLoad392_EveryFieldLoads` in `internal/config` fails if a field has no
tag or no fixture entry. (#392: `autoMerge`, and earlier
`autoAnalyzeAfterImport` (#146), were silently never loaded because a
separate hand-copied file struct lacked them.)

`analyzerConfig` and `uiPreferences` were opaque placeholders with no
design behind them and were never read; #408 removed them. They come back
when their shape is designed.

### Validation

`config.Load` checks the file before anything else runs. Errors stop the
program; warnings go to stderr and do not (#408).

**Comments.** `//` and `/* */`, outside strings. An unterminated `/*` is an
error: it would otherwise swallow the rest of the file, and the keys after
it would quietly fall back to their defaults.

**Keys.**

- A key that matches a known key only by case (`AutoMerge`) is an error.
  `encoding/json` matches keys case-insensitively, so such a key would be
  applied, not ignored; the user is made to fix the spelling instead.
- Any other unknown key is a **warning**, naming the key. A misspelling
  is caught without breaking a config that carries a key from another
  version.

**Values.** Every string value has leading and trailing whitespace removed
before it is checked or used.

| Key | Rule |
| --- | --- |
| rootPath, repositoryPath | Not empty. A leading `~` or `~/` is expanded to the home directory; the result must be absolute, and is cleaned (`filepath.Clean`) |
| activityLogPath | When set: as above |
| repositoryPath | Not the same as `rootPath`, and neither inside the other. The catalog treats every subdirectory of `rootPath` as a package |
| giteaBaseURL | Not empty; parses as a URL with scheme `http` or `https` and a host |
| giteaOrg | Not empty; no `/` |
| giteaToken | See § Secret Handling: a blank value counts as not set |
| repoDBName | A plain file name (no `/`) ending in `.db.tar.gz`, with a name before `.db`. `repo-add` accepts other compressions, but `internal/repodb` reads gzip only |
| builderBackend | `makepkg` |

**Not checked:** whether directories exist (Publish creates the repository
directory; the code that uses a path reports its errors), duplicate keys
(the last one wins), and a UTF-8 byte-order mark (rejected as invalid
JSON).

### Secret Handling: GiteaToken

1. **Precedence:** `AUR_CURATOR_GITEA_TOKEN`, if set, overrides the file's
   `giteaToken` key outright — a user who wants no secret on disk never
   sets the key at all. `config.Load` checks the env var first, and errors
   only if **both** sources are absent. A value that is empty or only
   whitespace counts as absent, in either source; `GiteaTokenSource`
   applies the same rule. (`giteaBaseURL` and `giteaOrg` have no
   alternate source and are checked against the file alone.)
2. **File permissions:** if the token comes from the file, `config.Load`
   warns — it does not refuse to start — if the file is group- or
   world-readable: highlight rather than block, as with other
   lower-certainty risk signals (`risk-analysis.md`). If the permissions
   cannot be read, the warning says that instead, with the reason.
3. **Type:** `giteaToken` is a `Secret`, not a `string`. `String()` and
   `GoString()` return `REDACTED`, so an accidental `%v`/`%+v` of
   `WorkspaceConfig` (including inside a wrapped error) cannot leak it.
   `MarshalJSON` is redacted too, because `encoding/json` ignores
   `String()`. There is deliberately no `UnmarshalJSON`: decoding a JSON
   string into `Secret` works natively, which is how `config.Load` reads
   the key. Deliberate access is always through `Value()`.
4. **No re-persistence:** the resolved value is never cached or written
   anywhere else.

### Ownership

Owned by `Workspace` as a plain embedded value — not a separately
identified, independently stored entity. It has no `id` and no
back-reference to `Workspace`.

### Relationships

- embedded in one Workspace

---

## Workspace

### Purpose

Represents the user's managed aur-curator environment. Root aggregate for
the curated collection.

The `Workspace` struct in `workspace.go` is unused; the application works
with `WorkspaceConfig` directly (§ Go Mapping → Concepts Without a Live
Go Type). The attributes below describe the concept.

### Attributes

| Attribute | Purpose | Kind |
| --- | --- | --- |
| id | Identifier | Stored |
| packageRepository | Local pacman repository | Relationship — `PackageRepositoryID`; refers to nothing persisted (§ PackageRepository) |
| policyConfig | Severity override configuration | Relationship — `PolicyConfigID`; refers to nothing persisted (§ PolicyConfig) |
| config | Workspace-scoped settings | Embedded `WorkspaceConfig`, by value |

The workspace root is `config.rootPath`, not a field of its own.

There is **no package list**. The curated set is always derived from the
filesystem by Package Catalog's live scan (`interfaces.md` § Package
Catalog) — including immediately after Import, since Import's clone puts
the package's directory on disk (`workflow.md` § Import). "Contains many
Packages" is a true relationship, just not a stored one.

### Relationships

- contains many Packages
- contains one PackageRepository
- contains one PolicyConfig
- embeds one WorkspaceConfig

---

## Aggregate Relationships

```text
Workspace
 ├── Packages (discovered from the filesystem, not stored)
 │     ├── git clone: origin + upstream (not modelled — Git owns it)
 │     ├── Review (current only)
 │     │      ├── Findings
 │     │      └── RiskAssessment
 │     └── Builds (latest attempt + last success)
 │            └── Artifacts (last success only)
 ├── PackageRepository
 │     └── Published Artifacts
 ├── PolicyConfig
 └── WorkspaceConfig (embedded, not a separate store)
```

---

## Open Questions

- Should Artifact distinguish build output from published packages
  (related: deriving `published` from the repository, #317)?
- Should RiskAssessment support re-computation (e.g. a "what would this
  look like under current policy" view), or does its audit-snapshot nature
  make it strictly write-once?
- Does PolicyConfig need its own version history, or is `updatedAt`
  sufficient given RiskAssessment already freezes `policyVersion` at the
  point of use?
- Should `PolicyConfig.workspaceID` be dropped, following
  `WorkspaceConfig`'s precedent? Left until PolicyConfig is next touched
  (e.g. when it is first persisted).
