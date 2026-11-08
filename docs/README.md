# Documentation Structure: aur-curator

For building and contributing, start with [`DEVELOPMENT.md`](../DEVELOPMENT.md). 
For using aur-curator, see the [README](../README.md).

> Last updated: 2026-10-31
> Status: Active

How the docs in this project are organized, and which of them are a source
of truth.

## Canonical and History

**Canonical** docs are hand-authored, cross-referenced and the source of
truth for behaviour: code is expected to match them, not the other way
round. If code and a canonical doc disagree, that is a bug in one of them;
which one is decided explicitly, never silently. A change to documented
behaviour changes the canonical docs first, reconciled across every doc
it affects: as the first commit on the issue branch, reviewed before any
code, and merged to `main` together with the code, so every merge leaves
docs and code in agreement and `main` releasable.

**History** docs record how the project got here: plans, packets, prompts,
reviews and superseded designs. They are kept for reference, are not a
source of truth, and nothing is implemented from them. They are excluded
from the public GitHub mirror.

Issues, ADRs and the backlog live in Gitea (ADRs are `type/adr` issues,
cited by number as `#NNN`), not in `docs/`.

## Canonical Documents

| Folder | Contents | What it defines |
| --- | --- | --- |
| `domain/` | `domain.md`, `architecture.md`, `interfaces.md`, `workflow.md`, `analyzer-specs.md`, `risk-analysis.md`, `threat-model.md` | The domain and architectural contract: entities, lifecycle, workflows, analyzer behaviour, interface responsibilities, and the threats aur-curator defends against |
| `app-contract/` | `developer-guide.md` | How the architecture is realised in the Bubble Tea application: composition root, dependency wiring, operations queue, message flow, implementation rules |
| `ux/` | `ui-overview.md`, `ui-implementation.md`, `ui-review.md`, `tui-dashboard-wireframe.md`, `tui-package-details.md`, `tui-review.md` | UI information architecture, implementation and screen-level wireframes |
| `product/` | `prd.md`, `roadmap.md` | Vision, scope, phased plan |
| (top level) | `glossary.md`, `design-trace-logging.md` | Vocabulary; the trace-logging design |

When canonical docs conflict: `workflow.md` → `domain.md` →
`interfaces.md` → `architecture.md` → `developer-guide.md` → the rest.

## History

| Path | Contents |
| --- | --- |
| `plans/` | One plan per step (approach, steps, tests), written before code: the spec for its issue while the issue is open, history once it is closed |
| `sprints/` | The original sprint plan and per-sprint specs (superseded by Gitea issues and milestones) |
| `screen-packets/`, `interface-packets/` | Self-contained implementation specs written for a local model during the initial build |
| `prompts/` | Work packets and prompts given to coding agents |
| `reviews/` | Review findings against the docs and code at the time |
| `archive/` | Superseded designs (e.g. `design.md`) |
| `backlog.md` | The pre-Gitea backlog |

`code-review-checklist.md` is a review procedure, not a spec; whether it
is still current is undecided.

## Note

`AGENTS.md` at the repo root holds the instructions for AI coding agents.
It is an operational file, not part of this taxonomy, and is not
published.
