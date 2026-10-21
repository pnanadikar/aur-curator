# Documentation Structure: aur-curator

For building and contributing, start with [`DEVELOPMENT.md`](../DEVELOPMENT.md). 
For using aur-curator, see the [README](../README.md).

> Last updated: 2026-10-08
> Status: Active

This describes how the docs in this project are organized and why, since
the doc set is intended to feed both human review and AI coding assistants
(a heavy-duty proprietary model for cross-document architecture work, and
a resource-limited local model for narrow implementation tasks).

## Canonical vs. Derived

Docs fall into two kinds. This distinction is a convention, not a folder —
group membership below tells you which kind a doc is.

**Canonical** — hand-authored, cross-referenced, source of truth. Open
questions live here. May say "see domain.md §X" freely. Edited directly,
one document at a time, with changes reconciled across all affected files.

**Derived** — generated from canonical docs, self-contained (no
cross-references — everything a consumer needs is inlined), narrow scope.
Never hand-edited directly; regenerated when the canonical docs they draw
from change. This mirrors the app's own "State is Derived Whenever
Possible" principle (architecture.md) — derived docs are a regeneratable
cache, not a duplicate source of truth.

## Foundation Documents

The following canonical documents together define the project's
architectural foundation. They establish the concepts, contracts and
behaviour that the implementation is expected to conform to.

- `domain/domain.md`
- `domain/architecture.md`
- `domain/interfaces.md`
- `domain/workflow.md`
- `domain/analyzer-specs.md`
- `domain/risk-analysis.md`
- `domain/go-domain-structs.md`
- `ux/ui-overview.md`
- `ux/ui-implementation.md`
- `glossary.md`

The remaining canonical documents build on this foundation:

- **Product** documents define scope and roadmap.
- **Application Contract** documents describe how the architecture is
  realised in the Bubble Tea application.
- **Sprint** documents define the implementation plan.

## Documentation Taxonomy

| Folder | Kind | Contents | Audience |
| --- | --- | --- | --- |
| `product/` | Canonical | `prd.md`, `roadmap.md` | Vision, scope, phased plan |
| `domain/` | Canonical | `domain.md`, `architecture.md`, `interfaces.md`, `workflow.md`, `analyzer-specs.md`, `risk-analysis.md`, `go-domain-structs.md` | The domain and architectural contract — entities, lifecycle, workflows, analyzer behaviour, interface responsibilities and Go struct mapping |
| `app-contract/` | Canonical | `composition-root.md`, `developer-guide.md` | Application composition and Bubble Tea implementation architecture (composition root, dependency wiring, operations queue, message flow) |
| `ux/` | Canonical | `ui-overview.md`, `ui-implementation.md`, `tui-dashboard-wireframe.md`, `tui-package-details.md`, `tui-review.md` | UI information architecture, implementation architecture and screen-level wireframes |
| `archive/` | — | Superseded docs, kept for history (e.g. `design.md`) | Not authoritative; do not implement from these |
| `screen-packets/` | Derived | One file per UI screen | Self-contained implementation spec for a local model implementing one screen |
| `interface-packets/` | Derived | One file per Go interface (`GitProvider`, `ReviewEngine`, `Builder`, `Publisher`, etc.) | Self-contained implementation spec for a local model implementing one interface |
| `sprints/` | Canonical | `sprint-plan.md`, `s01-slice.md` … `s09-ship.md` | Per-sprint implementation plan — goal, inputs, outputs, acceptance criteria, and the verbatim prompt for the coding assistant driving that sprint |

## Note

> `AGENTS.md` lives at the repo root (not under `docs/`) and is read
> automatically by Codex — it's an operational tool-input file, not part
> of this documentation taxonomy.

## Rules

- Canonical docs are edited directly, with cross-document reconciliation
  when a change affects more than one file.
- Derived packets are never hand-edited. If a packet looks wrong, fix the
  canonical doc it draws from and regenerate the packet.
- Verification documents record review findings against the canonical
  documentation. If a review identifies a discrepancy, update the
  canonical documentation (if the design is wrong) or the implementation
  (if the implementation is wrong); reviews themselves do not become
  sources of truth.
- Archived docs are kept for history only and marked superseded at the
  top of the file — they are never a source of truth and nothing should
  be implemented from them.
