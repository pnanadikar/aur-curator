# Git Command Walkthrough: How Upstream Changes Reach the Trusted Repository

> Last updated: 2026-10-23
> Status: Active — written from source (`internal/git`, v1.0.0 + main);
> first-review paths verified against a real run, later-merge paths not
> yet (see § Verification).
> Companion docs: `workflow.md`, `domain.md` (§ Review, § Reviewed Range),
> `architecture.md`, `interfaces.md` (§ Git Provider)

Shows every git command and Gitea API call aur-curator runs for a package,
in order, as a human would type them, so the mechanics are legible without
reading Go. Each command maps to a function in `internal/git`
(§ Source mapping). If this document and the code disagree, the code is
right and this document is a bug.

Examples use the package `yay`, workspace root `~/repos/aur`, and a Gitea
organisation `aur` at `https://gitea.example.com`.

---

## The model, in one paragraph

Every curated package is one normal Git repository (not a bare mirror) at
`<workspace root>/<package>`, with exactly two remotes: `upstream` (the AUR
— untrusted) and `origin` (your Gitea repository — the trusted record).
The local branch is always `master`. Nothing from `upstream` reaches
`master` or `origin` except the **specific commit a human approved** in a
Review. aur-curator never fetches from `origin`; it only pushes to it.

---

## How commands run

Every git command runs as `git <args>` with the package directory as its
working directory. Each runs in its own process group, so cancelling an
operation (`Ctrl+T`) kills git *and* its helpers (`git-remote-https`,
`curl`). Errors carry git's stderr, sanitised. Pushes use plain `git` and
your own credential helper; the Gitea token is used only for the two API
calls (create and delete repository). Analyze, Merge and the AUR version
check put a 60-second timeout on their git calls; Fetch puts one 60-second
timeout on its whole sequence (fetch, commit reads, review step) and says
so when it fires (#409); Build runs until done or cancelled; Import as a
whole has a 2-minute timeout.

---

## Scenario 1 — Import: bringing a new package under curation

Starting state: nothing exists — no local directory, no Gitea repository.

**1. Check the package exists on the AUR** (HTTP, not git):

```text
GET https://aur.archlinux.org/rpc/?v=5&type=info&arg=yay
```

A `resultcount` of 0 stops Import with "package not found on AUR"; nothing
has been created yet.

**2. Create the Gitea repository** (HTTP, with the token):

```text
POST https://gitea.example.com/api/v1/orgs/aur/repos   {"name": "yay"}
```

Expects `201 Created`; a `409` means a repository of that name already
exists. The response's `clone_url` (HTTPS) becomes `origin` in step 5.

**3. Clone the AUR repository** (run in the workspace root):

```sh
git clone https://aur.archlinux.org/yay.git ~/repos/aur/yay
```

**4. Rename the AUR remote to `upstream`:**

```sh
cd ~/repos/aur/yay
git remote rename origin upstream
```

This also renames the remote-tracking refs, so `upstream/master` now
exists — no separate fetch is needed after Import.

**5. Add the trusted remote:**

```sh
git remote add origin https://gitea.example.com/aur/yay.git
```

**6. Replace the cloned branch with an empty, unrelated `master`:**

```sh
git symbolic-ref --short HEAD          # the branch the clone checked out, e.g. master
git checkout --detach
git branch -D master                   # that cloned branch, whatever its name
git switch --orphan master
git rm -rf --ignore-unmatch .
git clean -fdx
git commit --allow-empty -m "Initial managed branch"
```

The cloned branch is deleted first because it would collide with the
orphan `master` (the AUR checks out `master`). `switch --orphan` already
empties the index and removes tracked files; `rm` (with `--ignore-unmatch`,
since there is usually nothing left to match) and `clean` are defensive.
The result is a `master` with one commit whose tree is git's well-known
empty tree:

```sh
git rev-parse master^{tree}
# 4b825dc642cb6eb9a060e54bf8d69288fbee4904
```

This **placeholder** is how every later step recognises "nothing reviewed
has been merged yet". It shares no history with `upstream`.

**State after Import:** local repository with both remotes; `master` is
the placeholder; `upstream/master` holds the AUR content. **Nothing has
been pushed** — the Gitea repository is empty until the first Merge.
Import itself also creates the package's first Review (application-level,
see Scenario 2).

**If Import fails partway,** nothing is rolled back. The error lists the
completed steps; whatever was created (the Gitea repository, a partial
local directory) must be removed by hand before retrying (`workflow.md`
§ Partial Import Failure).

---

## Scenario 2 — Fetch and starting a Review

**Fetch** (any time after Import, and before every review):

```sh
git fetch --prune upstream
```
Updates `upstream/master` only. Never touches `master`, never contacts
`origin`. After fetching, the Fetch action compares `upstream/master` with
the upstream commit of the stored review: if they differ — whether
`upstream/master` moved in this fetch or earlier (an `update` run, a
`git fetch` outside the app) — a new Review is started for
`upstream/master`, replacing the stored one (`domain.md` § Review Scope);
if they match, nothing else happens ("already up to date"). The `update`
command fetches through the same function, so it starts reviews the same
way (#405).

**Starting a Review** reads the live comparison of `master` and
`upstream/master`:

```sh
git for-each-ref --format=%(objectname) refs/heads/master   # master's commit, or empty
git rev-parse upstream/master                               # becomes Review.UpstreamCommit
git rev-parse <master>^{tree}                               # placeholder? (empty tree)
```

Then, depending on `master`:

| `master` is | Commits listed | Files diffed | Base stored |
| --- | --- | --- | --- |
| the placeholder | `git log master..upstream/master` (= all upstream history, since the placeholder shares none) | `git diff 4b825dc6… upstream/master` (every file a full addition) | none (first review) |
| real content | `git log master..upstream/master` | `git diff master...upstream/master` (three dots: upstream's changes since the merge base) | `git merge-base <master> <upstream>` → `Review.BaseCommit` |

`git log` runs as `git log --date=iso-strict --pretty=format:... <range>`.
Three dots matter: a two-dot diff would also show commits that exist only
on `master` as if upstream were removing them.

The Review stores the pair **(`BaseCommit`, `UpstreamCommit`)** — the
*reviewed range* (`domain.md` § Reviewed Range).

## Scenario 3 — What the Review screen and analyzers read

They never use the live comparison above. They read the **stored** range,
so a review shows exactly what was reviewed even after `upstream` moves on
or Merge moves `master`:

```sh
git rev-parse --verify --quiet <UpstreamCommit>^{commit}
git rev-parse --verify --quiet <BaseCommit>^{commit}        # if there is a base
git log --date=iso-strict --pretty=format:... <BaseCommit>..<UpstreamCommit>   # or just <UpstreamCommit> on a first review
git diff <BaseCommit> <UpstreamCommit>                      # or 4b825dc6… <UpstreamCommit> on a first review
```

If either commit is missing from the repository, this is an error naming
it. It never falls back to the live comparison, which would show something
other than what was reviewed.

**Run Analyzers** (TUI) runs no fetch and starts no review: it analyses
the stored range of the review the Review screen shows, with the commands
above and nothing else (#399). If a Fetch has replaced that review in the
meantime, Analyze is refused before any git command runs. The CLI's
`autoAnalyzeAfterImport` path does the same for the review Import has
just created.

**Approve and Reject run no git command.** The decision is recorded in the
Review (application level). Merge is a separate action, unless `autoMerge`
is on, in which case Merge follows a successful Approve.

---

## Scenario 4 — Merge (after Approve)

Merge receives one commit: **`Review.UpstreamCommit`, the commit that was
reviewed and approved.** It never merges the moving `upstream/master`
ref, so commits that arrived after the review cannot reach `master` or
`origin` this way.

```sh
git for-each-ref --format=%(objectname) refs/heads/master
git rev-parse --verify --quiet <reviewed>^{commit}
git rev-parse <master>^{tree}                               # placeholder?
```

**First merge** (`master` is the placeholder):

```sh
git reset --hard <reviewed>
git push -u origin master
```

`master` becomes exactly the reviewed commit, and the first content ever
reaches `origin`, with upstream tracking set.

**Later merges** (`master` has real content):

```sh
git merge-base master <reviewed>
```

| Merge base is | Meaning | What runs |
| --- | --- | --- |
| `master` itself | `master` is behind the reviewed commit | `git merge --ff-only <reviewed>`, then `git push origin master` |
| the reviewed commit, and `master` = reviewed | an earlier merge succeeded locally (e.g. its push failed) | `git push origin master` (retry) |
| the reviewed commit, `master` ahead of it | reviewed commit already in `master`, plus your own commits | nothing — no push, so your local commits are never published as part of this review |
| anything else | `master` has commits that are not in the reviewed commit | `git rev-list --count <reviewed>..master`, then stop with "N local commit(s) … resolve in git (rebase or reset) and retry"; nothing changed |

If the push fails after `master` moved, the error says so ("local master
updated … but push to origin failed"); retrying Merge takes the
push-retry row.

After a Reject, no merge happens: the repository, `origin` and `upstream`
are untouched, and a later upstream commit can still be reviewed.

---

## Scenario 5 — Build reads `master`, not `origin`

Before building, the Build action checks the package directory (warnings,
not blocks):

```sh
git status --porcelain --branch                             # uncommitted changes?
git rev-list --count upstream/master..master                # your own commits on master?
```

Build never clones from Gitea. It takes local `master`'s commit and clones
the **local** repository into a fresh, disposable directory under the
workspace's `.aur-curator/builds/<build-id>/`:

```sh
git rev-parse master                                        # the commit to build
git clone --local --no-hardlinks ~/repos/aur/yay ~/repos/aur/.aur-curator/builds/<build-id>/work/src
git -C <that directory> checkout --detach <commit>
```

`--no-hardlinks` makes the copy fully independent: nothing makepkg does in
the build directory can touch the managed repository. Because the source
is the committed `master`, uncommitted edits in the package directory are
never built (`workflow.md` § Local Version). `origin` is the trusted
record and the source for other machines, not the build input.

**Confirm and Publish run no git command that changes anything.** They
read `git rev-parse master` to match the build to the current commit;
Publish works on the local pacman repository, not on git.

---

## Read-only queries

Used by the Dashboard, Package Details and CLI commands. None change
anything. At startup, each package is first validated by reading its
branch and both remote URLs (the metadata row below).

| Question | Command |
| --- | --- |
| Commits behind upstream | `git rev-list --count master..upstream/master` (counts all upstream history while `master` is the placeholder) |
| Your own commits on `master` | `git rev-list --count upstream/master..master` (0 for the placeholder) |
| Is the reviewed commit merged? | `git merge-base --is-ancestor <reviewed> master` — any failure reads as "not merged" (fail-closed) |
| Upstream tip / master tip | `git rev-parse upstream/master` / `git rev-parse master` |
| Working tree state | `git status --porcelain --branch` |
| Remotes and branch | `git branch --show-current`, `git remote get-url origin`, `git remote get-url upstream` |
| A file at a commit | `git show <commit>:<path>` (e.g. PKGBUILD for version checks) |

## Remove (CLI only)

Deletes the Gitea repository through the API — `DELETE
/api/v1/repos/aur/yay`, where `404` counts as already deleted — and the
local directory. No git command is involved. See `workflow.md` § Remove
for the full sequence.

---

## Source mapping

All in `internal/git`.

| Step | Function | File |
| --- | --- | --- |
| Run any git command | `ExecRunner.Run` | `exec.go` |
| AUR existence check | `provider.checkAURPackageExists` | `import.go` |
| Create / delete Gitea repository | `giteaForge.CreateRepository` / `DeleteRepository` | `gitea.go` |
| Clone, remotes, placeholder `master` | `provider.Import` | `import.go` |
| Fetch | `provider.Fetch` | `fetch.go` |
| Read `master` without hiding git errors | `provider.localMasterStrict` | `merge.go` |
| Placeholder check | `provider.isEmptyMaster` | `diff.go` |
| Live comparison (new Review) | `provider.Diff`, `commitsInRange`, `filesInRange`, `mergeBase` | `diff.go` |
| Stored range (Review screen, analyzers) | `provider.ReviewDiff`, `verifyCommit`, `logCommits` | `diff.go` |
| Merge | `provider.Merge` (`NotFastForwardError`) | `merge.go` |
| Build checkout | `provider.CloneAt`, `provider.LocalHeadCommit` | `clone.go` |
| Commits behind, status, metadata, file at commit | `CommitsBehind`, `Status`, `Metadata`, `FileAtCommit`, `UpstreamCommit` | `inspect.go` |
| Local-only commits | `provider.LocalOnlyCommits` | `localcommits.go` |
| Merged check | `provider.IsMerged` | `merged.go` |

---

## Verification

This document is accepted only when its command sequences match a real
run. Capture every git command aur-curator executes:

```fish
set -x GIT_TRACE /tmp/git-trace.log
# import a throwaway package, review, approve, merge;
# then, if upstream has moved, fetch, review, approve, merge again
set -e GIT_TRACE
grep -E 'trace: (built-in|exec|run_command)' /tmp/git-trace.log
```

aur-curator's own `--trace` log also records every git command the runner
issues (`handoff cmd=[...]` lines), with package names on the
surrounding lines; it was used for the verification below.

| Path | Verified 2026-10-21 (v1.0.0 + main) |
| --- | --- |
| Import (AUR check, Gitea create, clone, remotes, placeholder `master`) | yes — iotop-rs, pfetch, visual-studio-code-bin |
| Fetch with nothing new upstream | yes |
| Run Analyzers (stored range only, no fetch; first review) | yes — 2026-10-23, units (#399) |
| Approve (no git) | yes |
| First merge (`reset --hard`, `push -u`) | yes |
| Build pre-checks and checkout (`clone --local --no-hardlinks`, `checkout --detach`) | yes |
| Confirm, Publish (no changing git command) | yes |
| Read-only queries at startup and refresh | yes |
| Fetch with a new upstream commit (new Review, three-dot diff, merge base) | **not yet** |
| Later merge (`merge --ff-only`, push), push retry, already-contained, diverged | **not yet** |
| Stored range with a base (`<base>..<upstream>`) | **not yet** |
| Remove | **not yet** (trace shows only its start) |

The unverified rows need a package whose AUR repository gains a commit
after its first merge.
