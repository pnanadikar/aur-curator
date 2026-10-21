# Risk Rules

> Last updated: 2026-07-20
> Status: Active
> Companion docs: domain.md, interfaces.md

## High Risk

- removed checksum verification
- removed PGP verification
- new download locations
- shell execution
- network downloads

## Medium Risk

- maintainer change
- new dependencies
- added patches

No analyzer detects these categories yet; Medium is currently unreachable (#23). 
A future analyzer’s categories must be added to defaultSeverity in internal/policy/engine.go 
explicitly. Unmapped categories fall back to Low.

## Low Risk

- version bump
- checksum refresh
- documentation updates
