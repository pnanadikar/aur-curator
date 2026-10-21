# Architecture: aur-curator

> Last updated: 2026-10-10
> Status: Active
> Companion docs: domain.md, interfaces.md
> This document describes the architectural principles of aur-curator. It intentionally avoids implementation details that may change over time.

---

## Overview

aur-curator is a Bubble Tea application that manages the lifecycle of a curated collection of AUR packages.

Its primary responsibilities are to:

- fetch upstream package updates
- assist the user in reviewing changes
- record review decisions
- build packages
- publish them into a local pacman repository

The application deliberately separates **mechanical operations** from **trust decisions**.

---

## System Architecture

```text
                  AUR
                   │
            git fetch/pull
                   │
                   ▼
          Local Git Repository
                   │
        (authoritative repository state)
                   │
      ┌────────────┴────────────┐
      │                         │
      ▼                         ▼
 Review Workflow          Build Pipeline
      │                         │
      ▼                         ▼
 Review Database         Package Artifacts
      │                         │
      └────────────┬────────────┘
                   ▼
          Local pacman Repository
                   │
                   ▼
                pacman
```

The UI orchestrates these components but does not become the source of truth for repository state.

## Repository Model

Each curated package is represented by a standard Git repository.

Every repository has exactly two remotes:
origin
    Personal Gitea repository

upstream
    Official AUR repository

The personal repository is considered the trusted source from which packages
are built.

The AUR repository is treated as an upstream source of package recipes.

The application never builds directly from the upstream remote.

## Repository Layout

Gitea organization:

``` text
aur/
    tor-browser-bin
    duplicati-bin
    units
    hplip-plugin
```

Local workspace:

``` text
~/repos/aur/
    tor-browser-bin/
    duplicati-bin/
    units/
```

Each package is a normal Git repository (not a bare mirror).

---

## Guiding Principles

### Automate Mechanics, Never Trust

aur-curator automates repetitive tasks such as fetching repositories, displaying diffs, building packages and publishing repositories.

Security decisions always require explicit user approval.

The application never:

- merge without an explicit Approve (the optional autoMerge setting only merges after an Approve you made)
- auto-build after review
- install or uninstall packages — pacman does both, from the local
  pacman repository (`workflow.md` § Installation)
- silently accept upstream changes

---

### Git Owns Repository State

Git is the authoritative source for repository information.

The application should derive information from Git whenever possible instead of duplicating it.

Examples of derived information include:

- commits behind upstream
- merge status
- changed files
- commit history
- branch relationships
- repository cleanliness

The application should query Git rather than maintain copies of this information.

---

### Store Only Irreducible State

Application state should be limited to information that cannot be recomputed.

Good examples include:

- review decisions
- reviewer notes
- build history
- publication history
- user preferences

Poor examples include:

- commits behind upstream
- merge status
- modified files

Those values already exist in Git and should be derived on demand.

---

### Git is an Audit Trail

Each package repository forms a permanent history of the review process.

The Git history should answer questions such as:

- Which version was reviewed?
- What changed?
- Which commit produced a package?
- Can the package be rebuilt?

Whenever practical, existing Git history should be preferred over additional application metadata.

---

### State is Derived Whenever Possible

The dashboard should compute its status from underlying sources instead of loading stored status flags.

For example:

```text
Git
 ↓
Repository status

Review database
 ↓
Review status

Build database
 ↓
Build status

Repository publisher
 ↓
Publication status
```

The UI becomes a projection of current system state rather than a database of status values.

---

### Explicit Workflow

Each package progresses through a clearly defined lifecycle.

```text
Review Pending → Approved → Merged → Built → Confirmed → Published
      └──────→ Rejected
```

Published is the final state; installation is done with pacman from the
local repository.

Import lands a package directly in Review Pending, because it creates the
review itself (`workflow.md` § Import). Building has no state of its own
between Merged and Built: build progress is shown in the Operations
Panel. `domain.md` § Package → Lifecycle defines each state and how it is
derived.

The lifecycle guides the user toward the next required action.

---

### Separation of Responsibilities

| Component | Responsibility |
| ----------- | ---------------- |
| Git | Repository state |
| Review engine | Present changes for human inspection |
| Risk analyzer | Highlight potentially dangerous changes |
| Builder | Produce packages |
| Repository publisher | Maintain local pacman repository |
| Bubble Tea UI | Present information and orchestrate workflows |

Each component should have a clearly defined interface.

---

### Thin UI

The Bubble Tea application should coordinate operations rather than implement them.

Business logic belongs in reusable packages.

The UI should primarily:

- display information
- collect user input
- dispatch actions
- present progress
- report results

This keeps the application testable and makes future front ends possible.

---

### Bounded, Derived Rendering

Any pane or tab that can hold variable-length or potentially multi-page
content must render through a bounded, scrollable viewport — never as a
plain string concatenated directly into a screen's layout.

This applies **per content area**, not per screen: a screen may have some
sections that are safely fixed-height (a header, a single summary line)
alongside others that need a viewport (a diff, a findings list, a log).
Each is judged on its own content shape.

A screen's rendered layout — the set of sections making up what the user
sees — must be built from a single source of truth shared between
rendering and any size/height calculation that depends on it. A layout
reconstructed independently in two places may agree by coincidence at
the time it's written, but nothing prevents the two copies from silently
diverging later, at which point sizing calculations are no longer sized
against what's actually rendered.

This principle exists because of a real defect: a screen's height
calculation was fixed for one scrollable pane while a sibling pane on
the same screen — rendering equally variable-length content through a
plain string with no bound — reproduced the identical rendering failure,
undetected until content grew long enough to trigger it. See
`developer-guide.md` § Implementation Conventions & Gotchas
("Scrollable-content convention", "Single source of truth for section
layout") for the implementation-level rules this drives.

---

## Persistence

Persistent application data should remain intentionally small.

Typical persisted data includes:

- review history
- build history
- configuration
- cached metadata

Repository state should continue to reside in Git.

---
## Development Philosophy

Default to breadth-first, stub-honest sequencing: build outward in the order a user encounters functionality, 
with every reachable path honest about its state (real, or clearly marked not-yet-implemented) before any one 
path goes deep. Unlike the hard rules above, this is a sequencing preference, not a safety property — deviate 
from it when a specific case genuinely calls for going deep first.

## Future Evolution

The architecture is intended to accommodate future enhancements without changing the core workflow.

Examples include:

- clean chroot builds
- remote builders
- package signing
- multiple repositories
- distributed build workers
- HTML audit reports

These should integrate as additional components rather than requiring changes to the fundamental architecture.
