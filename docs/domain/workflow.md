# Package Lifecycle

> Last updated: 2026-10-18
> Status: Active
> Companion docs: domain.md, tui-review.md, developer-guide.md

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

Import is owned by the Git Provider and performs the following as one
operation:

1. Create an empty repository on the personal Gitea instance via its API.
2. Clone the AUR repository, adding it as the `upstream` remote.
3. Add the new Gitea repository as the `origin` remote.
4. Reset local `master` to an empty/orphan state — no upstream content is
   merged in at this point. This is what makes the "(new) vs
   upstream/master" comparison above genuine: local `master` has no
   content yet, so the diff is fully additive, not merely displayed as
   one.

Import performs no further step after this. There is deliberately no
"register the package" or "refresh the catalog" step: Package Catalog
performs a live filesystem scan on every call rather than maintaining a
cache (`interfaces.md` § Package Catalog; full rationale in
`docs/interface-packets/package-catalog.md` § Design Note: No Cache).
The package is already curated as of step 2 — its directory exists on
disk from that point — so nothing further needs to happen for it to be
picked up; the next call to Package Catalog from anywhere simply sees
it. An earlier version of this document had a step 5 ("refresh the
catalog's cache"); removed once the cache itself was removed, see
`domain-package-review.md` DOM-004/DOM-008. 

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
many there are; the user resolves it in git (rebase or merge) outside the
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
  column ("N local"). Import's placeholder commit (empty tree) is never
  counted.
- The wording is "not on upstream", not "unreviewed": git cannot distinguish
  a local edit from a previously reviewed upstream commit that the AUR later
  dropped by force-pushing. Both are real divergence from upstream.
- If the count cannot be determined, the app says so ("? local" on the
  Dashboard, a warning row on Package Details). A failed check is never shown
  as 0 and never hides the indicator.
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
bug, to be fixed against this section, not per-screen discretion.
`ActionAvailability()` (`developer-guide.md` § Action Availability —
Single-Source Derivation) is this section expressed as code; the two must
never drift.

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
| T2 | Any | Fetch | New upstream commit | Update `upstream/master`; replace the current review with a new Pending review | Review Pending | Same state [2] |
| T3 | Any | Fetch | No upstream change | Update `upstream/master` (nothing new) | Same state | Same state |
| T4 | Review Pending | Analyze | — | Run analyzers, record findings | Review Pending | Review Pending [3] |
| T5 | Review Pending | Approve | AutoMerge off | Record Approved + optional reviewer notes | Approved | Review Pending [4] |
| T6 | Review Pending | Approve | AutoMerge on | Record Approved + notes, then Merge (T9) | Merged | Approved — merge failed, reported [5] |
| T7 | Review Pending | Reject | — | Record Rejected + optional rationale | Rejected | Review Pending |
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
2. Replaces a Pending, Approved, or Rejected review. Local HEAD and the last success are unchanged. The AUR update card can signal a new upstream version before Fetch.
3. Changes only the findings status (Not Run / 0 / N), which selects the Approve prompt text (`tui-review.md` § Confirmation Flows).
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

### Dashboard / Package Details

Derived from the matrix. These two screens share the same domain-action set
and conditions. Package Details additionally has `View Build Log` and `Back`,
which have no Dashboard equivalent.

| Action | Enabled when | Matrix rows |
| --- | --- | --- |
| Open / Open Review | always | — |
| Fetch | always | T2, T3 |
| Refresh (Dashboard only) | always | — |
| Analyze | `state == ReviewPending` | T4 |
| Merge | `state == Approved` | T9 |
| Build | `state ∈ {Merged, Built, Confirmed, Published}` | T10, T15 |
| Confirm | `state == Built` | T11 |
| Publish | `state == Confirmed` | T12 |
| Cancel (`^T`) | an operation is running (global, App-level) | T16 |
| View Build Log (Package Details only) | a build record exists for this package | — |
| Search (Dashboard only) | always | — |
| Back (Package Details only) | always | — |

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
| Back (`Esc`) | always |

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
4. Delete the package's derived store records — Review, Build, Artifact —
   and the `.aur-curator/packages/<pkg>/` directory that held them.
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

#### § Local Version — Git-Committed, Not Working-Tree

Principle: wherever a package's "local version" is needed for a staleness or update comparison, it means the PKGBUILD content committed at local master HEAD, read via the Git provider (LocalHeadCommit + FileAtCommit). It never means the raw file on disk in the managed workspace's working tree.
Rationale

Build (internal/build/engine.go) resolves LocalHeadCommit and clones fresh into a disposable workspace at exactly that commit before invoking the builder — it never reads or builds
from the working tree directly. This means an uncommitted change to a package's PKGBUILD (a manual hand-edit, a half-finished local patch, or any operation that touches the file
without committing it) has no effect on what would actually be built next. A "local version" read from disk can therefore disagree with what Build would actually produce — not a
stylistic inconsistency, but a false signal about real application state.

This mirrors the project's existing Git-as-source-of-truth pattern elsewhere in this document (Review compares master vs upstream/master by commit, not by file diff against an ambient
working state; Approve/Reject records a commit, not a file snapshot). Local version resolution follows the same rule for consistency, and because Build already depends on it being
true. What this means in practice:

Any code that needs "does this package have a newer version upstream than what's locally curated" must resolve local version via:

    provider.LocalHeadCommit(ctx, pkg) — the commit currently at local master.
    provider.FileAtCommit(ctx, pkg, <commit>, "PKGBUILD") — the PKGBUILD content at that commit.
    Parse the version from that content (never from a disk read of the file).

This applies identically to:

    Review-Pending packages — local version is read at review.UpstreamCommit (the commit the pending review is comparing against), not at HEAD, since the review hasn't been decided yet and HEAD may predate it.
    Approved / Merged / Built / Confirmed / Rejected / Published packages — local version is read at local HEAD, since that is the last commit that was actually merged and is what the next Build would use.

An uncommitted working-tree edit to a PKGBUILD is, by design, invisible to every staleness/update check until it is committed. This is intentional, not a gap: an uncommitted edit has
not gone through Review, and treating it as live data would mean the tool reacts to unreviewed local mutations — the same category of thing "Automate mechanics, never automate trust"
already guards against elsewhere in the lifecycle.

##### Known consumers of this rule (keep in sync if more are added)

    update CLI command (parseLocalMasterVersion, cmd/aur-curator/main.go) — implements this correctly today; the reference implementation for the pattern.
    #219's bucket-2 AUR-staleness check in update — implements this correctly for the review-pending case (FileAtCommit against review.UpstreamCommit).
    Dashboard's AUR Update card (deps.AUR.BulkCheck / internal/aurinfo) — currently reads disk directly, in violation of this rule; tracked as #231.

Any future code that needs a package's local version must follow this rule from the start rather than re-deriving its own working-tree read. If a genuine need arises for working-tree
state specifically (distinct from "local curated version"), it must be named and documented as a separate, explicitly different concept — not silently substituted for "local version"
the way #231's bug did.

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
