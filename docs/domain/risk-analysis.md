# Risk Analysis

> Last updated: 2026-10-22
> Status: Active
> Companion docs: domain.md, interfaces.md, analyzer-specs.md

This document defines the severity tiers and the finding categories that
map to them. How each category is detected is in `analyzer-specs.md`; how
findings, severity and the risk snapshot are stored is in `domain.md`
§ Finding and § RiskAssessment.

Risk is **highlighted, never blocking**. Severity tells the reviewer where
to look first; it never prevents Approve. The trust decision stays with the
human ("Automate mechanics. Never automate trust.").

## Severity Tiers

| Severity | Meaning |
| --- | --- |
| High | The change can weaken verification or run or fetch code the reviewer has not seen — inspect before approving |
| Medium | A change in who or what the package trusts, worth a look but not inherently dangerous |
| Low | Routine maintenance |

## Categories

Every finding carries exactly one category. Severity is **not** chosen by
the analyzer: Policy Engine resolves it from the category through the
default table below, overlaid with any user overrides (`PolicyConfig`).
Because one category maps to exactly one severity, a category must be
narrow enough that it never legitimately belongs to two tiers.

| Category | Default severity | Produced by | Risk |
| --- | --- | --- | --- |
| `url_new` | High | URL analyzer | new download location in `source=()` |
| `pgp_removed` | High | Checksum analyzer | PGP verification removed |
| `checksum_removed` | High | Checksum analyzer | checksum verification removed or weakened (`SKIP` where a real checksum existed) |
| `shell_exec` | High | Shell analyzer | new shell execution in `PKGBUILD` or `.install` |
| `network_download` | High | Shell analyzer | network access during `build()`/`package()` rather than through `source=()` |
| `checksum_refresh` | Low | Checksum analyzer | checksums changed with the sources |
| `version_bump` | Low | Checksum analyzer | `pkgver`/`pkgrel` changed |

`version_bump` comes from the Checksum analyzer because it already parses
the `PKGBUILD` variables it needs; there is no separate metadata analyzer.

The default table lives in `internal/policy/engine.go` (`defaultSeverity`)
and is versioned with the application, not persisted. User overrides
remap a category's severity; none are persisted yet, so the defaults
always apply today (`domain.md` § PolicyConfig).

### Unmapped categories

A category missing from the default table resolves to **Low** — not an
error, and the finding is never dropped: it still reaches the reviewer,
just without a severity there is evidence for. A new analyzer's
categories must therefore be added to `defaultSeverity` and to the table
above in the same change; otherwise they silently rank as Low.

## Risks Without a Category Yet

These are recognised risks that no analyzer detects. They get categories
only when an analyzer is written for them.

| Risk | Intended severity |
| --- | --- |
| maintainer change | Medium |
| new dependencies | Medium |
| added patches | Medium |
| install hooks added (`.install`) | not decided |
| documentation-only changes | Low |

No analyzer produces a Medium category today, so Medium is currently
unreachable (#23).

## Severity Over Time

Until the decision, a finding's severity is resolved **live** from its
category each time it is shown, so a policy change is reflected
immediately. When Approve or Reject is recorded after analyzers have run,
Policy Engine rolls the findings up into a `RiskAssessment` (highest
severity, count per severity, policy version) that is stored with the
review and never recomputed (`domain.md` § RiskAssessment).
