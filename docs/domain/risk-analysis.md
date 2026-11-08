# Risk Analysis

> Last updated: 2026-11-06
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
| `network_download` | High | Shell analyzer | network access from `PKGBUILD` or `.install` code rather than through `source=()` |
| `array_unreadable` | High | Pkgbuild analyzer | an array the analyzers read (`source`, checksums, `validpgpkeys`, dependencies) could not be parsed, so no check covered it (#484) |
| `install_changed` | Medium | Shell analyzer | a `.install` scriptlet added or changed (it runs as root at install time) |
| `pgp_key_changed` | Medium | Checksum analyzer | a key added to `validpgpkeys`, with or without one removed |
| `depends_added` | Medium | Depends analyzer | an entry added to `depends`, `makedepends`, `checkdepends` or `optdepends` |
| `checksum_refresh` | Low | Checksum analyzer | checksums changed with the sources |
| `version_bump` | Low | Checksum analyzer | `pkgver`/`pkgrel` changed |

`version_bump` comes from the Checksum analyzer because it already parses
the `PKGBUILD` variables it needs; there is no separate metadata analyzer.

The default table lives in `internal/policy/engine.go` (`defaultSeverity`)
and is versioned with the application, not persisted. User overrides
remap a category's severity; none are persisted yet, so the defaults
always apply today (`domain.md` § PolicyConfig).

### Unmapped categories

Analyzers emit only categories written in their source: the exported
constants in `internal/analyzer/categories.go`, listed in
`analyzer.Categories`. A test fails if any of them is missing from
`defaultSeverity`, and another if an analyzer gives a category as a
string literal instead of a constant. A new category is added to the
constants, to `defaultSeverity` and to the table above in the same
change.

A category missing from the table is therefore an internal
inconsistency: a bug that got past the tests, or a finding stored by a
version that had a category this one no longer has. It resolves to
**High**, never Low, so it is never ranked as routine; the finding is
never dropped. The first time a given category is seen in a run, the
error log gets a line naming it. The Review screen shows a modal, and the
Findings tab a standing message, asking the user to file a bug report
(`tui-review.md` § Internal Inconsistency) (#475).

**Retiring a category.** Findings are stored in review records with
their category, and severity is resolved when they are shown. A category
that has been in a release is therefore part of the stored format:
removing or renaming it needs a migration that rewrites stored findings
to the replacement category (a format change with a migration, so a
minor release). Removing it without one makes every stored finding of
that category an internal inconsistency.

## Risks Without a Category Yet

These are recognised risks that no analyzer detects. They get categories
only when an analyzer is written for them.

| Risk | Intended severity |
| --- | --- |
| maintainer change | Medium |
| added patches | Medium |
| documentation-only changes | Low |

New dependencies and `.install` changes have categories since #476
(§ Categories), which made Medium reachable (#23).

## Severity Over Time

Until the decision, a finding's severity is resolved **live** from its
category each time it is shown, so a policy change is reflected
immediately. When Approve or Reject is recorded after analyzers have run,
Policy Engine rolls the findings up into a `RiskAssessment` (highest
severity, count per severity, policy version) that is stored with the
review and never recomputed (`domain.md` § RiskAssessment).
