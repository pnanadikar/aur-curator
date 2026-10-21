# Roadmap

> Last updated: 2026-07-20
> Status: Active
> Companion docs: prd.md

This document captures planned enhancements, implementation phases, and external projects worth studying.

## Guiding Principle

> Automate mechanics. Never automate trust.

Features must strengthen the review workflow rather than bypass it.

---

## Phase 1 — MVP

- Dashboard
- Fetch upstream repositories
- Review commits and diffs
- Risk highlighting
- Manual approval
- Build using `makepkg`
- Publish to local pacman repository
- Install via `pacman`

---

## Phase 2 — Better Builds

- Builder interface
- Clean chroot backend
- Build history
- Build log viewer

---

## Phase 3 — Advanced Builders

- Remote builder
- Multiple builder backends
- Package signing
- Build-time network isolation — thin wrapper around `arch-nspawn`'s
  `--private-network`, scoped to the `makepkg` build step only (source
  retrieval happens before the chroot is prepared, so it's unaffected).
  Requires Policy Engine support for build-time network allowlisting
  (Go modules, Cargo, npm, etc.) — see `interfaces.md`. Not a new
  isolation mechanism or builder backend.

---

## Phase 4 — Repository Management

- Multiple repositories
- Repository health
- HTML audit reports
- Notifications
- CVE lookups

---

## External References

### aurch

Repository:

<https://github.com/Cody-Learner/aurch>

### Relevant ideas

- Local pacman repository publishing.
- Persistent `systemd-nspawn` build environment.
- Inspiration for future Builder implementations.

### Not adopted

- Build-centric workflow.
- Automatic update/build model.
- Overall application architecture.

aur-curator remains review-centric. The reviewed Git repository is the primary artifact.

### Potential Builder backends

- makepkg
- devtools clean chroot
- systemd-nspawn
- Podman / Docker
- Remote builder

The UI should depend only on a Builder interface.

## Sohimaster/traur

**Repository:** <https://github.com/Sohimaster/traur>

### Why it's interesting

`traur` is a security-focused AUR package analyzer. Unlike traditional AUR helpers, it performs static analysis of PKGBUILDs and related files to identify potentially dangerous changes.

Areas worth studying:

- Pluggable analysis architecture.
- PKGBUILD, `.install` and source URL analysis.
- Shell script inspection beyond simple pattern matching.
- Git history and maintainer analysis.
- Verification of binary package sources (`-bin` packages).

### Potential adoption

Rather than integrating `traur` as an ALPM hook, consider using it as an optional analysis backend during the **Review** stage.

```text
Fetch
   ↓
Run analyzers (internal and/or traur)
   ↓
Present findings
   ↓
Human review
   ↓
Approve / Reject
```

The preferred UI is an explainable list of findings rather than a single trust score.

### Status

**Deferred.** Revisit after the core review workflow and internal analyzer framework are implemented.

## Recommendations

- Use normal clones, not `git clone --mirror`.
- Keep `master` as the branch name to match the AUR.
- Keep `origin` as your Gitea repository and `upstream` as the AUR.
- Prefer this workflow for packages you trust and use regularly.
