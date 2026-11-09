# TUI Review Wireframe

---
> This document defines visual intent only.
>
> The implementation guide for the Review screen is
> `docs/ui/review.md`.
>
> If the two documents disagree, `review.md` takes precedence.
---
> Last updated: 2026-11-09
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
| **Diff** | PKGBUILD plus any changed file that is not `.SRCINFO`, an `.install` file or a patch (e.g. `LICENSE`, stray checksum files). Multiple files render concatenated in one scrollable pane, each with a file header line. Control, bidi and zero-width characters show as escapes (§ Escaped Characters, #491). Always enabled. If only files on the other tabs changed, it says so instead of showing nothing; if the reviewed range changes no files at all, it says that. |
| **.SRCINFO** | Dedicated diff for `.SRCINFO`. |
| **.install** | Dedicated diff for `*.install` files. |
| **Patches** | Dedicated diff for patch files (`*.patch`, `*.diff`). |
| **Findings** | Not a diff — shows analyzer output. Displays one of three explicit states: `Not Run`, `0 findings`, or `N findings`. These are distinct and never collapsed into each other — "not run" must never read as "0 findings." Once the analyzers have run, the tab's first line is the dimmed note `Findings point at changes; they are not a verdict. The diff is the review.` (#474, #484), followed by the AUR lines (§ AUR Lines, #495); with no findings it then says `No findings.` A finding whose category has no severity puts the § Internal Inconsistency lines, in the error style, above that note. Findings are numbered `i/N` in display order (§ Findings Format, #471). |

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
file). The active tab's content pane always has focus and consumes the
scroll keys: `↑`/`↓` and `k`/`j` one line, `PgUp`/`PgDn` a page, `Home`/`End`
to the top or bottom (#344, #471). Only `PgUp`/`PgDn` reach the viewport's
own key handling, which would also act on `f`, `b`, `d`, `u`, space, `h` and
`l`; `←`/`→` are unbound (long lines are for a pager, #488). Switching to a
diff tab shows that tab's files from the top.

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
^PgDn/^PgUp Tabs  a Run Analyzers  ENTER Approve  ^X Reject  d Diff tool  v View  Esc Back  ? more
```

Follows the Action Bar Convention (`tui-package-details.md` § Action Bar
Convention): compact bar, `?` for full help. 

`a Run Analyzers`, `ENTER Approve` and `^X Reject` require
`Review.Status == Pending` (`workflow.md` § Action Availability);
otherwise they render dimmed, and the full help gives the prerequisite.

`d Diff tool` requires a tab that shows a diff (not Findings); `v View`
requires content in the pane. Both are described in § Your Own Tools
(#488).

`ENTER Approve` and `^X Reject` apply to the **review as a whole** — the full
reviewed commit range across all changed files — regardless of which tab is
currently active. They are not per-file or per-diff actions.

Dimmed actions are not focusable via keyboard navigation, and pressing
their key is a no-op (Action Bar Convention). `?` does nothing while the
Approve or Reject modal is open: the modal owns every key.

The full help (`?`) also lists the scroll keys, after the actions (#471).
They are not in the compact bar: it is not cut to the screen width, and a
wrapped bar would throw off the content-height calculation.

```text
^PgDn/^PgUp  Tabs           ^X           Reject         PgUp/PgDn    Scroll page
a            Run Analyzers  Esc          Back           Home/End     Top/Bottom
ENTER        Approve        ↑/↓ k/j      Scroll line    ?            Close help
```

### AUR Lines

Between the scope note and the findings, dimmed, from the fields stored
on the review (`domain.md` § AUR Record, #495):

On a first review (Import):

```text
AUR: maintainer alice; co-maintainers bob, carol
AUR: first submitted 2025-02-01 (646 days old); 1 vote; popularity 0.00011
AUR: maintainer not compared: no earlier record
```

On a later review with the same maintainer:

```text
AUR: maintainer alice; co-maintainers bob, carol
AUR: maintainer unchanged since 2026-10-30
```

- First line: `AUR: maintainer <name>` (co-maintainers when there are
  any), or `AUR: orphaned`.
- Second line, on a first review only (Import): first submitted, age,
  votes, popularity. The age is counted to when the record was read, so
  the line does not change later. Popularity has three significant
  digits: it is often far below 0.01.
- Comparison line, when there is no `maintainer_changed` finding:
  `maintainer unchanged since <date>`, or `maintainer not compared: no
  earlier record`. A change is a finding instead.
- When the read failed, one line in the error style replaces them:
  `AUR record not read: <reason>. Maintainer not compared.`
- Reviews from before #495 show no AUR lines.

### Escaped Characters

Diff lines (every tab that shows a diff), finding summaries and
locations are written by the AUR maintainer. Any character in them that
could make the screen show something other than the file is shown as a
visible escape in the error style, never sent to the terminal and never
silently removed (#491, `threat-model.md` S11):

| Characters | Shown as |
| --- | --- |
| carriage return | `\r` |
| other C0 control characters (ESC, BS, NUL, …), DEL, a byte that is not valid UTF-8 | `\xNN`, e.g. `\x1b` |
| C1 control characters (U+0080–U+009F) | `\uNNNN`, e.g. `\u009b` |
| bidi controls U+202A–U+202E, U+2066–U+2069; bidi marks U+200E, U+200F, U+061C | `\uNNNN`, e.g. `\u202e` |
| zero-width characters U+200B–U+200D, U+2060, U+FEFF | `\uNNNN`, e.g. `\u200b` |

Tab is shown as before and a newline still ends a line. Other non-ASCII
text (é, CJK, emoji) is unchanged. A file that contains the text `\x1b`
literally shows it in the line's own colour, so only colour tells it
from an escape.

### Findings Format

Below the scope note, the AUR lines and the `Findings: N (…)` header, each finding is
one line, numbered in display order (severity, then as reported), with
its Location, when it has one, on the next line (#471):

```text
 1/12 [HIGH] url: New source URL: https://a.example/x.tar.gz
             PKGBUILD:29
 …
12/12 [LOW] checksum: Checksum values updated
             PKGBUILD:41
```

- The number is right-aligned to the width of N, so the severities line
  up; the Location is indented by the number column plus seven spaces.
- The numbering is how a list longer than the pane shows that more
  follow: it changes the content only, never the layout (no extra line,
  no change to the pane height).
- Tabs in a summary (the Shell analyzer quotes the PKGBUILD line as
  written) are shown as four spaces, as in the diff.

---

## Your Own Tools

Two keys hand the screen's content to the user's own tools (#488). While
a tool runs, aur-curator is suspended: the tool has the terminal, and
aur-curator comes back on the same tab at the same scroll position when
the tool exits. Operations keep running but their results appear only
afterwards, and nothing can be cancelled meanwhile.

**`d` Diff tool** runs, in the package's clone,

```text
git difftool <baseCommit> <upstreamCommit> -- <each file of the active tab>
```

that is, the reviewed range (§ Reviewed Range in `domain.md`) for every
file the tab shows, one after another, as `git difftool` does on the
command line. The user's git configuration decides everything else: the
tool (`diff.tool`; with none set, git tries kompare, emerge, vimdiff,
nvimdiff) and whether git asks before each file (`difftool.prompt`). On a
first review (Import) the old side is empty. Not available on Findings.

**`v` View** writes the active pane's whole content, as the screen shows
it but without colour, to a temporary file and opens it in `$PAGER`
(`less -R` when unset). Control and invisible characters are therefore
escaped as on the screen (§ Escaped Characters). On a tab that shows a
diff the file is named `….diff`, so a pager such as `bat` highlights it;
otherwise `….txt`. The file is private (mode 0600) and removed when the
pager exits. The pager runs without `LESSOPEN` and `LESSCLOSE`, so
`less` passes the file to no filter program.

If git or the pager cannot run, a toast says why and the error log keeps
git's full message. git reports its own failures (no diff tool found, a
bad commit) but not the tool's: a tool that exits with an error leaves
git exiting normally, unless `difftool.trustExitCode` is set.

The files are untrusted text; the README (§ Reading a Review in Your Own
Tools) says what that means for editors and pagers.

## Confirmation Flows

### Approve

Every `ENTER` (Approve) opens a confirmation modal, regardless of Findings
status. Its first line depends on the analyzer state, and is always one of
three distinct messages — "not run" never reads as "0 findings":

| Findings status | First line |
| --- | --- |
| `Not Run` | Three fixed lines and a blank line: `This update has not been analysed, so these checks have not run:` / `sources, checksums, PGP keys, dependencies, shell code and the` / `AUR maintainer. Approve anyway?` (#495) |
| Run, 0 findings | There are 0 findings, approve? |
| Run, N findings | The one-line findings summary, then `Approve?` |

The N-findings line comes from `findingsSummary()`, the same summary the
tab bar shows next to the Findings tab. It is one line by construction, never a
per-finding list: the modal has no scrolling, so its height must not grow
with the number of findings.

Below that, an optional notes input:

```text
┌──────────────────────────────────────────────────────────────────┐
│ This update has not been analysed, so these checks have not run: │
│ sources, checksums, PGP keys, dependencies, shell code and the   │
│ AUR maintainer. Approve anyway?                                  │
│                                                                  │
│ Reviewer notes (optional):                                       │
│ ________________________________________________                 │
│                                           [Enter] [Esc]          │
└──────────────────────────────────────────────────────────────────┘
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

## Internal Inconsistency

A finding whose category has no severity mapping is a bug
(`risk-analysis.md` § Unmapped categories). It is ranked HIGH. When the
screen loads such a review, a modal shows, for each such category:

```
Internal inconsistency: finding category "<name>" has no severity (ranked HIGH).
Please file a bug report with this line, the package name and aur-curator's version.
```

`ENTER` or `ESC` dismisses it. It shows once per opening of the screen.
The same lines stay at the top of the Findings tab while the review is
shown (#475).

---

## Open Items

- Findings subview (populated layout: findings list, severity, grouping) —
  not yet wireframed.
- Whether the Approve modal should be skippable (e.g. a one-key approve for
  frequently-reviewed `-bin` packages) is deferred to a future Settings
  view; current decision is **always-on** for MVP. Note the trade-off has
  changed since #22: skipping the modal would now also skip the only place
  reviewer notes can be entered.
