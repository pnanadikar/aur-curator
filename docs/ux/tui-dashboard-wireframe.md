# TUI Dashboard Wireframe

> Last updated: 2026-10-30
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
┌────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ [1] Packages   [2] Repository   [3] Settings   [4] Help   [q] Quit                                                 │
│ [ Packages: 6 ]  | AUR: 1 | Fetched: 2 | To review: 1 | Approved: 1 | Merged: 1 | ...                              │
│Filter: Packages  Sort: (·) Alphabetical   ( ) Lifecycle                                                            │
│  Package      State                               Next Action  Available      Published  Risk     Local  Behind    │
│> yay          Review Pending 12.4.1-1 → 12.4.2-1  Review       —              No         HIGH            ↑2        │
│  broot        Approved       0.44.1-1 → 0.45.0-1  Merge        —              No         LOW             ↑1        │
│  fd           Merged                    10.1.0-1  Build        —              Older      LOW                       │
│  bat          Built                     0.24.0-2  Confirm      —              No         Not Run                   │
│  ripgrep      Confirmed                 13.0.0-1  Publish      14.1.1-1       No         LOW                       │
│  units        Published                 2.24-1    —            —              Yes        LOW      2                │
│                                                                                                                    │
│ ^PgDn/^PgUp Cards  ^O Sort  ENTER Open  M Merge  F Fetch  R Refresh  B Build  C ...  ? more                        │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Operations                                                                                                         │  ← App level
│ ────────────────────────────────────────────────────────────────────────────────────────────────────────────────── │
│   Fetch                                                                                                            │
│   Analyze                                                                                                          │
│   Build                                                                                                            │
│   Publish                                                                                                          │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
Idle                                                                                                                    ← status bar, App level
```

The first box is the Dashboard's own area. The header, toast row,
Operations Panel and status bar are drawn by the App and appear on every
screen (§ 7). `>` marks the selected row. The selected card is shown in
brackets.

The row under the toolbar is the table's header row (§ 4, Header Row).

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
| AUR | AUR fetch pending | The AUR has moved past the reviewed commit (§ 4, Available). Shows `?` instead of a count until the check arrives, or if it failed |
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

The main working area. A header row naming the columns, then one row per
curated package.

Columns, in order:

| Column | Content |
| --- | --- |
| Package | Package name |
| State | Lifecycle state, followed by the versions it concerns (below) |
| Next Action | The action the state calls for (table below) |
| Available | The AUR's version, when the AUR has moved past the reviewed commit (below) |
| Published | `Yes` / `Older` / `No` (below) |
| Risk | Highest finding severity, or `Not Run` (below) |
| Local | Commits on local `master` not on upstream (below) |
| Behind | `↑N`: commits behind upstream, i.e. fetched but not merged. Blank when 0 |

Packages without both an `origin` and an `upstream` remote are left out of
the table. Each one is written to the error log.

#### Header Row

One row above the package rows, naming the columns: `Package`, `State`,
`Next Action`, `Available`, `Published`, `Risk`, `Local`, `Behind`.

- Each label starts where its column starts. `Package` starts two cells
  in, under the package names, not under the `>` cursor.
- State's width depends on the page: it is the longest State cell among
  the rows shown, versions included, up to 34 cells (§ State). The header
  uses the same width, so its labels stay over their columns as you page.
- `Behind` labels the `↑N` count. Being the last column, it has no fixed
  width. Without the label, `↑N` read as a misplaced Local value whenever
  Local was blank.
- Drawn dim, like other secondary text. No rule line under it.
- Shown whenever the table is: also above "No packages match the current
  filter.", so the layout does not move when a filter empties the list.
  It is not shown while the package data is still loading, when the
  plain list is shown instead.
- It takes one row from the table: the page size allows for it.

#### Page Size

A page holds as many package rows as fit between the header row and the
action bar, down to a single row, so the selected row is always on
screen. `↓` on a page's last row and `↑` on its first move to the next or
previous page (§ Interaction). When not even one row fits, the Dashboard
shows a single line instead (§ 6).

#### State

The lifecycle state (`lifecycle.DeriveState`), followed by the versions
that state is about. A version is the PKGBUILD's `[epoch:]pkgver-pkgrel`,
written as the AUR writes it (`workflow.md` § AUR Version Check), read
from git at a commit:

| State | Shows | Version read at |
| --- | --- | --- |
| Not Reviewed | `Not Reviewed` | — |
| Review Pending, Approved | `Approved 1.10.0-1 → 1.11.0-1` | The review's base commit, then its reviewed commit: the range the Review screen shows |
| Review Pending, Approved, first review | `Review Pending → 1.11.0-1` | The reviewed commit. A first review has no base commit |
| Rejected | `Rejected 1.11.0-1` | The reviewed commit |
| Merged, Built, Confirmed, Published | `Merged 1.11.0-1` | Local `master` HEAD: what Build builds, and what Published compares against |

- Next Action shows no versions. It always sits beside State, so the
  same pair would show twice.
- The versions are read when the Dashboard loads the package's review
  data (at start, on Refresh, and after an operation), not from the AUR.
  They show even when the AUR request fails.
- The versions line up down the page: the state names are padded to the
  longest one, the arrows sit in one column, and each row's own version
  (reviewed, or `master` HEAD) starts in the same column:

  ```text
  Review Pending 1.10.0-1 → 1.11.0-1
  Approved                → 2.27-1
  Merged                    1.11.0-1
  ```
- The State column is at most 34 cells. When the page does not fit, every
  pair drops its base version (`Approved → r345.abc1234-1`), so the arrows
  stay in one column; a cell still too long is cut with `…`.
- When a version cannot be read, State shows the state alone, never half
  a pair. A version the PKGBUILD computes, or one it lacks, is a property
  of the package (Available shows `?` for it). A failed git read is
  written to the error log.
- Sorting and filtering go by the state, not the versions.

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

Whether the AUR has moved past the commit the current review covers,
i.e. whether a Fetch would bring something new. What is compared, every
possible result, the limits (VCS packages included) and the decisions
behind them are in `workflow.md` § AUR Version Check; this section only
says how the column shows them. An update that is already fetched and
waiting for review is shown by `↑N` and the Review Pending state, not
here; one that is approved but not merged, by the Approved state and its
Merge Next Action.

| Value | Meaning |
| --- | --- |
| `y` | Stale: the AUR has `y`, which differs from the reviewed commit's version (State shows that one). `y` is not necessarily higher |
| `—` | Up to date |
| `not on AUR` | Not on the AUR |
| `?` | Version not readable, local version failed, or request failed |
| `…` | Check still running |

A version longer than the column (25 cells) is cut with `…`.

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
Commits Not on Upstream).

| Value | Meaning |
| --- | --- |
| (blank) | None |
| `N` | N commits not on upstream |
| `?` | The check failed |

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
| Terminal too short for one package row | `Terminal too small: At least <N> lines and <M> columns recommended.` in place of the Dashboard |

In the "terminal too small" line, `<N>` is the terminal height at which a
page holds 6 package rows, and `<M>` the width at which the Dashboard's
widest line is not cut. Both are worked out from what is on screen at the
time, so they change with the number of Operations Panel lines, the
action-bar help and the versions in the State column. Only the height
brings up the line: a terminal too narrow cuts the rows on the right
(#457 makes the columns adapt).

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
  full page moves to the next page; `↑` on the first row of any page but
  the first moves to the previous page, onto its last row (#447).
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
