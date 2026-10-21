# Interfaces

> Last updated: 2026-10-19
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
                        │
        ┌───────────────┼────────────────┐
        │               │                │
        ▼               ▼                ▼
 Package Catalog   Review Engine     Build Engine
        │               │                │
        │               ▼                ▼
        │       Analyzer Manager     Builder
        │               │                │
        ▼               ▼                ▼
 Git Provider      Analyzers       Publisher
```

Each component owns a single responsibility.

Interfaces below are grouped into two kinds:

- **Service Interfaces** — coordinate behavior against domain entities.
  They may read or write state, but their responsibility is expressed as
  *what they do*, not *what they store*. Not all front their own
  persistence — Package Catalog, for instance, performs live filesystem
  discovery with no cache or private store at all (see below).
- **Persistence Interfaces** — store and retrieve specific domain records.
  Their responsibility is expressed as *what they persist*, matching a
  narrow `Get`/`Save`/`Delete`-shaped contract in `go-domain-structs.md`.
  They contain no business logic and never derive or validate state.

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

> Renamed from "Repository Store." The previous name collided with
> `GitRepository` and `PackageRepository` and, worse, was store-shaped
> naming applied to what is actually a discovery/coordination service —
> see below.

#### Purpose

Discover the curated package collection by scanning the filesystem
directly. **The filesystem is the registry** — a package is curated if
and only if its directory exists under the workspace root
(`architecture.md` § Repository Layout). There is no cache: every call
performs a live scan (see `domain-package-review.md` DOM-004 and the
full `docs/interface-packets/package-catalog.md` § Design Note: No
Cache for why a cache was considered and rejected — a plain directory
listing is cheap enough at this project's ~10-12 package scale that
caching it would add real complexity for no measurable benefit).

#### Responsibilities

- discover repositories under the workspace, live, on every call (the
  authoritative enumeration — see `package-catalog.md` for what counts
  as a curated directory)
- resolve a package's local repository path (pure path computation, no
  I/O)

#### Owns

Nothing stored — it doesn't own a cache, a store, or any persisted
state. It owns the *discovery logic* (what counts as a curated
directory), not any data derived from running it. The underlying fact of
curation belongs to whatever exists on disk under the workspace root; it
does not own Git state (see Git Provider) or pacman repository state
(see Publisher / `PackageRepository`).

This is a **service**, not a persistence interface, despite having no
persistence of its own — its responsibility is *what it does* (scan,
resolve a path), matching this document's own definition of Service
Interfaces above, not *what it stores*, since it stores nothing. Any
consumer needing to know the current package set (e.g. Git Provider's
Import, after it has already cloned a package's directory into place)
calls this interface directly; the next call anywhere simply sees the
new directory, since every call is already live.

#### Design Note: `RepositoryLocator` Eliminated

An earlier, undocumented design had `git.Provider` depend on a separate
`RepositoryLocator` interface (`RepositoryPath`, `Register`) rather than
on Package Catalog directly. This was decided against in conversation
during Sprint 6 planning, but the decision was never written into this
document — it only ever lived in `s06-app-dashboard.md`'s prose and
`dependencies.go`'s field, leaving the confirmed `git-provider.md`
interface packet (and the actual `internal/git` code) still depending on
`RepositoryLocator` unchanged. See
`git-package-review.md` NOTE-001 for the full history of this drift.

**Resolved here:** `RepositoryLocator` is eliminated. `git.Provider`
depends on `catalog.PackageCatalog` directly for the one of
`RepositoryLocator`'s two former responsibilities that survives —
resolving a package's local repository path. The other
(`Register`) has no equivalent at all: since `PackageCatalog` performs a
live scan on every call rather than maintaining a cache, there's nothing
to register or refresh — Import simply stops calling anything after its
clone completes. See `docs/interface-packets/package-catalog.md` for the
full interface spec and its § Migration Note. `git-provider.md` needs
regenerating from this document to reflect the removal; that packet is
not edited here since it isn't part of this canonical-doc set.

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

#### Responsibilities

- discover analyzers
- execute analyzers
- collect findings
- normalize analyzer output
- tolerate analyzer failures

#### Owns

Analyzer execution.

It does not decide whether findings are acceptable.

---

### Analyzer

#### Purpose

Inspect packages for a specific class of issues.

#### Responsibilities

Each analyzer should examine one aspect of a package.

Examples include:

- PKGBUILD analyzer
- .install analyzer
- URL analyzer
- Checksum analyzer
- Shell analyzer
- Git history analyzer
- Metadata analyzer

Future analyzers may include:

- traur
- aurscan
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
- apply user policy
- group related findings (Not in scope (#19): findings are counted by severity only. RiskAssessment.Rationale is reserved and currently never set.)

#### Owns

Risk classification.

This separates "what was found" from "how important it is."

---

### Builder

#### Purpose

Produce installable packages.

#### Responsibilities

- build packages
- stream build logs
- report build results
- expose build artifacts

Possible implementations:

- makepkg
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

Persist review history.

#### Responsibilities

- reviewed range (base commit and reviewed upstream commit)
- review status and rationale (approved/rejected, with optional explanation)
- reviewer notes
- timestamps

#### Owns

Review metadata.

It deliberately stores only information that cannot be derived from Git.

---

### Build Store

#### Purpose

Persist build history.

#### Responsibilities

- build timestamps
- build success/failure
- produced artifacts
- build backend used

#### Owns

Historical build information.

## Design Principles

Each interface should satisfy the following principles:

- Own one responsibility.
- Hide implementation details.
- Expose capabilities rather than implementation.
- Be independently testable.
- Support multiple implementations.
- Avoid knowledge of unrelated subsystems.

Interfaces should communicate using domain objects (Package, Review, Finding, BuildResult, Repository) rather than implementation-specific data structures.

---

## Future Interfaces

These are anticipated but not yet required.

- Notification Provider
- Package Signer
- CVE Provider
- Release Notes Provider
- Audit Report Generator
