# Package Lifecycle

> Last updated: 2026-10-30
> Status: Active
> Companion docs: domain.md, tui-review.md, developer-guide.md,
> git-command-walkthrough.md (the git commands behind each step)

```plaintext
Repository Imported
        │
        ▼
Fetch Upstream
        │
        ▼
Review Changes
        │
        ▼
Approve or Reject
        │
        ▼
Merge into Personal Repository
        │
        ▼
Build Package
        │
        ▼
Publish Repository
```

Publish is the final step aur-curator performs. Installing and upgrading
curated packages is done with plain pacman from the local package
repository (§ Installation); aur-curator never installs or uninstalls
packages (#351).

## Import (New Package)

A package with no prior reviewed commit (first-time addition to the curated
collection) has no local `master` to diff against. In this case, Review
presents the entire upstream state as an additive diff — every line of
PKGBUILD, .SRCINFO, .install (if present), and patches (if present) appears
as an addition, rather than a delta against a prior version.

```text
Reviewing: (new) vs upstream/master (9f8e7d6)         1 commit, 4 files added
```

The same Review workflow applies unchanged: the user inspects the diff (here,
the whole file), optionally runs analyzers, and records an Approve or Reject
decision. Approval proceeds to Merge as normal.

### Import Mechanics

Import creates the repository from scratch and lands the package directly
in **Review Pending** — there is no separate Fetched step for a new
package, since Import already retrieves the upstream content that Fetch
would otherwise pull.

Import is a CLI command (`aur-curator <config-file> import <package>`;
the TUI's Import is deferred, #112). The Git Provider performs the
repository part as one operation:

1. Check that the package exists on the AUR (RPC); nothing is created if
   it does not.
2. Create an empty repository on the personal Gitea instance via its API.
3. Clone the AUR repository, renaming its remote to `upstream`.
4. Add the new Gitea repository as the `origin` remote.
5. Replace local `master` with an empty, unrelated placeholder commit — no
   upstream content is merged in at this point. This is what makes the
   "(new) vs upstream/master" comparison above genuine: local `master` has
   no content yet, so the diff is fully additive, not merely displayed as
   one.

The command then creates the package's first Review (Pending), and runs
the analyzers on it if `autoAnalyzeAfterImport` is set; a failed analysis
is reported but does not undo the Import.

There is deliberately no "register the package" step: Package Catalog
scans the filesystem on every call rather than keeping a cache
(`interfaces.md` § Package Catalog). The package is curated as soon as its
directory exists (step 3); the next catalog call anywhere sees it.

Nothing is pushed to `origin` during Import — the Gitea repository stays
empty until Merge, which creates local `master` from the reviewed
upstream commit and performs the first push. (See § Merge —
for an imported package, Merge creates `master` for the first time rather
than fast-forwarding an existing one.)

### Partial Import Failure

If Import fails partway (e.g. the Gitea repo is created but the clone
fails), aur-curator does not attempt automatic rollback at this scale
(~10-12 packages). The user must be shown an explicit message
identifying which steps completed and which did not, so they can
manually verify and clean up — e.g. delete an orphaned Gitea repo, remove
a partial local clone. Silence about partial state is not acceptable;
the message is mandatory,
not optional error text.

The message — the same for a failure and a timeout — names what to clean
up before retrying: the Gitea repository (its clone URL) once it has been
created, and the local directory `<rootPath>/<pkg>` once the clone has
run, or "if it exists" when a timeout interrupted the clone. Every failure
and timeout is also written to the error log with the same guidance
(#401). If Import succeeds but creating its first review fails, nothing
needs cleaning up: the CLI says the package is imported, and the
Dashboard creates the review the next time it loads the package.

## Review Decisions

Approve and Reject apply to the review as a whole (the full reviewed commit
range across all changed files), not to individual diffs or tabs.

Rejecting a review means no further action is taken on the reviewed commit —
the update is not merged, built, or published. Rejection does not delete or
revert the local repository; it simply records the decision. A future
upstream commit may be reviewed independently.

A rejected package offers no further in-app action until a Fetch brings a
newer upstream commit (§ State Transitions and Action Availability, T8). A previously published
version remains installable with plain `pacman -S`.

## Merge

Merge is **fast-forward only**: local `master` advances to the reviewed
upstream commit, and nothing else. If local `master` has commits of its own
(made outside aur-curator — see § Local Commits Not on Upstream), the
fast-forward is impossible and Merge fails with a clear message naming how
many there are; the user resolves it in git (rebase or reset) outside the
app. Conflict resolution is deliberately not built into the TUI at this scale.

Merge must fast-forward to `Review.UpstreamCommit` — the commit that was
actually reviewed and approved — not to the moving `upstream/master` ref, so
that only reviewed commits can ever be merged. Merge never pushes local
commits as part of a review: if `master` already contains the reviewed commit,
nothing is merged, and the push is retried only when `master` is exactly at
that commit.

For an imported package, Merge creates local `master` for the first time
(from the reviewed upstream commit) and performs the first push to `origin`.

## Local Commits Not on Upstream

Local `master` is a normal git repository, so commits can reach it outside
aur-curator. They have not been through Review. The responsibility for them
is the user's: the app points them out wherever they matter, never blocks on
them, and never claims more than git can establish.

- The app counts commits on `master` that are not on upstream
  (`git rev-list --count upstream/master..master`) and shows them on Package
  Details ("N local commit(s) not on upstream") and in the Dashboard's Local
  column ("N"). Import's placeholder commit (empty tree) is never
  counted.
- The wording is "not on upstream", not "unreviewed": git cannot distinguish
  a local edit from a previously reviewed upstream commit that the AUR later
  dropped by force-pushing. Both are real divergence from upstream.
- If the count cannot be determined, the app says so ("?" in the
  Dashboard's Local column, a warning row on Package Details). A failed
  check is never shown as 0 and never hides the indicator.
- Build requires explicit confirmation while N > 0, or when the check
  failed, on the Dashboard and Package Details alike: "This build includes N
  local commit(s) not on upstream. Build anyway?" The check runs live at the
  key press, not from the value shown on screen. This is one stage of the
  Build confirmation flow (§ Build Confirmation).
- The build's activity-log line records the count actually built
  (`localCommits`: the count, or `unknown`).
- Merge cannot fast-forward past them (§ Merge).

## State Transitions and Action Availability

This section is the single source of truth for package lifecycle
transitions and for which domain actions are enabled in each state, across
the Dashboard, Package Details, and Review screens. Dashboard and Package
Details must share identical conditions — any divergence between them is a
bug, to be fixed against this section, not per-screen discretion. Today
each screen implements these conditions itself (`developer-guide.md`
§ Lifecycle and Action Availability); one shared implementation of this
section is proposed for v2 (#404).

What each state *means*, and how it is derived, is defined in `domain.md`
§ Package → Lifecycle. In short: before the reviewed upstream commit is
merged, the state comes from the review (Review Pending / Approved /
Rejected). After it is merged, Built / Confirmed / Published
describe the **last successful build of local `master` HEAD** — a build of
any earlier commit does not count, and a running, failed, or cancelled build
never changes the state.

> **Implementation status (2026-10-17):** implemented. Published is a
> stored flag on the last success, not derived from the package
> repository — a deliberate trade-off, see § Published: Stored Flag, Its
> Limits, and Drift Detection.

### Transition Matrix

"Same state" means the package stays in the state it was in. "Last success"
is the most recent successful build; "HEAD" is local `master` HEAD.

| # | State | Action | Guard | System action | On success → | On failure / cancel → |
| --- | --- | --- | --- | --- | --- | --- |
| T1 | Not curated | Import | — | Create Gitea repo, clone AUR as `upstream`, add `origin`, empty local `master` | Review Pending | Not curated, partial-failure report [1] |
| T2 | Any | Fetch | `upstream/master` after the fetch differs from the stored review's upstream commit, or there is no review | Update `upstream/master`; replace the current review with a new Pending review | Review Pending | Same state [2] |
| T3 | Any | Fetch | `upstream/master` equals the stored review's upstream commit | Update `upstream/master` (nothing new) | Same state | Same state |
| T4 | Review Pending | Analyze (Review screen) | — | Run analyzers on the review's stored range, record findings | Review Pending | Review Pending [3][15] |
| T5 | Review Pending | Approve | AutoMerge off | Record Approved + optional reviewer notes | Approved | Review Pending [4][15] |
| T6 | Review Pending | Approve | AutoMerge on | Record Approved + notes, then Merge (T9) | Merged | Approved — merge failed, reported [5]; Review Pending if refused [15] |
| T7 | Review Pending | Reject | — | Record Rejected + optional rationale | Rejected | Review Pending [15] |
| T8 | Rejected | (none but Fetch) | — | — | — | — [6] |
| T9 | Approved | Merge | — | Fast-forward local `master` to the reviewed commit, push to `origin` | Merged | Approved — merge failed, reported [7] |
| T10 | Merged | Build | — | Build HEAD | Built (new last success, unconfirmed) | Merged [8] |
| T11 | Built | Confirm | Last success = build of HEAD | Mark last success Confirmed | Confirmed | Built; refused if guard fails [9] |
| T12 | Confirmed | Publish | Last success = build of HEAD | Copy artifacts to the package repository, `repo-add`, mark last success Published | Published | Confirmed, repository directory restored [14]; refused if guard fails [9] |
| T15 | Built, Confirmed, Published | Build (rebuild) | Last success = build of HEAD | Rebuild prompt, then build HEAD | Built (new last success, unconfirmed) | Same state [11] |
| T16 | Any | Cancel (`^T`) | An operation is running | Abort the running operation | Same state | — [12] |
| T17 | Any | Remove (CLI) | — | Report whether installed, delete Gitea repo, build dirs, store records, local clone | Not curated | Partial, each step reported [13] |

T13 and T14 (Install, reinstall) were removed with the Install feature
(#351); the remaining row numbers are kept so existing references stay
valid.

Notes:

1. No automatic rollback; the user is told which steps completed (§ Partial Import Failure). Dashboard Import is deferred (#112). The first review shows a fully additive diff.
2. Replaces a Pending, Approved, or Rejected review. Local HEAD and the last success are unchanged. The AUR card can signal before Fetch that the AUR has moved past the reviewed commit (§ AUR Version Check). The comparison is with the upstream commit the stored review covers (`Review.UpstreamCommit`), not with the package version and not with where `upstream/master` was before this fetch — so a new commit without a version bump still starts a review, and it holds however `upstream/master` got there: this Fetch, an earlier `update`, or a `git fetch` outside the app (#405).
3. Changes only the findings status (Not Run / 0 / N), which selects the Approve prompt text (`tui-review.md` § Confirmation Flows). Analyze runs on the review the Review screen shows; it does not fetch and never starts a review (#399). If any analyzer fails, or the run is cancelled, nothing is recorded: the findings status keeps its previous value and the operation fails with an error naming the analyzer (#410).
4. The Approve modal is always shown; its notes go to `Review.ReviewerNotes`.
5. The Approved decision is recorded even when the merge fails.
6. The only way out of Rejected is a Fetch bringing a newer upstream commit (T2). A previously published version stays in the local package repository and remains installable with plain pacman (§ Installation). A rejected change can be fixed locally only outside the app (§ Local Commits Not on Upstream).
7. Fast-forward only (§ Merge), to the reviewed commit. Local commits not on upstream make the fast-forward impossible.
8. A failed or cancelled build leaves the state unchanged. A failure is shown through the latest attempt's status (build status column, Failed card); a cancelled build is not recorded at all (`domain.md` § Build → Cancellation). Build goes through the Build confirmation flow (§ Build Confirmation), identically on the Dashboard and Package Details.
9. Confirm and Publish act only on the last success whose commit equals HEAD. If it does not match — e.g. a merge moved HEAD — the operation refuses with a clear message, records nothing, and logs the refusal. The UI also disables these actions, but the guard is enforced in the operation itself, not only in the UI.
10. (Removed with Install, #351.)
11. The previous last success is untouched until the rebuild succeeds, so a running or failed rebuild never demotes the package; its published artifact stays in the package repository until a new one is published. A successful rebuild always needs a fresh Confirm. See § Rebuild Confirmation.
12. A cancelled build is not recorded (#304); every other cancelled operation leaves no state change.
13. CLI only. aur-curator does not uninstall the package; it reports whether it is installed. The shared package repository is deliberately untouched (§ Remove Mechanics).
14. Publish records every change it makes to the repository directory and undoes them all if any copy or `repo-add` fails (#379), so a failed publish never leaves a file that differs from the database entry naming it. Per artifact: a file already there with the same content is left alone; a different file with the same name (a rebuild of the same commit) is renamed to `<file>.aur-curator-backup` first, and put back on failure or deleted on success; a new file is removed again on failure. New content goes in through a temporary file (`.aur-curator-publish-*`) renamed into place, so a file is never half-written. If restoring fails, the error says so and `check` reports the mismatch. A backup that already exists was left by a publish that was killed before it finished; Publish deletes it (error log entry) and goes ahead, since it is about to replace the file and the database entry anyway. If that publish fails too, the restored file may not be the one the database names: `check` reports it, and publishing again fixes it. A stray `.aur-curator-publish-*` temporary file left by a kill is never named by the database and can simply be deleted.
15. Analyze, Approve and Reject act only on the review the Review screen shows: each passes that review's ID, and if the stored review has a different one — a Fetch replaced it after the screen loaded it — the action is refused, records nothing, and is logged (activity log and error log, like note 9's refusals). The guard is enforced in the Review Engine (`RunAnalyzers`, `RecordDecision`), not only in the UI. The Review screen stays open, reloads the stored review, and shows the refusal as its error banner (#399).

### Dashboard / Package Details

Derived from the matrix. These two screens share the same domain-action set
and conditions.

| Action | Key | Enabled when | Matrix rows |
| --- | --- | --- | --- |
| Open (Dashboard) | `Enter` | a row is selected | — |
| Open Review (Package Details) | `Enter` | the package has a review | — |
| Fetch | `f` | always | T2, T3 |
| Merge | `m` | `state == Approved` | T9 |
| Build | `b` | `state ∈ {Merged, Built, Confirmed, Published}` | T10, T15 |
| Confirm | `c` | `state == Built` | T11 |
| Publish | `p` | `state == Confirmed` | T12 |
| Cancel | `^T` | an operation is running (global, App-level) | T16 |
| Refresh (Dashboard only) | `r` | always | — |
| Cards, Sort, Search (Dashboard only) | `^PgDn`/`^PgUp`, `^O`, type | always (Sort on the Packages card only) | — |
| Tabs (Package Details only) | `^PgDn`/`^PgUp` | always; the Build Log tab says "No builds yet" when there is none | — |
| Help | `?` | always | — |
| Back (Package Details only) | `Esc` | always | — |

Analyze is on the Review screen only (§ Review).

Merge and Build depend on whether the **reviewed commit** is in `master`
(`Git.IsMerged(Review.UpstreamCommit)`, the same check the lifecycle uses),
never on where upstream's tip is: a merged package whose upstream has moved
on can still be built, and has nothing to merge (#406).

Package Details has no Refresh: it reloads on every arrival and after each operation on its package. The Dashboard's Refresh re-scans the catalog and re-runs the AUR update check.

With `Confirmed` a distinct state, Confirm and Publish follow from the state
alone; no build flags are consulted for availability.

### Review

| Action | Enabled when |
| --- | --- |
| Switch tab (`^PgDn` / `^PgUp`) | always — the `.SRCINFO`, `.install` and Patches tabs are enabled only when the review changes a file of their kind (on a first review, when the package has one), never based on lifecycle state (`tui-review.md` § Tab Bar, #385) |
| Run Analyzers (`a`) | `Review.Status == Pending` |
| Approve (`Enter`) | `Review.Status == Pending` |
| Reject (`^X`) | `Review.Status == Pending` |
| Help (`?`) | always |
| Back (`Esc`) | always |

Reject uses `^X`, not a plain letter, so a decision is never taken by a
stray keypress.

### Build Confirmation

Pressing Build (T10, T15) on the Dashboard or on Package Details runs the
same confirmation flow, from one shared implementation; any difference
between the two screens is a bug. The stages run in order. Each either
passes silently or asks a Y/n question; answering no (or Esc) at any stage
ends the flow with nothing built and nothing recorded. Every check runs live
at the key press, never from a value shown on screen.

1. **Uncommitted changes.** If the working tree has uncommitted changes:
   *"You have uncommitted changes that won't be included in this build —
   Build always uses the last committed version. Continue?"* If the status
   check itself fails, this stage is skipped — Build uses the committed tree
   either way — and the remaining stages still run.
2. **Local commits not on upstream** — § Local Commits Not on Upstream.
3. **Rebuild** — § Rebuild Confirmation.

When every stage has passed, the build is requested.

### Rebuild Confirmation

Rebuild (T15) is a mechanical repeat of the build step against the same
already-reviewed, already-merged commit — it does not reopen or re-derive any
trust decision, since the last success already records exactly which commit
was built. When `Build` is pressed and the last success is the build of HEAD:

- If the existing artifact's checksum still matches what's on disk:
  soft confirmation, not a block — *"A verified build already exists,
  rebuild anyway?"*
- If the checksum no longer matches (corruption, manual tampering,
  etc.): different messaging, still not a block — *"A build already
  exists but appears corrupted. Rebuild recommended. Proceed?"*
- If the last success is for an older commit (HEAD has moved): proceed
  silently — this is an ordinary new build (T10), not a rebuild.

Until the rebuild succeeds, the previous last success stays in effect: a
running or failed rebuild leaves the state unchanged.

This is integrity verification of the existing artifact, not a
reproducibility check — Linux builds (`makepkg` in particular) are not
expected to produce byte-identical output across runs even for an
unchanged commit, so a *new* rebuild's checksum is never compared
against the *old* one.

## Published: Stored Flag, Its Limits, and Drift Detection

"Published" is recorded as a stored flag on the last-success build record
(`Build.Published`), set only after `repo-add` succeeds (T12). The local
package repository itself is the real source of truth; the flag is the
app's working copy of it. Lifecycle, the Dashboard, and action
availability all read the flag. This is a deliberate trade-off (#317,
deferred), not an oversight.

**Why a stored flag is acceptable here:** the app never removes anything
from the package repository (no `repo-remove`, no pruning; Remove leaves it
untouched, § Remove Mechanics), and only one machine publishes to it.
Under those conditions the flag can only go stale through rare failures or
through actions outside the app.

**Known ways the flag and the repository can disagree:**

- **In the repository, not marked published.** The flag save failed after
  a successful `repo-add`; the app was killed between the two; or a
  multi-artifact `repo-add` (e.g. main + `-debug`) registered only some
  files. Harmless for installing.
- **Marked published, not in the repository.** Only through actions
  outside the app (manual pruning, rebuilding the repository database).
- **Repository file does not match its database entry.** `pacman -S`
  then fails its checksum check. Publish replaces a same-named file (a
  rebuild of the same commit produces the same filename) before
  `repo-add`, but undoes the replacement if `repo-add` fails (T12, note 14),
  so this needs Publish to be killed between the two, a failed restore
  (reported in Publish's error), or external damage.

**Detection:** `check` reports all three (§ Check (Consistency), check 3),
by matching each artifact's `checksum` against the database's
`%SHA256SUM%` and each repository file against its database entry. It
never changes the flag or the repository.

**Remedies:** in the repository but not marked → Confirm (if Built), then
Publish; registering an already-registered file again is harmless. Marked
but missing, or file/database mismatch → rebuild, Confirm, Publish (or
re-run `repo-add` by hand with the right file). A removal that was
intended → `remove` the package.

**Revisit (#317) if:** a second machine starts publishing to the shared
repository, or repository pruning/management is built (`roadmap.md`
Phase 4). Either makes the flag go stale routinely, and "published" should
then be derived from the repository (matching `%SHA256SUM%` against
`Artifact.Checksum`) instead of stored.

## Installation

aur-curator never installs or uninstalls packages (#351). Publish (T12)
places a package in the local package repository; from there it is
installed and upgraded with plain pacman, like any other repository:

- configure the local package repository in `/etc/pacman.conf`;
- `pacman -S <pkg>` installs a curated package, `pacman -Syu` upgrades
  curated packages together with the rest of the system.

The same applies on every machine that uses the shared repository, which
is how deployment to additional machines works. Installation needs root
and belongs to pacman; duplicating it in a TUI would add a sudo prompt and
nothing else.

## Remove (Decurate a Package)

Remove tears down an already-imported curated package entirely — the
inverse of Import. It is **not a lifecycle state**: there is no `Removed`
value in the lifecycle state machine shown above, because Remove doesn't
transition a package forward or backward through review — it removes the
package from curation altogether. A removed package simply stops
existing as a curated entity; if the user wants it back, they Import it
again from scratch, starting a fresh review history.

Remove is a CLI-only operation (`aur-curator <config-file> remove
<package-name>`), not exposed in the TUI.

### Remove Mechanics

Remove is owned by the CLI entry point directly (not the Git Provider,
unlike Import) and performs the following as one best-effort operation,
after an explicit interactive confirmation:

1. Check whether the package is installed, with `pacman -Q <pkg>` (no
   root needed). aur-curator does not uninstall it, just as it does not
   install it (§ Installation). If it is installed, Remove's summary ends
   with *Still installed: run `sudo pacman -Rs <pkg>`*, and the activity
   log records `installed=yes`. If the check itself fails, the summary says
   so ("could not check whether <pkg> is installed") — it is never
   silently skipped. An installed package that is no longer curated
   simply receives no further updates from the shared repository;
   removing it is the user's call.
2. Delete the personal Gitea repository via its API.
3. Delete every build directory the package’s records reference under 
   .aur-curator/builds/ — the latest attempt, the last success and the last failure.
4. Delete the package's store records: its Review and Build records, the
   `.aur-curator/packages/<pkg>/` directory that held them, and each of
   its Artifact records (`.aur-curator/artifacts/<artifact-id>/`).
5. Delete the local git clone (the workspace package directory).

Each step is attempted independently; a failure in one does not abort
the others, matching Import's own partial-failure philosophy at this
scale (~10-12 packages) — see § Partial Remove Failure below.

**The shared local pacman repository is deliberately untouched.** Remove
does not call into the Publisher, does not run `repo-remove`, and does
not delete the package's artifact file from the repository directory.
This is a considered exclusion, not an oversight: `prd.md`'s own
rationale for the local pacman repository — rollback capability and easy
deployment to additional machines — is independent of whether a package
is still actively curated. A package removed from curation remains
installable from the shared repository (via plain `pacman -S`, with no
aur-curator involvement) until the repository itself is separately
pruned, which Remove does not do.

### Partial Remove Failure

If any step above fails — the Gitea API is unreachable, the local
directory can't be deleted due to a permissions issue — aur-curator does not attempt automatic rollback of the
steps that already succeeded, for the same reason Import doesn't:
manual verification and cleanup is preferable to complex rollback logic
at this scale. Each outcome (success, failure, or skip) is recorded to
the activity log and, for failures, the error log, so the user can see
exactly which steps completed and which didn't. Silence about partial
state is not acceptable here either — the same standard as § Partial
Import Failure above.

## AUR Version Check

The `update` command and the Dashboard (Available column, AUR card) both
use this check. For every curated package, it tells you whether the AUR
has changed the package's build recipe since the version you reviewed,
which means a Fetch would bring something new to review. It does this
with one request to the AUR, without fetching any repository.

### Terms

- **AUR repository**: the small git repository the AUR keeps for each
  package (`https://aur.archlinux.org/<pkg>.git`). It holds the build
  recipe: the PKGBUILD, `.SRCINFO`, and any patches or install scripts.
  An **AUR commit** is a commit in it.
- **Project source**: the code the recipe builds, which lives elsewhere
  (GitHub, the project's own site, …). A **source commit** is a commit
  there.
- **Version string**: the version as the AUR shows it,
  `[epoch:]pkgver-pkgrel`, for example `1.4.4-2`, `1:1.4.4-2` or
  `r123.abc1234-1`. It is built from the PKGBUILD's `epoch`, `pkgver` and
  `pkgrel` assignments; the epoch is left out when it is absent or 0.
  `pkgver` alone is only one part of it.

In aur-curator, **upstream** means the AUR repository, not the project
source: the AUR remote of each package repository is called `upstream`,
and a review's upstream commit is an AUR commit.

### The Problem

The AUR changes recipes all the time. To know which curated packages
have something new to review, aur-curator could fetch every AUR
repository, but that is slow and touches every repository. The check
answers the same question more cheaply: is the AUR's version string for
this package different from the one in the commit you reviewed?

It does not tell you whether the project has made a release or has new
source commits (§ VCS Packages, § Limits), whether the installed package
is current (that is pacman's job, § Installation), or what is left to do
with the reviewed commit (the lifecycle state and its Next Action show
that).

### How It Works

#### AUR Version

One request to the AUR's API (RPC v5 `info`) covers every curated
package. For each package the AUR returns the version string from the
`.SRCINFO` of its latest AUR commit; every push to the AUR must carry a
`.SRCINFO`. The request has a 15-second timeout.

#### Local Version: Git-Committed, Not Working Tree

The **local version** is the version string from the PKGBUILD at the AUR
commit your current review covers, whatever the review's state: Pending,
Approved, Rejected, or Approved and merged. It is read from git, never
from the PKGBUILD file on disk. A package with no review has no local
version.

The version string is built from the first plain `epoch`, `pkgver` and
`pkgrel` assignments in the PKGBUILD, indented or not. It cannot be read
when `pkgver` or `pkgrel` is missing, or when `epoch`, `pkgver` or
`pkgrel` is computed rather than written out: its value contains a `$` or
a backtick, and aur-curator does not run shell code. A `pkgver()` function
makes no difference; § VCS Packages says why.

**Why the reviewed commit, not local `master`:** `master` holds only what
has been merged. Comparing it with the AUR mixes two things: the AUR
moving, and reviewed work not merged yet. An approved review that is not
merged already covers the AUR's version, so a comparison with `master`
would offer a Fetch that finds nothing, on every check; a rejected review
would be offered for as long as the rejected version stays current on the
AUR.

**Why committed, not the file on disk:** Build clones local `master` into
a fresh directory and never reads the working tree, so an uncommitted
edit to a PKGBUILD has no effect on what is built. It has not been
reviewed either; reacting to it would mean reacting to unreviewed
changes. So an uncommitted edit is, by design, invisible to the check.

#### Comparison

The check only asks whether the two version strings are the same
(ignoring case). It does not ask which is higher. The reviewed commit is
one of the AUR repository's own commits, so if the AUR now shows a
different version string, the AUR has a later commit, even when that
version looks lower (a maintainer rolling `1.5` back to `1.4`, say).

#### Results

Every package gets exactly one result:

| Result | When |
| --- | --- |
| Stale | Both version strings are known and differ: a Fetch brings a new AUR commit |
| Up to date | Both are known and the same |
| Not on the AUR | The AUR does not know the package (deleted, renamed, or never there) |
| Version not readable | The PKGBUILD was read, but its version string cannot be (§ Local Version). True on every check until the recipe changes |
| Local version failed | No review, the review cannot be read or names no commit, or the PKGBUILD at that commit cannot be read (a timeout included) |
| Request failed | The AUR request failed (network, HTTP error, bad response, timeout). Every package is unknown |

Anything unknown is never shown or counted as up to date.

### VCS Packages

#### What a VCS package builds

A VCS package (`foo-git`, `-hg`, `-svn`, …) points its source at a
version-control repository without a fixed revision, for example
`git+https://example.com/foo.git`. `makepkg` therefore builds the latest
source commit of the project's default branch: its current development
state, not a release. A source pinned to a tag (`#tag=v1.2.3`) builds a
fixed release; such a package is normally named `foo`, not `foo-git`.

#### Where its version string comes from

A VCS PKGBUILD has both a plain `pkgver` assignment and a `pkgver()`
function. The function works out a version for the source being built,
for example `r123.abc1234` (commit count and short hash) or
`1.2.3.r45.gabc1234` (`git describe`: 45 commits after tag `1.2.3`).
After running it, `makepkg` writes the result back into the plain
assignment. So the plain `pkgver` in a VCS recipe is whatever `pkgver()`
produced at the maintainer's last build before pushing, and that is what
the AUR shows.

#### When the AUR moves

By AUR convention a VCS package is not pushed just because the project
has new source commits: every build picks them up anyway. A maintainer
pushes when the recipe changes (dependencies, build steps, sources), and
the plain `pkgver` usually changes too, because they built before
pushing.

#### What the check does

It compares the plain `pkgver` like any other package's and ignores the
`pkgver()` function. Both sides are recipe values from the same AUR
repository, so a different version string still means a later AUR
commit: a recipe change, which is what Review is for.

#### Rejected alternative

Treating a VCS package's version as not readable. `update` would then
never offer a VCS package, and its recipe changes would only be found by
fetching it by hand.

#### How yay and paru handle them

By default they compare the installed version, which `pkgver()` produced
at build time, with the AUR's plain value. The installed one is usually
ahead, so a VCS package is rarely offered. With `--devel` they run
`git ls-remote` on the PKGBUILD's source repositories and offer a rebuild
when the project's latest source commit differs from the one recorded at
install time (yay keeps these in `~/.cache/yay/vcs.json`;
`yay -Y --gendb` seeds it). That answers "has the project's source
moved?", which aur-curator does not ask.

### Limits

- **An AUR commit that does not change the version string** is not seen,
  for any package. VCS packages are more prone to it: a maintainer who
  pushes a recipe fix without rebuilding keeps the old `pkgver`. A Fetch
  still finds it, because Fetch compares commits, not versions
  (§ Transition Matrix, note 2). #442 would let the check compare commits
  too.
- **New source commits of a VCS package** are not seen: the AUR has
  nothing new. Building a VCS package compiles the project's latest
  source commit at Build time, which no review has covered, whatever its
  `pkgver` says. That is a trust question rather than a version-check
  one, for the threat model (post-v1 plan, D). A release package (`foo`),
  where one exists, avoids it.
- **The AUR unreachable** gives "Request failed" for every package;
  nothing reads as up to date.

### Where It Is Used

- **`update`**: § Update. § Scan says how each result is reported and how
  it affects the exit status.
- **`outdated`**: § Outdated, a read-only list of the stale packages.
- **Dashboard**: the Available column and the AUR card
  (`tui-dashboard-wireframe.md` § Available). The check runs when the
  Dashboard starts and on Refresh (`R`). A failed request is written to
  the error log. The Dashboard's State column also shows versions, read
  from git at the review's commits or at local `master` HEAD
  (`tui-dashboard-wireframe.md` § State). They are for display only and
  never take part in this comparison.

New code that needs a local version uses the same code. If the version on
local `master`, or on disk, is ever genuinely needed, it is a separate,
explicitly named concept, never substituted for "local version".

In the code: `internal/aurinfo`. `Client.BulkCheck` makes the request and
the comparison; `NewGitLocalVersionResolver` reads the local version
(`git.Provider.FileAtCommit`); `ExtractFullVersion` builds the version
string. Local reads run under `git.GitTimeout`.

### Decision History

| Issue | Decision | Why |
| --- | --- | --- |
| #331 | Unknown never reads as up to date | A failed or partial check must not hide an update |
| #402 | Every package gets one result, with a reason when unknown | Nothing is silently skipped |
| #426 | Compare with the reviewed commit, not local `master` | `master` mixes the AUR moving with reviewed work not merged yet |
| #433 | Compare same/different, not higher/lower; build the version string as the AUR does; computed values are unknown | The reviewed commit is one of the AUR's own; aur-curator does not run shell code |
| #434 | VCS packages compare their plain `pkgver`; `pkgver()` is ignored | A different version string is still a later AUR commit. Before #434 the code meant to treat `pkgver()` as not readable, but only noticed it on the PKGBUILD's first line, so real VCS packages were already compared this way |
| #442 | (Open) compare AUR commits with `git ls-remote` | Closes the "version string unchanged" gap |

## Update

`aur-curator <config-file> update` is the daily step after
`pacman -Syu`: it checks every curated package against the AUR, offers a
Fetch where the AUR has moved past the reviewed commit, and ends by
saying what happened. Its exit status tells a wrapper whether there is
anything to review. It is CLI-only, takes the workspace lock, and never
installs anything.

### Scan

One AUR request covers every curated package; each package's local
version is read as in § AUR Version Check. If the catalog cannot be read or
the AUR request fails, `update` stops before asking anything (exit 1).

Each package ends the scan as one of:

| Outcome | When | Reported |
| --- | --- | --- |
| Up to date | The AUR has not moved past the local version | Counted |
| Stale | The AUR has moved past the local version | Offered (§ Offer) |
| Not checked | Version not readable, or not on the AUR (§ AUR Version Check, Results) | Named with the reason. Does not affect the exit status: it is a property of the package, true on every run, not a failure of this one. |
| Check failed | Local version failed (§ AUR Version Check, Results); or, for an approved review, whether its commit is merged cannot be checked | Named with the reason. Exit 1. |

A package whose merge check failed is still offered if it is stale: it
is treated as not merged (fail-closed) and asked about on its own
(§ Offer), and the failure is named.

A package with a pending review that the AUR has not moved past prints
`<pkg>: review pending; it covers the AUR's current version.`

### Offer

A Fetch that brings a new upstream commit replaces the stored review with
a new Pending one (§ Transition Matrix, note 2). Stale packages are
offered in two groups, by what that would discard:

1. **Asked one at a time, with a warning** — a pending review (its notes
   and findings), or an approved review not merged yet (the approval).
   Prompt: `[y/n/q]`.
2. **Nothing to discard** — approved and merged, or rejected. Listed
   first when there is more than one, then asked one at a time. Prompt:
   `[y/n/a/q]`.

| Answer | Meaning |
| --- | --- |
| `y`, `yes` | Fetch this package |
| `n`, `no` | Skip this package |
| `a`, `all` | Group 2 only: fetch this package and every remaining one in group 2 without asking |
| `q`, `quit` | Skip this package and every remaining one, in both groups |

Answers are case-insensitive. Anything else is asked again with a hint.
End of input (EOF) is `q`. Any other error reading the answer is `q` and
a failure (exit 1). Packages skipped by `q` are named:
`Aborted. Skipped: a, b.`

Each Fetch prints its outcome: a review was started for the new commit,
there was no new upstream commit, or it failed with the reason. A Fetch
that ran but could not read the upstream commit (no review step ran)
counts as failed.

### Summary and Exit Status

The last line answers: did anything change, and is there anything to
review?

- Every package up to date (nothing stale, not checked or failed):
  `All N curated packages match the AUR.`
- Otherwise one line, with only the non-zero parts, in this order:
  `Fetched 2 (new reviews pending: yay, broot); 1 skipped; 1 failed; 1 not checked; 3 up to date.`
  "Fetched" counts Fetches that completed; the names are those that
  started a review. "Skipped" counts packages answered `n` or skipped by
  `q`.

Before the summary line, every failed package is named with its reason
(stderr), and every not-checked package with its reason (stdout).
"Update complete." is no longer printed.

| Exit | Meaning |
| --- | --- |
| 0 | Completed. Nothing failed and no curated package has a Pending review. |
| 10 | Completed. Nothing failed, and at least one curated package has a Pending review when `update` ends: started by this run, already pending, or stale and not fetched. Open the TUI to review. |
| 1 | A check or a Fetch failed, reading an answer failed, or `update` could not start (workspace lock held, catalog unreadable, AUR request failed). Takes precedence over 10. |
| 2 | Usage error. |

An approved review that is not merged does not make the exit 10: what is
left there is a merge, not a review.

## Outdated

`update` is the only other way to ask which curated packages the AUR has
something different for, and it is interactive and fetches. Scripts need
the answer alone: a status bar module (waybar, polybar) that runs a
command every few minutes and shows its output, a shell greeting, a
timer. `aur-curator <config-file> outdated` prints that list and changes
nothing. It is the counterpart of `yay -Qua`.

### What It Does

It runs `update`'s scan (§ Update, § Scan) and stops there: one AUR
request, and local reads only. It never fetches, creates or changes a
review, writes a store, or writes the error or activity log. Like every
other command it takes the workspace lock, so it does not run while the
TUI or another command is open (exit 1, with the usual message).

### Output

One line on stdout per stale package, in catalog order:

```text
pfetch 1.11.0-1 -> 1.12.0-1
yay 12.4.1-1 -> 12.4.2-1 (older review pending)
broot 0.44.1-1 -> 0.45.0-1 (approved, not merged)
```

- The left version is the reviewed commit's, the right one the AUR's
  (§ AUR Version Check). They differ; the right one is not necessarily
  higher.
- The marker says what a Fetch would discard (§ Offer): a pending review
  for an older version (`older review pending`), or an approved review
  not merged yet (`approved, not merged`). Merged and rejected reviews
  have no marker.
- `->` is ASCII, so scripts can split on it. The number of lines is the
  number of stale packages.

What is not listed:

| Scan outcome | Shown |
| --- | --- |
| Up to date, including a pending review that already covers the AUR's version | Nothing |
| Not checked (version not readable, or not on the AUR) | Nothing. A property of the package, true on every run; `update` and the Dashboard name it |
| Check failed | Its name and reason on stderr |

### Exit Status

| Exit | Meaning |
| --- | --- |
| 0 | Completed, whether or not anything was listed |
| 1 | A package's check failed (the stale ones are still listed), or `outdated` could not start or scan (workspace lock held, catalog unreadable, AUR request failed) |
| 2 | Usage error |

Errors go to stderr only. A status bar running `outdated` every few
minutes while the AUR is down would otherwise fill the error log with the
same line.

### Decision History

| Issue | Decision |
| --- | --- |
| #403 | A separate `outdated` command rather than `update --list`; lists stale packages only, with the Fetch-discard markers; takes the workspace lock like every command; no log writes; exit 0 or 1 |

## Check (Consistency)

`aur-curator <config-file> check` cross-checks the curated package list
against aur-curator's own stored records and build output. It is
CLI-only and takes the workspace lock, so it never runs while the TUI is
open or a build is running.

It fixes some problems automatically and only reports others. It never
touches Gitea or the local git clones. It reads the local pacman
repository (check 3) but never changes it.

| Check | What it looks for | Action |
| --- | --- | --- |
| 0 | Workspace root or `.aur-curator/` missing or not a directory | Fails immediately (exit 1) |
| 1 | A catalog package with no Review record, or an unreadable one | Report |
| 1 | `.aur-curator/packages/<pkg>/` for a package no longer in the catalog | Delete |
| 2 | A Build record listing an artifact ID whose record is missing | Remove the reference. A reference to an artifact record that exists but cannot be read is kept and reported. |
| 2 | Artifact records for packages no longer in the catalog | Delete |
| 2 | Artifact records from builds other than the package's last success | Delete |
| 2 | Artifact records that cannot be read | Report, one finding per record |
| 2 | Build directories under `.aur-curator/builds/` that no build record or artifact record references | Delete. Skipped with a finding if any build or artifact record cannot be read, because then no directory is known to be safe. |
| 3 | A successful build whose artifact file is missing from its build directory | Report |
| 3 | A repository database entry whose file is missing from the repository directory, or whose file's SHA-256 differs from the entry's `%SHA256SUM%` | Report. Remedy: rebuild, Confirm, Publish (or re-run `repo-add` by hand with the right file). |
| 3 | A last success not marked Published, though some or all of its artifact checksums are in the repository database | Report. Remedy: Confirm (if Built), then Publish. |
| 3 | A last success marked Published, though not all of its artifact checksums are in the repository database | Report. Remedy: rebuild, Confirm, Publish; if the removal was intended, `remove` the package. |

The check 3 repository rows are explained in § Published: Stored Flag, Its
Limits, and Drift Detection. A missing repository database means nothing
has been published yet. It is not an error. An artifact with an empty
checksum never matches a database entry. If the repository database or a
repository file cannot be read, the affected comparisons are skipped and
reported as a finding — never counted as a match or a mismatch.

Every deletion and every failure is written to the activity log. Any
finding, or any report-only problem, makes `check` exit 1. Otherwise it
exits 0.
