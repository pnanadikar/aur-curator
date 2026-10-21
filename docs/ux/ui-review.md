# Review Screen Implementation Guide

> Last updated: 2026-10-01
> Status: Active
> Companion docs: tui-review.md (visual intent), developer-guide.md, workflow.md
>
> Implementation guide for the Review screen. `tui-review.md` defines the
> visual intent; where the two disagree, this document takes precedence.
> Rewritten 2026-10-01 against the code as of #296 — the previous version
> predated the Tab/focus correction (2026-08-23), #22 (notes moved into the
> Approve modal) and #296.

## Purpose

Implements the Review screen: the comparison of local `master` against
`upstream/master` for one package, analyzer findings, and the Approve/Reject
decision. Model: `screens.ReviewScreen` (`internal/ui/screens/review_screen.go`,
modal flows in `review_modals.go`). It is a top-level screen reached from
Package Details via `NavigateMsg`, not a Package Details tab.

---

## Widgets

- `widgets.TabBar` — Diff, .SRCINFO, .install, Patches, Findings. Disabled
  tabs are dimmed and skipped. The Findings label carries the
  `findingsSummary()` text.
- `widgets.DiffPane` — bounded, scrollable diff view for the Diff-family tabs.
- `widgets.ContentPane` — bounded, scrollable view for the Findings tab.
- `widgets.ConfirmModal` — the Approve and Reject confirmation flows,
  rendered as a centered overlay.
- Action bar — rendered by `renderActionBar()`.

There is no text input on the screen itself. Reviewer notes are entered in
the Approve modal.

Root-owned (not this screen): status bar, toasts, navigation.

---

## State

Owns:

- active tab (`tabs`), `diffPane`, `findingsPane`
- loaded data: `diff`, `review`
- `loading`, `err`, `opFailure` (last failed operation, shown as a banner)
- `changed` — whether a decision or analysis changed the review; returned
  in `BackMsg` / `ReviewDecisionBackMsg` so callers know to reload
- `modal` and `modalFlow` — the open confirmation flow, if any
- `traceCtx` — correlation context for the current approve/reject path

Does not own:

- navigation (emits `BackMsg` / `ReviewDecisionBackMsg`; `App` navigates)
- the Operations Queue (emits `AnalyzeRequestedMsg` / `MergeRequestedMsg`;
  `App` queues them)
- toasts (emits `ToastMsg`)
- review persistence rules (delegated to `review.Engine.RecordDecision`)

There is no focus state. The active tab's content pane is the only focus
target, so there is nothing to track.

---

## Init()

`loadReviewCmd()`: ensures a Review exists for the package and loads the
current diff via `git.Provider.Diff`, returning `reviewLoadedMsg`. It does
not fetch from the network or overwrite an existing Review; that is the
explicit Fetch action's job.

---

## Update()

Handled in this order:

1. **`tea.WindowSizeMsg`** — store size, `syncLayout()`, resize any open modal.
2. **`reviewLoadedMsg`** — `applyLoadedReview()`: populate diff, review and
   findings content, then `syncLayout()`.
3. **`reviewSavedMsg`** — the decision was persisted (or failed):
   - success: toast ("Package change approved" / "...rejected"), then
     `ReviewDecisionBackMsg`; if Approved and `WorkspaceConfig.AutoMerge`,
     also `MergeRequestedMsg`. Sent with `tea.Sequence`.
   - error: toast with the error, then `ReviewDecisionBackMsg`.
4. **`msgs.OperationCompletedMsg`** for this package — analyzers finished:
   store the returned Review, refresh findings content, and if
   `FindingsStatus == run`, switch to the Findings tab.
5. **`msgs.OperationFailedMsg`** for this package — record `opFailure`,
   shown as an error banner above the header.
6. **Modal open** — every remaining message goes to the modal first, and
   its result is dispatched (see Dialogs). No key reaches the screen while
   a modal is open.
7. **`loading`** — no keys are handled while loading.
8. **Keys** (`bubbles/key` bindings):

| Key | Condition | Action |
| --- | --- | --- |
| `Esc`, `Ctrl+C` | always | `BackMsg{Changed}` |
| `Ctrl+PgDn`, `Ctrl+→` | always | next enabled tab |
| `Ctrl+PgUp`, `Ctrl+←` | always | previous enabled tab |
| `a` | `Status == Pending` | `loading = true`, emit `AnalyzeRequestedMsg` |
| `Enter` | `Status == Pending` | open the Approve modal |
| `Ctrl+X` | `Status == Pending` | open the Reject confirm modal |
| `PgUp`, `PgDn` | always | scroll the active tab's pane |

`Tab`/`Shift+Tab` are reserved for focus-cycling and are deliberately
unbound here — never use them to switch tabs (`developer-guide.md`
§ Tab/Shift+Tab reserved for focus, not content-switching).

---

## View()

Built from `fixedPartsAboveContent()`, the active pane, and
`fixedPartsBelowContent()`. `syncLayout()` sizes the panes from the same
functions — one source of truth for layout (`developer-guide.md` § Single
source of truth for section layout).

```text
[opFailure banner, only if set]
Header              ‹ Package Details   <pkg> - Review   Status: <status>
Comparison Summary  Reviewing: master (<sha>) vs upstream/master (<sha>)  N commits, M files changed
Tab Bar             Diff  .SRCINFO  .install  Patches  Findings: <summary>
Content pane        DiffPane or ContentPane (bounded, scrollable)
Action Bar
```

With a modal open, the body renders underneath and the modal is overlaid
centered, so the review stays visible and is not pushed upward.

---

## Dialogs

Owned by `ReviewScreen` (`m.modal`, `m.modalFlow`), not the root model.

**Approve** (`modalApprove`) — opens on every Approve:

- First line by findings state: not run → "No analyzers have been run for
  this review. Approve anyway?"; run with 0 → "There are 0 findings,
  approve?"; run with N → the `findingsSummary()` line, then "Approve?".
  Always one line plus prompt: the modal cannot scroll.
- Input labelled "Reviewer notes (optional):", starting blank and focused.
- Modal kind: `ModalKindConfirmText`. Hint row: `[Enter] Confirm   [Esc] Cancel`.
- `Enter` → `RecordDecision(Approved)` with the trimmed input as
  `ReviewerNotes`. `Esc` → close, nothing recorded, typed text discarded.

**Reject** — two steps:

1. `modalRejectConfirm` (`ModalKindChoice`, buttons `[Y] [n]`):
   "Reject this review?". `y` → step 2. `n`/`Esc` → close, nothing
   recorded. `←`/`→`/`Tab`/`Shift+Tab` move button focus; `Enter`
   activates the focused button (`[Y]` by default).
2. `modalRejectReason` (`ModalKindText`, buttons `[Enter] [Skip]`):
   "Reason for rejection (optional):". `Enter` (on the input or the
   `[Enter]` button) → `RecordDecision(Rejected)` with the trimmed text as
   `Rationale`. `[Skip]` or `Esc` → the same with an empty `Rationale` —
   `Esc` here skips the reason, it does not cancel the rejection.
   `Tab`/`Shift+Tab` cycle input → `[Enter]` → `[Skip]`. `ReviewerNotes` is always empty on a reject.

`saveReviewCmd` writes the activity log using `decisionComment()`:
`ReviewerNotes` for an approval, `Rationale` for a rejection.

---

## Theme

Use semantic styles from `internal/theme` only. Never use `lipgloss.Color()`
directly.

---

## Invariants Checklist

- "Not run" and "0 findings" are never displayed the same way — in the tab
  label, the Findings pane, or the Approve modal.
- Approve always opens the modal; there is no direct-approve path.
- Approval notes go to `ReviewerNotes`; rejection reasons go to `Rationale`.
- `a`, `Enter`, `Ctrl+X` act only when `Status == Pending`; otherwise they
  render dimmed with "(requires Pending)".
- `Tab`/`Shift+Tab` never switch tabs.
- Every tab's content renders through a bounded pane; none is a bare string.
- Layout sizing and `View()` share one section-building function.
- Tests pass; gofmt and lint clean.
