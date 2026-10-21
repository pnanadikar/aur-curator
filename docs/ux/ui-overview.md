
# UI Overview

> Last updated: 2026-07-28
> Status: Active
> Companion docs: domain.md, tui-dashboard-wireframe.md, tui-package-details.md, tui-review.md

## Purpose

This document defines the information architecture of **aur-curator**. It describes **what** the user interface presents and how it maps to the domain model. It deliberately avoids implementation details.

## Design Principles

- Dashboard-first workflow.
- Domain-centric views.
- Derived state is preferred over stored state.
- Progressive disclosure.
- Long-running operations execute asynchronously with visible progress.

## Navigation

```text
Dashboard
 ├── Package Details
 │     ├── Review
 │     │      ├── Summary
 │     │      ├── Commit History
 │     │      ├── PKGBUILD Diff
 │     │      ├── .SRCINFO Diff
 │     │      ├── .install Diff
 │     │      ├── Patch Diff
 │     │      ├── Findings
 │     │      └── Decision
 │     └── Build Log
 ├── Repository Status
 └── Settings
```

---

## Screen Responsibilities

### Dashboard

Displays package summaries.

Owns

- package table
- filter controls

May navigate to

- Package Details

---

## Dashboard

### Purpose

Answers: **What requires my attention?**

### Reads

#### Primary domain entities

- Workspace
- Package

#### Derived fields

- lifecycleState
- installedVersion
- upstreamVersion
- commitsBehind
- mergeStatus
- repository cleanliness
- highest finding severity
- latest build status
- published status

### Displays

- package table
- package summary
- operations queue
- status bar

### Domain actions

- Open package
- Build approved package(s)
- Confirm built package(s)
- Publish confirmed package(s)
- Install published package(s)

### Infrastructure actions

- Fetch upstream
- Refresh state
- Re-run analyzers

### Navigation

Package Details, Repository Status, Settings

### State transitions

Fetched → Review Pending

Approved → Build Pending

Built → Confirmed

Confirmed → Published

Published → Installed

---

## Package Details

### Purpose

Answers: **What is the current state of this package?**

### Reads

Package, GitRepository, Review, Build, Artifact

Derived:

- lifecycleState
- commitsBehind
- cleanliness
- mergeStatus
- publishedArtifact

### Displays

- metadata
- commit summary
- review summary
- latest build
- artifact history

### Domain actions

- Start review
- Merge approved review
- Build
- Confirm
- Publish
- Install

### Infrastructure actions

- Fetch
- Refresh Git state

### Navigation

Dashboard, Review, Build Log

---

## Review

### Purpose

Answers: **Should these upstream changes be trusted?**

### Reads

Review, Findings, RiskAssessment

Review.status, Review.decisionTime, and Review.rationale carry the trust
decision directly — there is no separate Decision entity (see
`domain.md`).

Derived:

- review status
- highest severity
- affected files

### Displays

- comparison summary
- tab bar
- diff viewer
- findings summary
- reviewer notes
- action bar

### Subviews

- Summary
- Commit History
- PKGBUILD Diff
- .SRCINFO Diff
- .install Diff
- Patch Diff
- Findings
- Decision

### Domain actions

- Approve review
- Reject review
- Record notes

### Infrastructure actions

- Re-run analyzers
- Refresh diff

### Navigation

Package Details

### State transitions

Pending → Approved

Pending → Rejected

Approved → Package lifecycle becomes Approved

---

## Build Log

### Purpose

Answers: **How did this build execute?**

### Reads

Build, Artifact

Derived:

- elapsed time
- artifact publication state

- build progress
- build log
- artifact summary

### Domain actions

None.

### Infrastructure actions

- Start build
- Cancel build (if supported)

### Navigation

Package Details

### State transitions

Build Pending → Running → Success/Failed

---

## Repository Status

### Purpose

Answers: **What is the state of the local package repository?**

### Reads

PackageRepository, Artifact

Derived:

- package count
- disk usage
- published artifacts
- repository integrity
- last repo update

- repository summary
- integrity status
- publication status

### Domain actions

None.

### Infrastructure actions

- Publish artifacts
- Verify repository
- Refresh

### Navigation

Dashboard

---

## Settings (Future)

### Purpose

Configure the workspace.

### Reads

Workspace
Policy Engine

### Domain actions

Update configuration.

### Infrastructure actions

Reload configuration.

### Navigation

Dashboard

---

### View-to-Domain Mapping

| View | Primary entities |
| ------ | ------------------ |
| Dashboard | Workspace, Package |
| Package Details | Package, GitRepository, Review, Build |
| Review | Review, Finding, RiskAssessment |
| Build Log | Build, Artifact |
| Repository Status | PackageRepository, Artifact |
| Settings | Workspace, Policy Engine |
