# Package Details

> Last updated: 2026-10-10
> Status: Active
> Companion docs: ui-overview.md, domain.md, workflow.md, tui-dashboard-wireframe.md

## Purpose

Answers: **What is the current state of this package, and what should I
do next?**

Reached with `Enter` on a Dashboard row.

---

## Layout

Summary tab, for a package in the Merged state:

```text
aur-curator          Workspace: /home/pramod/repos/aur                    ← App header
Last Refresh: 19:07
┌────────────────────────────────────────────────────────────────────────────────────────────┐
│ [1] Packages   [2] Repository   [3] Settings   [4] Help   [q] Quit                         │
│                                                                                            │
│‹ Dashboard  duplicati-bin  Lifecycle: Merged                                               │
│Commits Behind: 0  Status: Clean  Branch: master  Origin: https://gitserver.n/aur/dup...    │
│ [ Summary ]   Commits   Build Log   Artifacts                                              │
│                                                                                            │
│Review Summary                                                                              │
│Findings: 6 (3 HIGH, 3 LOW)                                                                 │
│Status: Approved                                                                            │
│Decided on: 2026-10-09 13:17:32                                                             │
│                                                                                            │
│Latest Build                                                                                │
│Status: success  Start: 2026-10-09 19:01:01                                                 │
│Duration: 23s                                                                               │
│                                                                                            │
│                                                                                            │
│ENTER Open Review  ^PgDn/^PgUp Tabs  F Fetch  M Merge  B Build  C Confirm  P Publish  ...   │
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

The action bar is abbreviated in the drawing; the full bar ends with
`Esc Back  ? more`. Dimmed entries cannot be shown in plain text here.
The App header, toast row, Operations Panel and status bar are drawn by
the App, as on every screen (`tui-dashboard-wireframe.md` § 7).

The content area between the tab bar and the action bar is one scrollable
pane. It holds the selected tab's content and fills the remaining height.

---

## Sections

### 1. Nav Menu

The same Nav Menu as the Dashboard, followed by a blank line. `[1]` jumps
to the Dashboard. `[3]` is dimmed until the Settings screen exists. A Nav
Menu jump clears the Back history.

### 2. Operation Failure Banner

Shown only after an operation on this package fails while the screen is
open (for example a Fetch or Build), above the header:

```text
⚠ Build failed: <error>
```

It does not replace the screen's content. It clears when an operation on
the package next succeeds, or when the screen is re-entered.

### 3. Header

```text
‹ Dashboard  <package>  Lifecycle: <state>
```

The state is derived the same way as on the Dashboard
(`lifecycle.DeriveState`, `domain.md` § Package → Lifecycle).

### 4. Stats Row

```text
Commits Behind: N  Status: <working tree>  Branch: <branch>  Origin: <url>
```

| Field | Values |
| --- | --- |
| Commits Behind | Fetched upstream commits not merged into local `master` |
| Status | `Clean`, `Detached HEAD`, `Has untracked files`, `Uncommitted changes` |
| Branch | The checked-out branch |
| Origin | The `origin` remote URL |

A field whose check failed shows `—`, never a guessed value.

### 5. Warning Rows

One row per warning, under the stats row, each starting with `⚠`:

- a check that failed, e.g. `⚠ Could not check commits behind: <error>`
  (the field it feeds shows `—`)
- local commits not on upstream: `⚠ N local commit(s) not on upstream`
  (`workflow.md` § Local Commits Not on Upstream)

No rows when there is nothing to report.

### 6. Tab Bar

```text
[ Summary ]   Commits   Build Log   Artifacts
```

The selected tab is in brackets. `^PgDn` / `^→` and `^PgUp` / `^←` move
to the next or previous tab, wrapping around. All four tabs are always
available. Opening the screen shows the Summary tab.

### 7. Tabs

#### Summary

Two blocks:

**Review Summary**

| Line | Content |
| --- | --- |
| Findings | Findings summary, shared with the Review screen (#198): `Not Run`, a zero-findings result, or `N (x HIGH, y LOW)` |
| Status | `Pending`, `Approved` or `Rejected` |
| Decided on | Decision time, or `—` while Pending |

With no review at all, the block shows only No review.

**Latest Build**: the latest build attempt, any status.

| Line | Content |
| --- | --- |
| Status / Start | Build status (`pending`, `running`, `success`, `failed`) and start time |
| Duration | Shown once the build has ended |

With no build, the block shows `No builds recorded`.

#### Commits

The upstream commits not yet merged, one per line:

```text
  635e8660  Yigit Sever  upgpkg: pfetch 1.11.0-1  2026-06-10T01:43:29+03:00
```

Short hash, author, subject, date. With nothing to merge, it shows
`No commits in diff`.

#### Build Log

The log of the latest build attempt, under a `Build Log` heading.
Otherwise one of these lines:

| Shown | When |
| --- | --- |
| `No builds yet` | The package has never been built |
| `Build queued — press End to load its output` | Right after Build was pressed on this screen (the tab switches here automatically) |
| `Waiting for the build to start…` | The requested build has not started; the latest record is still the previous build |
| `Waiting for output…` | The build is running but has written no log yet |
| `Loading build log…` | A build just finished; its log is being read |
| `Build cancelled` | The requested build was cancelled |
| `No log recorded for this build` | The build record has no log path |
| `Build log is empty` | The build finished with an empty log |
| `Could not load the build log: <error>` | Reading failed. Also written to the error log |

`End` on this tab re-reads the log and jumps to its bottom: the way to
follow a running build. When a build completes, the screen switches to
this tab and loads the finished log.

The log is shown the way a terminal would show it: on lines containing carriage returns (e.g. curl progress), only the text after the l
ast \r is shown. Other control characters except tab are dropped. The log file on disk is never modified.

#### Artifacts

The artifacts of the last successful build, under an `Artifact History`
heading:

```text
  1. duplicati-bin-2.4.0.1-2-x86_64.pkg.tar.zst (x86_64) 2.4.0.1-2
     Checksum: fa7faf305612fb1fa666f43f39214b2b3b9d684b250219e75ad16567210c4dde
     Size: 81.7 MB

  2. duplicati-bin-debug-2.4.0.1-2-x86_64.pkg.tar.zst (x86_64) 2.4.0.1-2
     Checksum: ee1669b6ea5fef33193a9cfa8c64e65b6b608fb907d645c4ca61824c9e09e2ab
     Size: 4.1 MB
```

Older builds' artifacts are not kept: a new successful build deletes the
previous one's records and files (#130, #352). Otherwise one of these lines:

| Shown | When |
| --- | --- |
| `Loading artifacts…` | The tab was just opened; the list is being read |
| `No artifacts recorded for this package` | The read succeeded and the package has no artifacts |
| `Could not load artifacts: <error>` | Reading failed. Also written to the error log |

If some artifact records cannot be read, the tab lists each unreadable record first, noting it may 
belong to any package, then this package’s readable records (or the empty state). If the artifact 
store itself cannot be read, it shows only the error.

### 8. Action Bar

Follows § Action Bar Convention below. Actions, in bar order:

| Key | Action | Available when |
| --- | --- | --- |
| `Enter` | Open Review | The package has a review |
| `^PgDn` / `^PgUp` | Tabs | Always |
| `F` | Fetch | Always |
| `M` | Merge | Review Approved and commits behind > 0 |
| `B` | Build | Review Approved and its commit merged (Merged or later) |
| `C` | Confirm | State is Built |
| `P` | Publish | State is Confirmed |
| `Esc` | Back | Always |

These conditions follow `workflow.md` § Action Availability and match the
Dashboard's. There is no Refresh and no Analyze on this screen; analyzers
run from the Review screen. There is no Install: installation is done with
pacman (`workflow.md` § Installation, #351).

Build always goes through the Build confirmation flow (`workflow.md`
§ Build Confirmation), shown as a modal over the screen.

With `?` pressed, the full help replaces the compact bar (state: Merged):

```text
│ ENTER        Open Review                  C            Confirm — requires Built            │
│ ^PgDn/^PgUp  Tabs                         P            Publish — requires Confirmed        │
│ F            Fetch                        Esc          Back                                │
│ M            Merge — requires Approved    ?            Close help                          │
│ B            Build                                                                         │
```

### 9. Loading and Error States

| Situation | Shown |
| --- | --- |
| Loading | `Loading <package>…` |
| The review or diff could not be loaded | The error, and `Esc Back` |

Any other failed check does not fail the screen: it shows as `—` in its
field plus a warning row (§ 5).

---

## Interaction

- `PgUp` / `PgDn` scroll the content pane; `Home` and `End` jump to its
  top and bottom (`End` on Build Log also reloads the log).
- `Esc` goes back to the previous screen. While the full help is open,
  `Esc` closes the help instead.
- `Enter` opens the Review screen for this package.
- Operations keep running while you navigate.

---

## Design Notes

### Action Bar Convention

This section is the single definition of the action bar for **every
screen** with one: Dashboard, Package Details, Review and Repository 
Status (#333). Help has no actions beyond the Nav Menu, so it has no 
action bar. Screens reached only through the Nav Menu list no Back: a 
Nav Menu jump clears the back history.

It replaces the earlier "dimmed with inline `(requires …)` reason"
convention and its Dashboard Exception: with more actions per screen
(Confirm and Publish on Package Details, #340) inline reasons no longer
fit on one line, and the Dashboard already omitted them.

**Compact bar (default).** One line at the bottom of the screen:

- Every action relevant to the screen is listed, regardless of state, as
  key + short label (`B Build`), in a fixed order per screen. The bar's
  layout therefore does not shift as the state changes — on the
  Dashboard, as the cursor moves between rows.
- Available actions render normally; unavailable actions render dimmed,
  **with no reason text**.
- The bar always ends with `? more`.
- The bar must fit on one line at the minimum screen width. If a screen's
  bar does not, its labels are shortened; the bar is never wrapped and
  never truncated.

**Full help (`?`).** Pressing `?` replaces the compact bar with the full
help, in place, below the screen's content:

- Every action, in columns: key, label, and — for an unavailable action —
  its prerequisite (`Merge — requires Approved`).
- `?` or `Esc` closes it. While it is open, `Esc` closes the help rather
  than navigating back.
- Its height is part of the screen's single layout (`developer-guide.md`
  § Single source of truth for section layout): the content area shrinks
  while it is open; nothing overflows or is pushed off screen.

**Prerequisite text.** Each action's prerequisite is written once and
shared by every screen. It states the rule in `workflow.md` § Action
Availability, in its terms:

| Action | Prerequisite text |
| --- | --- |
| Analyze / Run Analyzers | requires Review Pending |
| Merge | requires Approved |
| Build | requires Merged or later |
| Confirm | requires Built |
| Publish | requires Confirmed |
| Approve / Reject | requires a Pending review |

An action that is unavailable for a reason the lifecycle does not express
(e.g. no package selected) uses a short specific reason instead.

**Unchanged rules.**

- Pressing a dimmed action's key is a no-op (or optionally surfaces a
  one-line status message restating the prerequisite).
- Dimmed actions are **not focusable** — keyboard navigation skips them.
- An open modal owns every key: `?` does nothing while a modal is open.
- `?` is handled like the screen's other action keys. If a screen ever
  gives a text-entry field exclusive focus outside a modal, `?` goes to
  the field instead.

The full action vocabulary stays visible at all times, and the full help
teaches the lifecycle rules from the UI itself.

### Implementation Note

`bubbles/help` omits disabled key bindings rather than dimming them, so
it cannot render the compact bar as specified. The bar is a shared
widget (`internal/widgets`) that may borrow `bubbles/help`'s short/full
layout but renders availability itself. Screens supply the action list
and each action's availability; they do not format the bar.
