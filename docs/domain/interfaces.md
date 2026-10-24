# Interfaces

> Last updated: 2026-10-22
> Status: Active
> Companion docs: domain.md, architecture.md
> This document defines the major architectural interfaces within aur-curator.
>
> These are architectural contracts rather than language-specific interfaces.
> Their purpose is to separate responsibilities and allow independent evolution
> of components.

---

## Overview

```text
                           Bubble Tea UI
         ┌──────────────┬───────┴───────┬──────────────┐
         ▼              ▼               ▼              ▼
  Package Catalog  Review Engine   Build Engine    Publisher
         ▲          │    │    │      │      │
         │          │    ▼    ▼      │      ▼
         │          │ Analyzer Policy│   Builder
         │          │ Manager  Engine│  (makepkg)
         │          │    │           │
         │          │    ▼           │
         │          │ Analyzers      │
         │          ▼                │
         └───── Git Provider ◄───────┘
```

Arrows point from a component to what it is constructed with. Stores are
left out of the picture. The full wiring is done once at startup — the Git
Provider and Package Catalog in `cmd/aur-curator/main.go`, everything else
in `internal/deps`:

| Component | Constructed with |
| --- | --- |
| Package Catalog | workspace root |
| Git Provider | Package Catalog (repository paths), a git command runner, Gitea client (Import), workspace root |
| Review Engine | Git Provider, Review Store, Analyzer Manager, Policy Engine |
| Analyzer Manager | a logger (currently none) |
| Policy Engine | `PolicyConfig` (currently empty — `domain.md` § PolicyConfig) |
| Build Engine | Builder (`MakepkgBuilder`), Build Store, Artifact Store, Git Provider, store paths |
| Publisher | Build Store, Artifact Store, `PackageRepository`, workspace root, repository database name |

Each component owns a single responsibility.

Interfaces below are grouped into two kinds:

- **Service Interfaces** — coordinate behavior against domain entities.
  They may read or write state, but their responsibility is expressed as
  *what they do*, not *what they store*. Not all front their own
  persistence — Package Catalog, for instance, performs live filesystem
  discovery with no cache or private store at all (see below).
- **Persistence Interfaces** — store and retrieve specific domain records.
  Their responsibility is expressed as *what they persist*, matching a
  narrow `Get`/`Save`/`Delete`-shaped contract declared in
  `internal/domain` (`domain.md` § Go Mapping). They contain no business
  logic and never derive or validate state.

---

## Service Interfaces

### Git Provider

#### Purpose

Provide repository information and perform Git operations.

#### Responsibilities

- import new packages (create Gitea repository, clone upstream, establish
  remotes, reset local `master` to an empty state) — see `workflow.md`
  § Import
- fetch upstream repositories
- determine repository status
- compute commits behind
- generate diffs: the live comparison of local `master` with
  `upstream/master` (including its merge base, which becomes a new
  review's base commit), and the diff of a stored review range — base
  commit to reviewed upstream commit — independent of where `master` is
  now (`domain.md` § Reviewed Range)
- retrieve commit history
- perform merges
- expose repository metadata

#### Owns

Repository state.

It is the authoritative source for all Git-derived information.

---

### Package Catalog

Code: `internal/catalog` (`PackageCatalog`, implemented by
`FilesystemCatalog`, constructed with `WorkspaceConfig.rootPath`).

#### Purpose

Discover the curated package collection by scanning the filesystem
directly. **The filesystem is the registry** — a package is curated if
and only if its directory exists under the workspace root
(`architecture.md` § Repository Layout). There is no cache: every call
performs a live scan (§ Design Note: No Cache).

#### Responsibilities

- **`Packages(ctx)`** — list the curated set, live, on every call: every
  direct subdirectory of the workspace root whose name does not start
  with `.` (so `.aur-curator/` and similar are never packages). Files are
  ignored. Names are returned in directory order (sorted by name); an
  empty workspace yields an empty result, not an error.
- **`RepositoryPath(name)`** — resolve a package's local repository path
  (`<rootPath>/<name>`). Pure path computation: no I/O, and the directory
  need not exist.

There are deliberately no other methods — no `Refresh` (every call is
already live, so there is nothing to refresh) and no `Register`,
`Unregister`, `Add` or `Remove` (membership is a filesystem fact; nothing
is granted or revoked). Import calls nothing on the catalog after its
clone completes: the next `Packages` call anywhere sees the new directory.
Remove deletes the directory; that is the whole of "un-registering"
(`workflow.md` § Remove).

#### What Is Not Validated

`Packages` does **not** check that a listed directory is a Git repository
with both `origin` and `upstream` remotes (`architecture.md` § Repository
Model). A directory existing is enough to be listed. A malformed directory
surfaces later, as a real Git error, the first time something performs a
Git operation on it (Fetch, Diff, Merge).

This is an accepted trade-off, not an oversight. An earlier design
validated remotes on every scan, which meant a `git` subprocess per
directory per call — noticeable overhead when the Dashboard re-reads the
package list on every navigation. At this project's scale (a handful of
manually curated packages, not an untrusted multi-user filesystem), a late
but explicit Git error is an acceptable failure mode.

#### Failure Semantics

- `Packages` errors only if the context is already cancelled or the
  workspace root itself cannot be read (missing, permission denied) —
  never because an individual package directory is malformed.
- `RepositoryPath` errors only for a structurally invalid name: empty, or
  containing a path separator or `..`. It never errors because a package
  is not currently curated; a caller that needs to know that checks
  membership in `Packages` first.

#### Owns

Nothing stored — no cache, no store, no persisted state. It owns the
*discovery rule* (what counts as a curated directory), not any data
derived from running it. The fact of curation belongs to what exists on
disk under the workspace root; the catalog does not own Git state (see
Git Provider) or pacman repository state (see Publisher).

It is a **service**, not a persistence interface: its responsibility is
what it does (scan, resolve a path), not what it stores. Any component
that needs "what is currently curated" calls `Packages` rather than
scanning the filesystem itself — centralizing the discovery rule is the
reason the interface exists.

#### Design Note: No Cache

A cache was designed and rejected: a `catalog.json` file with
mtime-based invalidation and a `Refresh` method. Live discovery on every
call was chosen instead, for two reasons:

1. **A directory listing is cheap at this scale.** The curated set is
   about 10–12 packages (`prd.md`); listing that many directory entries
   costs microseconds.
2. **The real cost was elsewhere.** The concern that prompted the cache —
   the Dashboard re-reading the package list on every screen switch —
   turned out to be the per-directory remote validation, not the listing.
   Removing the validation (§ What Is Not Validated) removed the cost.
   Caching an already-cheap listing would have added a schema, staleness
   logic, and failure handling for the cache file itself, to solve a
   performance problem that does not exist.

If the workspace ever grows large enough for a live scan to be
measurable, revisit then; `Packages`' signature does not preclude adding a
cache behind it.

#### Design Note: No Separate Path Locator

An earlier design had the Git Provider depend on a separate
`RepositoryLocator` interface (`RepositoryPath`, `Register`). It was
eliminated: the Git Provider depends on `PackageCatalog` for the one
responsibility that survived — resolving a package's repository path.
`Register` has no equivalent, because there is nothing to register
(§ Responsibilities). `RepositoryPath` also lost its context parameter,
since it performs no I/O.

---

### Review Engine

#### Purpose

Coordinate the package review workflow.

#### Responsibilities

- initiate reviews
- collect analyzer findings
- present review information
- record review decisions
- determine review status

#### Owns

Review workflow.

It does **not** perform analysis itself.

---

### Analyzer Manager

#### Purpose

Coordinate one or more analyzers.

Code: `internal/analyzer` (`Manager`, `New`).

#### Responsibilities

- run the built-in set of analyzers — a fixed set compiled in; there is
  no discovery or plugin mechanism
- give each analyzer the diff of the review's stored range (base commit
  to reviewed upstream commit), never the live `master...upstream/master`
- collect and normalize their findings
- report analyzer failures: a failing analyzer does not stop the
  others, and the run returns the findings of those that succeeded plus
  an error naming every analyzer that failed. A run with an error is
  incomplete: the Review Engine saves nothing from it (#410)

#### Owns

Analyzer execution.

It does not decide whether findings are acceptable.

---

### Analyzer

#### Purpose

Inspect packages for a specific class of issues.

#### Responsibilities

Each analyzer examines one aspect of the reviewed changes.

Current analyzers (rules in `analyzer-specs.md`):

- URL analyzer — `url_analyzer.go`
- Checksum analyzer — `checksum_analyzer.go`
- Shell analyzer — `shell_analyzer.go`

Possible future analyzers:

- `.install` analyzer
- Git history / maintainer analyzer
- metadata analyzer
- external tools: traur, aurscan
- local AI analyzers

#### Owns

Production of findings.

---

### Policy Engine

#### Purpose

Convert findings into meaningful risk assessments.

#### Responsibilities

- classify findings
- determine severity
- apply user policy (overrides from `PolicyConfig`; none are persisted
  yet, so only the default mapping applies — `domain.md` § PolicyConfig)
- group related findings (Not in scope (#19): findings are counted by
  severity only. `RiskAssessment.Rationale` is reserved and currently
  never set.)

#### Owns

Risk classification.

This separates "what was found" from "how important it is."

---

### Build Engine

Code: `internal/build` (`Engine`, `NewEngine`).

#### Purpose

Coordinate building a package from its reviewed, merged commit.

#### Responsibilities

- resolve the commit to build: local `master` HEAD
- clone that commit into a disposable workspace inside the build's own
  directory, never building from the working tree
- delegate the build itself to a Builder
- persist the build records and artifacts through the Build Store and
  Artifact Store — latest attempt and last success, cancellation,
  failure-workspace retention (`domain.md` § Build)
- recover builds left Pending or Running by an interrupted process, at TUI
  start, under the instance lock (`domain.md` § Build → Interruption)

#### Owns

The build workflow and its records. It does not run makepkg itself.

---

### Builder

Code: `internal/build` (`Builder`, `MakepkgBuilder`).

#### Purpose

Produce installable packages from a prepared workspace.

#### Responsibilities

- build packages
- stream build logs
- report build results (`BuildResult`)
- expose build artifacts

Current implementation: `MakepkgBuilder`, always used (the
`builderBackend` config key is not read yet — #408).

Possible future implementations:

- clean chroot
- systemd-nspawn
- Podman
- remote builder

#### Owns

Package construction.

---

### Publisher

#### Purpose

Maintain the local pacman repository.

#### Responsibilities

- publish package artifacts
- update repository metadata
- verify repository integrity
- support repository signing (future)

#### Owns

Repository publication.

---

### Bubble Tea UI

#### Purpose

Present information and orchestrate workflows.

#### Responsibilities

- display state
- collect user input
- coordinate subsystem interactions
- present progress
- display results

The UI should contain minimal business logic.

---

## Persistence Interfaces

### Review Store

#### Purpose

Persist the current review of each package — one record per package, not
a history (`domain.md` § Review Scope: Current, Not Historical).

#### Responsibilities

- reviewed range (base commit and reviewed upstream commit)
- review status and rationale (approved/rejected, with optional explanation)
- reviewer notes
- findings, findings status and the decision-time risk assessment
- timestamps

#### Owns

Review metadata.

It deliberately stores only information that cannot be derived from Git.

---

### Build Store

#### Purpose

Persist each package's two build records: the latest attempt and the
last success (`domain.md` § Build → Latest Attempt and Last Success).

#### Responsibilities

- build timestamps
- build success/failure
- produced artifacts (by ID)
- build backend used
- the Confirm and Publish gates, on the last-success record
- access to build logs

#### Owns

Build records.

---

### Artifact Store

#### Purpose

Persist the artifact records of each package's last successful build.

#### Responsibilities

- artifact metadata: filename, version, architecture, checksum, size
- lookup by ID and by package
- reporting unreadable records alongside the readable ones, so callers
  can decide whether partial data is usable (`domain.md` § Artifact →
  ArtifactStore)

#### Owns

Artifact metadata. The package files themselves live in build directories
and the package repository, not in the store.

---

## Design Principles

Each interface should satisfy the following principles:

- Own one responsibility.
- Hide implementation details.
- Expose capabilities rather than implementation.
- Be independently testable.
- Support multiple implementations.
- Avoid knowledge of unrelated subsystems.

Interfaces should communicate using domain types (`PackageName`, `Review`,
`Finding`, `Build`, `Artifact`) rather than implementation-specific data
structures.

---

## Future Interfaces

These are anticipated but not yet required.

- Notification Provider
- Package Signer
- CVE Provider
- Release Notes Provider
- Audit Report Generator
