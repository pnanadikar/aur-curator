# Glossary

This document defines the primary architectural vocabulary used throughout AUR Curator. The terms here are conceptual rather than implementation-specific.

---

## Workspace


A Workspace encompasses:

- the directory hierarchy containing the local Git repositories for curated packages;
- the application's persisted state, including reviews, build history, configuration, and other metadata; and
- the configured binary pacman repository to which approved packages are published.

The binary pacman repository is an external resource referenced by the Workspace rather than owned by it.
All packages, reviews, builds, and configuration belong to exactly one Workspace.

See: `domain.md`

---

## Package

An AUR package under curation.
A Package is the primary object that progresses through the review, build and publication workflow.

---

## GitRepository

The Git repository associated with a Package, including the local repository and its configured remotes.
Represents repository identity and long-lived configuration. Operational repository state remains authoritative in Git.

---

## Git Provider

Application service responsible for performing Git operations required by the workflow.
Examples include Import, Fetch, Diff, Merge, Status and Metadata.

---

## Review

The current human review of a specific upstream repository state.
Only the current review is retained.

---

## Finding

An observation produced by an analyzer.
Findings are inputs to policy evaluation and are never reviewer decisions.

---

## RiskAssessment

The policy engine's evaluation of a collection of Findings.
Represents an objective assessment rather than a human decision.

---

## Build

A build execution corresponding to an approved reviewed commit.

---

## Artifact

Represents the binary package produced by a successful build that can be published to the PackageRepository and installed using pacman.

---

## PackageRepository

A binary pacman repository into which AUR Curator publishes approved packages.
The PackageRepository is an external resource referenced by the Workspace from which pacman installs published packages.
See: `domain.md`

---

## PolicyConfig

Workspace configuration controlling policy evaluation.

---

## Analyzer

A component that examines repository changes and produces Findings.

---

## Policy Engine

A component that evaluates Findings and produces a RiskAssessment.
