# PRD: aur-curator

> Last updated: 2026-08-09
> Status: Active
> Companion docs: roadmap.md

## Vision

Because the AUR is an unvetted, user-submitted repository, installing updates blindly exposes users to malware, compromised maintainers, and supply-chain attacks. aur-curator is a Bubble Tea TUI designed to enforce a strict due-diligence process.

- It maintains a personally curated collection of personally reviewed AUR packages in Gitea
- It automates the mechanical work of fetching, building, and publishing, but strictly gates integration behind human review—ensuring no unvetted code ever touches your system.

The aur-curator trust chain:

```text
AUR upstream
      │
      ▼
Review upstream changes
      │
      ▼
Personal Gitea repository
      │
      ▼
Build locally with makepkg
      │
      ▼
Install with pacman
```

## Goals

- Review every PKGBUILD update.
- Never auto-merge.
- Never auto-install.
- Build reproducible packages.
- Publish packages into a local pacman repository.

## Package Lifecycle

Each package progresses through an explicit set of states. This state
machine (see `architecture.md` / `workflow.md`) drives the dashboard and
determines which actions are available at any point:

```text
+Review Pending → Approved → Merged →
+[Build Pending] → Built → Confirmed → Published → Installed
```
 
> `Unknown` and `Fetched` are omitted — not independently reachable in
> the current implementation. Import creates a `Review` record
> synchronously (`workflow.md` § Import, #156), so a package's first
> observable state is always `Review Pending`. The only stages a user
> actually observes between `Merged` and `Built` are those two — there is
> no intermediate observable lifecycle state. `Merged` is the last state
> confirmed implemented; `Built` onward remains target design pending
> Sprint 7/8. See `lifecycle-state-reconciliation.md` for the full audit
> this revision is based on.

The simplified end-to-end flow is:

AUR → Fetch → Review → Approve/Reject → Merge → Build → Publish → Install

## Workspace

```text
~/repos/aur/
    package1/
    package2/
```

Each repository has two remotes:

- `origin` → personal Gitea repository (trusted source builds are made from)
- `upstream` → AUR (treated as a source of package recipes, never built from directly)

## Features

### Dashboard

- package
- installed version
- upstream version
- commits behind
- lifecycle state
- build status
- repository status
- risk indicator

### Fetch

Fetch all upstream repositories.

### Review

Show:

- commit log
- PKGBUILD diff
- `.SRCINFO` diff
- `.install` diff
- patch diff

Highlight risk findings, grouped by severity (see `risk-analysis.md`):

#### High risk

- removed checksum verification
- removed PGP verification
- new download locations
- shell execution
- network downloads

#### Medium risk

- maintainer change
- new dependencies
- added patches

#### Low risk

- version bump
- checksum refresh
- documentation updates

Findings are produced by a set of pluggable analyzers (PKGBUILD, `.install`,
URL, checksum, shell, Git history, metadata) coordinated by an Analyzer
Manager, and classified into severities by a Policy Engine. This keeps "what
was found" separate from "how important it is," and allows future analyzer
backends (e.g. `traur`) to be added without changing the review workflow.
See `interfaces.md` for the architectural contracts.

### Merge

Manual approval only.

### Build

Initially:

- makepkg

Later:

- clean chroot

### Publish

Create/update a local pacman repository using:

```text
repo-add custom.db.tar.gz *.pkg.tar.zst
```

### Install

Install via pacman from the local repository.

## Why a Local pacman Repository?

Even for only 10-12 packages it provides:

- repeatable installs
- version history
- rollback capability
- one installation mechanism
- easy deployment to additional Arch machines
- separation of review from installation

## Configuration

JSONC (see `docs/domain/workspace-config-spec.md` for the full field
list, mandatory/optional rules, and `docs/examples/config.jsonc` for a
complete annotated sample — this is a minimal illustrative example of
just the two mandatory fields):
 
complete annotated sample — shown below are all five mandatory fields):

```jsonc
{
  "rootPath": "~/src/aur",
  "repositoryPath": "/srv/pacrepo",
  "giteaBaseURL": "https://gitea.example.com",
  "giteaOrg": "aur",
  "giteaToken": "REPLACE_ME_OR_USE_AUR_CURATOR_GITEA_TOKEN_ENV_VAR"
}
```
 

### Gitea (for Import)

Import (see `workflow.md` § Import) creates repositories on a personal
Gitea instance via its API. `giteaBaseURL` and `giteaOrg` are mandatory —
`config.Load` errors if either is absent (`domain-package-review.md`
DOM-015).

`giteaToken` is mandatory in a precedence-aware sense: prefer the
`AUR_CURATOR_GITEA_TOKEN` environment variable over the `giteaToken` file
key — the environment variable always takes precedence if set, so a
token never has to touch disk (`domain.md` § Secret Handling: GiteaToken).
`config.Load` errors only if neither source supplies a value.

**Resolved:** token storage mechanism is environment variable (preferred)
or the config file (fallback) — OS keyring was not adopted. This closes
what was previously an open item in this document; see `domain.md` §
Secret Handling: GiteaToken for the full policy.

## UI

Bubble Tea

Views:

- Dashboard
- Package Details
- Diff Viewer
- Build Log
- Repository Status

## Out of Scope (MVP)

- Package signing
- Clean chroot / remote / containerized builders
- Multiple repositories
- Notifications
- CVE lookups
- HTML audit reports

These are tracked as future enhancements (see `roadmap.md`) and should
integrate as additional components without changing the core review-centric
workflow.

## Future

- package signing
- clean chroot
- remote builder
- notifications
- multiple repositories

## Risk Mitigation Layers

The risk categories above (High/Medium/Low) describe what static review — human inspection and Analyzer findings — can surface **before** a build runs. This is necessarily incomplete:

- Static review examines the PKGBUILD, `.install`, and related files as text. It cannot see behavior introduced transitively through the dependency chain at build time — e.g. an `npm`/`cargo`/`go` build script pulled in by a declared dependency, rather than the top-level PKGBUILD itself.
- A reviewed and Approved package can still exfiltrate data during `Build Pending → Built` if the review missed something, or if the risk was never visible as text in the first place.

Build-time sandboxing (see `architecture.md` isolation backends) is therefore not a replacement for review — it does not inform the Decision, and findings from it are not fed back into the Review workflow. It is an independent second layer: bounding what a build can *do* to the host, regardless of whether the Decision to approve it was correct.

**Explicit limitation:** filesystem isolation is not network isolation. Even with build-time network allowlisting (Phase 3), a build with any permitted network access can still probe or attack other reachable hosts. Sandboxing bounds damage to the local filesystem and secrets; it does not bound lateral network movement.
