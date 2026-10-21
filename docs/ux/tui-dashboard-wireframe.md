# TUI Dashboard Wireframe

> Last updated: 2026-10-10
> Status: Active
> Companion docs: ui-overview.md, domain.md, workflow.md, tui-package-details.md

## Purpose

The Dashboard is the primary landing view of **aur-curator**. It answers:

> **What requires my attention, and what should I do next?**

It is a derived projection over Package, Review, RiskAssessment, Build,
git state and the AUR update check. No dashboard field is stored.

---

## Layout

```text
Operation cancelled: duplicati-bin                                        ← toast, only while active
aur-curator          Workspace: /home/pramod/repos/aur                    ← App header
Last Refresh: 14:24
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ [1] Packages   [2] Repository   [3] Settings   [4] Help   [q] Quit                         │
│ [ Packages: 6 ]  | AUR: 1 | Fetched: 2 | To review: 1 | Approved: 1 | Merged: 1 | ...      │
│Filter: Packages  Sort: (·) Alphabetical   ( ) Lifecycle                                    │
│> yay          Review Pending  Review    —              No     HIGH                  ↑2     │
│  broot        Approved        Merge     —              No     LOW                   ↑1     │
│  fd           Merged          Build     —              Older  LOW                          │
│  bat          Built           Confirm   —              No     Not Run                      │
│  ripgrep      Confirmed       Publish   13.0 → 14.1    No     LOW                          │
│  units        Published       —         —              Yes    LOW      2 local             │
│                                                                                            │
│ ^PgDn/^PgUp Cards  ^O Sort  ENTER Open  M Merge  F Fetch  R Refresh  B Build  C ...  ? more│
└────────────────────────────────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ Operations                                                                                 │  ← App level
│ ────────────────────────────────────────────────────────────────────────────────────────── │
│   Fetch                                                                                    │
│   Analyze                                                                                  │
│   Build                                                                                    │
│   Publish                                                                                  │
└────────────────────────────────────────────────────────────────────────────────────────────┘
Idle                                                                                            ← status bar, App level
```

The first box is the Dashboard's own area. The header, toast row,
Operations Panel and status bar are drawn by the App and appear on every
screen (§ 7). `>` marks the selected row. The selected card is shown in
brackets.

The table has no column header row. Cells that would be ambiguous without
a header label themselves (`2 local`, `↑2`).

---

## Sections

### 1. Nav Menu

The first row of the Dashboard:

```text
[1] Packages  [2] Repository  [3] Settings  [4] Help  [q] Quit
```

A single key press jumps to that screen. The jump is absolute: it does not
add to the Back history, and it clears that history. `[1]` is the current
screen, so it does nothing here. `[3]` is dimmed until the Settings screen
exists.

---

### 2. Summary Cards

Nine cards, in this order. Each card shows a short label and a count; the
toolbar's `Filter:` line shows the card's descriptive name while it is
selected. Each card's count comes from the same predicate that filters the
table when the card is selected, so the count always matches the rows shown.
The stage cards (To review through Confirmed) do not overlap; Packages,
AUR, Fetched and Failed cut across them.

| Card | Filter line | Counts packages where |
| --- | --- | --- |
| Packages | Packages | Always (every curated package) |
| AUR | AUR fetch pending | The AUR has a newer version than the fetched one (§ 4, Available). Shows `?` instead of a count until the check arrives, or if it failed |
| Fetched | Fetched (not merged) | Commits behind upstream > 0: fetched upstream commits not yet merged |
| To review | Review Pending | State is Review Pending |
| Approved | Approved (not merged) | State is Approved |
| Merged | Merged (not built) | State is Merged |
| Built | Built (not confirmed) | State is Built |
| Confirmed | Confirmed (unpublished) | State is Confirmed |
| Failed | Last build failed | The latest build attempt failed, for a package in the build flow (Approved through Published). A failed rebuild of a Published package counts here without changing its state (#309) |

`^PgDn` / `^PgUp` move the selection to the next or previous card, and the
table shows only that card's packages. Packages is selected at startup and
shows everything.

---

### 3. Toolbar

One row under the cards:

- **Filter**: shows the label of the selected card.
- **Sort**: `(·) Alphabetical   ( ) Lifecycle`, toggled with `^O`. Only
  available while the Packages card is selected; on other cards it is
  dimmed.

---

### 4. Package Table

The main working area. One row per curated package, without a header row.

Columns, in order:

| Column | Content |
| --- | --- |
| Package | Package name |
| State | Lifecycle state (`lifecycle.DeriveState`) |
| Next Action | The action the state calls for (table below) |
| Available | Whether the AUR has something newer than the fetched version (below) |
| Published | `Yes` / `Older` / `No` (below) |
| Risk | Highest finding severity, or `Not Run` (below) |
| Local | Commits on local `master` not on upstream (below) |
| ↑N | Commits behind upstream: fetched but not merged. Blank when 0 |

Packages without both an `origin` and an `upstream` remote are left out of
the table. Each one is written to the error log.

#### Next Action

Derived from the lifecycle state.

| State | Next Action |
| --- | --- |
| Not Reviewed | Fetch |
| Review Pending | Review |
| Approved | Merge |
| Merged | Build |
| Built | Confirm |
| Confirmed | Publish |
| Published | — |
| any other state | — |

Not Reviewed normally cannot be seen: Import creates the review, and
loading the Dashboard creates a missing one for older packages. It shows
only when that creation failed, which is also written to the error log.

Building has no state of its own between Merged and Built. Build progress
is shown in the Operations Panel (§ 7), not by a Next Action.

Published is the final state. Installing and upgrading are done with
pacman from the local repository (`workflow.md` § Installation, #351).

#### Available

Whether the AUR has a newer version than the one already **fetched**,
i.e. whether a Fetch would bring something new. An update that is already
fetched and waiting for review is shown by `↑N` and the Review Pending
state, not here. The local version is read from git at the fetched commit
(`workflow.md` § Local Version), never from the working tree.

| Value | Meaning |
| --- | --- |
| `x → y` | AUR has `y`, newer than the fetched `x` |
| `→ y` | Same, when `x → y` does not fit the column (25 cells) |
| `—` | Check succeeded; nothing newer on the AUR |
| `not on AUR` | The AUR does not know the package (deleted or renamed) |
| `?` | Unknown: the whole check failed, or the local version could not be determined |
| `…` | Check still running |

Only a completed, successful check may read as `—`. While the check has
not arrived or has failed, the AUR Update card shows `?` instead of a
count. A failed check is written to the error log.

#### Published

From the package's last successful build (`workflow.md` § State
Transitions, #309):

| Value | Meaning |
| --- | --- |
| `Yes` | The last success is published, and it was built from the current local `master` HEAD |
| `Older` | The last success is published, but for an older commit. A previous version is in the package repository; the merged one is not built and published yet |
| `No` | No successful build, or the last success is not published |

#### Risk

| Situation | Shows |
| --- | --- |
| No review | `No review`, dimmed |
| Analyzers not run | `Not Run`, dimmed |
| Analyzers run, no findings | `NONE`, green |
| Approved or Rejected, findings present | The highest severity frozen in its RiskAssessment (`HIGH` / `MEDIUM` / `LOW`) |
| Pending, findings present | The highest severity of the current findings, computed live by the Policy Engine |

These are always distinct: `Not Run` never stands for "analyzers ran and
found nothing" (see the FindingsStatus rule in `developer-guide.md`), and a
review with no findings never shows a severity.

#### Local

Commits on local `master` that are not on upstream (`workflow.md` § Local
Commits Not on Upstream). The table has no header row, so the cell labels
itself:

| Value | Meaning |
| --- | --- |
| (blank) | None |
| `N local` | N commits not on upstream |
| `? local` | The check failed |

Local commits never disable Build: pressing Build runs the Build
confirmation flow, which asks (`workflow.md` § Build Confirmation, #320).

---

### 5. Action Bar

The last row of the Dashboard. It follows the Action Bar Convention
(`tui-package-details.md` § Action Bar Convention): a compact one-line
bar, unavailable actions dimmed with no reason text, and `?` to open the
full help, which gives each unavailable action's prerequisite. `?` or
`Esc` closes the help. While the help is open the table gets shorter.

Actions, in bar order:

| Key | Action | Available when |
| --- | --- | --- |
| `^PgDn` / `^PgUp` | Next / previous card | Always |
| `^O` | Toggle sort | Packages card selected |
| `Enter` | Open Package Details | A row is selected |
| `M` | Merge | State is Approved and commits behind > 0 |
| `F` | Fetch | A row is selected |
| `R` | Refresh | Always |
| `B` | Build | State is Merged, Built, Confirmed or Published (a rebuild from the last three) |
| `C` | Confirm | State is Built |
| `P` | Publish | State is Confirmed |

These conditions follow `workflow.md` § Action Availability. Actions apply
to the selected row only.

Build always goes through the Build confirmation flow (`workflow.md`
§ Build Confirmation). Its prompts open as a modal over the Dashboard.

`R` reloads the package list, review data and AUR check. If the list is
empty (for example because the first load failed), it shows the loading
state while retrying; otherwise the rows update in place.

---

### 6. Loading and Empty States

| Situation | Shown |
| --- | --- |
| Package list loading | `Loading packages...` |
| Package list failed to load | `Error loading packages: <error>` (press `R` to retry) |
| No curated packages | `No curated packages found.` |
| Review data still loading | A plain list of package names, each with its state or `(loading...)`; cards, toolbar and columns appear once every package's data has arrived |

---

### 7. App-Level Areas

Drawn by the App, around every screen, not by the Dashboard:

- **Header**: two lines above the screen's box. The first shows
  `aur-curator` and the workspace path; the second shows the last refresh
  time (`Last Refresh: HH:MM`), updated by `R` and by each package-list
  load.
- **Toast row**: the very first line, above the header. Short messages
  such as `Operation cancelled: duplicati-bin`, a queue position, or a
  reset selection. Shown only while a toast is active.
- **Operations Panel**: a bordered box titled `Operations` with four fixed
  lanes, one per line: Fetch, Analyze, Build and Publish. Each lane shows
  its status (idle, running, completed, failed, cancelled) and keeps its
  own log. Only one operation runs at a time
  (`developer-guide.md` § Operations Queue). `^T` cancels the running
  operation only; queued operations are not affected.
- **Status bar**: the last line, under the Operations Panel. Shows `Idle`
  when no operation is running.

---

## Interaction

- `↑` / `↓` or `k` / `j` move the table cursor. `↓` on the last row of a
  full page moves to the next page.
- `Enter` opens Package Details for the selected row.
- Fetch, Build and other operations keep running while you navigate.
- There is no multi-select and no Tab focus cycle on this screen. `Tab`
  is reserved for moving focus within a screen (`developer-guide.md`
  § Tab/Shift+Tab).

---

## Design Notes

The Dashboard is intentionally action-oriented. The Summary Cards, the
Next Action column and the Operations Panel together answer three
questions without opening Package Details:

1. What changed?
2. What needs attention?
3. What should I do next?
