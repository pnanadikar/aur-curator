# TUI Review Wireframe

---
> This document defines visual intent only.
>
> The implementation guide for the Review screen is
> `docs/ui/review.md`.
>
> If the two documents disagree, `review.md` takes precedence.
---
> Last updated: 2026-10-19
> Status: Active
> Companion docs: ui-overview.md, workflow.md, domain.md

> **Note (2026-10-01, #22):** the always-visible Reviewer Notes field is
> gone. Notes are now entered in the Approve confirmation modal, which
> opens on every Approve (see § Confirmation Flows). With the field gone,
> the tab content pane is the screen's only focus target, so the
> content-pane ↔ notes focus cycle described in the 2026-08-23 revision no
> longer exists. `TAB`/`SHIFT+TAB` remain reserved for focus-cycling, never
> tab-switching (`developer-guide.md` § Tab/Shift+Tab reserved for focus,
> not content-switching); they currently have nothing to cycle between.
> Run Analyzers moved from `^A` to plain `a` (REV-004): the `^` existed
> only to avoid colliding with typing in the notes field.
>
> **`docs/ui/review.md`** (which takes precedence per the note above) was
> rewritten on 2026-10-01 for both changes, and updated for #385 and #387.

## Purpose

The Review screen answers:

> **Should these upstream changes be trusted?**

It is reached from Package Details (`ENTER Open Review`) and scopes a single
Review entity: the reviewed range for one package — from the review's base
commit to the reviewed upstream commit (`domain.md` § Reviewed Range) —
plus any analyzer findings and the resulting Decision. The screen shows the
same range before and after Merge (#387).

Lifecycle semantics (Approve/Reject meaning, Import handling) are defined in
`workflow.md`. This document covers the screen layout, tab behavior, and
interaction/confirmation flows only.

---

## Layout

```text
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ ‹ Package Details        yay — Review                        Status: Pending               │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│ Reviewing: a1b2c3d → 9f8e7d6                                    2 commits, 3 files changed │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│ [ Diff ]   .SRCINFO   .install   Patches        Findings: Not Run                          │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│  --- PKGBUILD (master)                                                                     │
│  +++ PKGBUILD (upstream/master)                                                            │
│                                                                                            │
│  -pkgver=12.4                                                                              │
│  +pkgver=12.5                                                                              │
│                                                                                            │
│  -source=("https://github.com/Jguer/yay/releases/download/v12.4/...")                      │
│  +source=("https://github.com/Jguer/yay/releases/download/v12.5/...")                      │
│                                                                                            │
├────────────────────────────────────────────────────────────────────────────────────────────┤
│ ^PgDn/^PgUp Tabs  a Run Analyzers  ENTER Approve  ^X Reject  Esc Back  ? more              │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

### First-time Import variant

When a package has no prior reviewed commit (see `workflow.md` § Import),
the review has no base commit, so the comparison summary and the diff tabs
render differently:

```text
│ Reviewing: (new) → 9f8e7d6                            1 commit, 4 files changed            │
```

Every file is shown in full as additive (all lines shown as `+`), since there
is no prior version to compare against. Because every file counts as
changed, each of the `.SRCINFO`, `.install` and Patches tabs is enabled
exactly when the package has such a file (§ 3 Tab Bar). All other behavior
(analyzers, Approve/Reject, confirmations) is unchanged.

---

## Sections

### 1. Header

- Back navigation to Package Details.
- Package name and current Review status (Pending / Approved / Rejected).

### 2. Comparison Summary

States exactly what was reviewed: the review's base commit → the reviewed
upstream commit (`Review.BaseCommit` → `Review.UpstreamCommit`), plus a
count of commits and changed files in that range. On a first review the
base is shown as `(new)`.

The range is the one stored on the review, never the live
`master...upstream/master`. The base is where `master` stood relative to
upstream when the review was created (their merge base), so the line reads
the same before and after Merge, and a later fetch does not change it. If
`master` carries local commits that are not on upstream, the base is still
the last upstream commit, not the local tip. This wording is deliberate —
the application never implies comparison against a "published version".

### 3. Tab Bar

```text
[ Diff ]   .SRCINFO   .install   Patches        Findings: Not Run
```

| Tab | Content |
| --- | --- |
| **Diff** | PKGBUILD plus any changed file that is not `.SRCINFO`, an `.install` file or a patch (e.g. `LICENSE`, stray checksum files). Multiple files render concatenated in one scrollable pane, each with a file header line. Always enabled. If only files on the other tabs changed, it says so instead of showing nothing; if the reviewed range changes no files at all, it says that. |
| **.SRCINFO** | Dedicated diff for `.SRCINFO`. |
| **.install** | Dedicated diff for `*.install` files. |
| **Patches** | Dedicated diff for patch files (`*.patch`, `*.diff`). |
| **Findings** | Not a diff — shows analyzer output. Displays one of three explicit states: `Not Run`, `0 findings`, or `N findings`. These are distinct and never collapsed into each other — "not run" must never read as "0 findings." |

`.SRCINFO`, `.install` and Patches are **always shown**, and enabled only
when this review changes a file of their kind; otherwise they are dimmed
and skipped (#385). An enabled `.install` tab is itself a signal: install
scripts are a high-risk change (`risk-analysis.md`). On a first review
(Import) every file is an addition, so a tab is enabled exactly when the
package has such a file. Tabs are re-evaluated whenever the diff is
reloaded; if the active tab becomes dimmed, Diff becomes active.

Tabs are switched with Ctrl+PgDn/Ctrl+PgUp (also Ctrl+Right/
Ctrl+Left). Dimmed tabs are skipped during tab navigation, consistent
with the dimmed-action rules (`tui-package-details.md` § Action Bar
Convention). TAB/SHIFT+TAB
never switch tabs on this screen (see the note at the top of this
file). The active tab's content pane always has focus and consumes
PgUp/PgDn to scroll. Switching to a diff tab shows that tab's files from
the top.

### 4. Reviewer Notes

Not a section of the screen. Reviewer notes are entered in the Approve
confirmation modal (§ Confirmation Flows → Approve), whose text input
maps to `Review.ReviewerNotes`. The input always starts blank: a Pending
review's `ReviewerNotes` is always empty, so there is nothing to pre-fill.

Reject records its optional reason in `Review.Rationale`, not
`ReviewerNotes` (§ Confirmation Flows → Reject). The two fields stay
separate: notes accompany an approval, rationale explains a rejection.

Because there is no text field on the screen itself, every action key is
a plain or `^`-modified key with no text-entry collision to guard against
(`developer-guide.md` § Modifier-key convention).

### 5. Action Bar

```text
^PgDn/^PgUp Tabs   a Run Analyzers   ENTER Approve   ^X Reject   Esc Back   ? more
```

Follows the Action Bar Convention (`tui-package-details.md` § Action Bar
Convention): compact bar, `?` for full help. 

`a Run Analyzers`, `ENTER Approve` and `^X Reject` require
`Review.Status == Pending` (`workflow.md` § Action Availability);
otherwise they render dimmed, and the full help gives the prerequisite.

`ENTER Approve` and `^X Reject` apply to the **review as a whole** — the full
reviewed commit range across all changed files — regardless of which tab is
currently active. They are not per-file or per-diff actions.

Dimmed actions are not focusable via keyboard navigation, and pressing
their key is a no-op (Action Bar Convention). `?` does nothing while the
Approve or Reject modal is open: the modal owns every key.

---

## Confirmation Flows

### Approve

Every `ENTER` (Approve) opens a confirmation modal, regardless of Findings
status. Its first line depends on the analyzer state, and is always one of
three distinct messages — "not run" never reads as "0 findings":

| Findings status | First line |
| --- | --- |
| `Not Run` | No analyzers have been run for this review. Approve anyway? |
| Run, 0 findings | There are 0 findings, approve? |
| Run, N findings | The one-line findings summary, then `Approve?` |

The N-findings line comes from `findingsSummary()`, the same summary the
tab bar shows next to the Findings tab. It is one line by construction, never a
per-finding list: the modal has no scrolling, so its height must not grow
with the number of findings.

Below that, an optional notes input:

```text
┌──────────────────────────────────────────────────────────────┐
│ No analyzers have been run for this review. Approve anyway?  │
│ Reviewer notes (optional):                                   │
│ ________________________________________________             │
│                                          [Enter] [Esc]       │
└──────────────────────────────────────────────────────────────┘
```

- Appears only at the moment Approve is pressed — not a standing banner.
- Renders as a centered overlay above the Review screen content, so the
  review body stays visible behind it and is not pushed upward.
- The input starts blank and has focus when the modal opens.
- `Enter` → Decision recorded as Approved, with the input's text (possibly
  empty) stored in `Review.ReviewerNotes`. The review closes and returns
  to Package Details.
- `Esc` → modal dismisses; no Decision recorded and any typed text is
  discarded. The user remains on the Review screen exactly as before.
  Cancelling does not nudge the user toward Run Analyzers — it is a plain
  cancel.

Approving without running analyzers remains a soft guard rail, not a hard
block: the user may always approve, appropriate for low-complexity packages
(see `risk-analysis.md` § Analyzer-Optional Review). The not-run wording
keeps the skip a deliberate, visible choice rather than an accidental one.

### Reject

Two-step confirmation, always shown regardless of Findings status.

**Step 1 — confirm intent:**

```text
┌──────────────────────────────────────────────────────────────┐
│ Reject this review?                                          │
│ No further action will be taken — this update will not be    │
│ installed.                                          [Y] [n]  │
└──────────────────────────────────────────────────────────────┘
```

**Step 2 — optional reason (only shown if Step 1 = Y):**

```text
┌──────────────────────────────────────────────────────────────┐
│ Reason for rejection (optional):                             │
│ ________________________________________________             │
│                                              [Enter] [Skip]  │
└──────────────────────────────────────────────────────────────┘
```

- Decision is recorded as Rejected regardless of whether a reason is
  provided; the reason, if given, is stored in `Review.Rationale`.
  `ReviewerNotes` stays empty on a rejected review.
- Both confirmation steps render as centered overlays above the Review screen
  content rather than as inline blocks that displace the main screen.
- Rejection does not modify the local repository or revert anything — it
  only records the decision. A future upstream commit may still be reviewed
  independently (see `workflow.md` § Review Decisions).

---

## Open Items

- Findings subview (populated layout: findings list, severity, grouping) —
  not yet wireframed.
- Whether the Approve modal should be skippable (e.g. a one-key approve for
  frequently-reviewed `-bin` packages) is deferred to a future Settings
  view; current decision is **always-on** for MVP. Note the trade-off has
  changed since #22: skipping the modal would now also skip the only place
  reviewer notes can be entered.
