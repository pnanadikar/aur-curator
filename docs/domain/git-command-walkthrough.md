# Git Command Walkthrough: How Upstream Changes Reach the Trusted Repository

> Companion to `git-provider.md`, `workflow.md`, and the
> `internal/git` remediation phase documents. Shows the same sequence
> `internal/git` runs programmatically, but as a human would type it,
> so the mechanics are legible independent of Go/test-double
> abstractions.
>
> **Verification status, as of Checkpoint 3:** the Import sequence below
> (Scenario 1) reflects code confirmed and verified through Phase 4 —
> real, tested, done. The Merge sequences (Scenarios 2 and 3) reflect
> `phase05.md`'s design — written, not yet implemented or verified
> against real code. If this session resumes after Phase 5 has actually
> landed, double-check Scenarios 2/3 against the verified `merge.go`
> before trusting this document over the code.

---

## The model, in one paragraph

Every curated package is one normal Git repository with exactly two
remotes: `origin` (the personal Gitea repository — trusted, built from)
and `upstream` (the official AUR repository — untrusted, never built
from directly). The local branch is always `master`. Nothing from
`upstream` reaches `origin` without a human approving a Review in
between — that's the entire point of the application.

---

## Scenario 1 — Import: bringing a new package under curation

Starting state: nothing exists yet — no local clone, no Gitea repo. Using
`yay` as the example package throughout, matching the rest of this
project's docs and tests.

**1. Create the Gitea repository via the Gitea API** (not a `git`
command — an HTTP `POST` to `/api/v1/orgs/aur/repos`). Returns the new
repository's clone URL, e.g. `https://gitea.example.com/aur/yay.git`.

**2. Clone the AUR repository:**

```sh
git clone https://aur.archlinux.org/yay.git /home/user/repos/aur/yay
```

By itself, this names the resulting remote `origin` — pointing at AUR.
That's git's default behavior, not something configured. This is exactly
why the next step exists.

**3. Rename that remote to `upstream`:**

```sh
cd /home/user/repos/aur/yay
git remote rename origin upstream
```

Now `upstream → AUR`. The name `origin` is free again.

**4. Add the real, trusted remote:**

```sh
git remote add origin https://gitea.example.com/aur/yay.git
```

Now both remotes are correctly configured: `origin → Gitea`,
`upstream → AUR`.

**5. Reset local `master` to a genuinely empty state:**

```sh
git switch --orphan master
git rm -rf .
git clean -fdx
git commit --allow-empty -m "Initial managed branch"
```

Walking through why all four commands are needed, not just the first and
last: `switch --orphan master` starts a new, parentless branch — but it
does **not** clear the working tree; the AUR-cloned files are still
sitting on disk and still staged. `rm -rf .` force-removes everything
from both the index and the working tree. `clean -fdx` removes anything
left over that wasn't tracked (build artifacts, ignored files). Only
then does `commit --allow-empty` produce a commit whose **tree** is
genuinely empty — not just a commit made with an empty index by
coincidence, but one you can independently verify:

```sh
git rev-parse master^{tree}
# 4b825dc642cb6eb9a060e54bf8d69288fbee4904
```

That hash is git's single well-known empty-tree object, present in every
repository ever created — not something specific to this project.

**6. Register the package** in the Package Catalog (an application-level
step, not a git command).

**State after Import:** a local repo exists, with a real `master` branch
pointing at one commit — a commit that exists, but whose content is
empty. Both remotes are correctly configured. **Nothing has been pushed
to `origin`** — the Gitea repository is still empty at this point.

---

## Scenario 2 — First review cycle: Fetch, Diff, Merge (first-time)

**Fetch**, run at any point after Import (and again before every future
review):

```sh
git fetch --prune upstream
```

Updates `upstream/master` (and prunes any deleted remote branches).
Never touches `master`, never touches `origin`.

**Diff** — what the Review screen actually shows. Because local `master`
has an empty tree, the application compares against git's empty-tree
object rather than `master` itself:

```sh
git rev-parse upstream/master
git log --pretty=... upstream/master
git diff 4b825dc642cb6eb9a060e54bf8d69288fbee4904 upstream/master
```

Every file in the diff appears as a full addition — there's nothing to
subtract from, since the empty tree has nothing in it. This is
`workflow.md`'s "(new) vs upstream/master" first-review presentation,
and it's genuine, not simulated: the empty-tree comparison produces a
real, fully-additive diff by construction.

**Human reviews, approves.**

**Merge (first-import path)** — per `phase05.md`'s design:

```sh
git rev-parse --verify --quiet master
# returns the empty commit's real SHA — the ref exists

git rev-parse <that-sha>^{tree}
# 4b825dc642cb6eb9a060e54bf8d69288fbee4904 -- matches the known-empty hash

git reset --hard upstream/master
git push -u origin master
```

The tree-hash check is what distinguishes "first review" from "ongoing
review" — not whether `master` exists (it always does, after Import),
but whether its content is still the placeholder. `reset --hard` moves
`master` to point at the exact commit that was reviewed and approved,
and syncs the working tree to match in the same step. `push -u`
establishes the tracking relationship between local `master` and
`origin/master` — this is genuinely the first content `origin` has ever
received.

**State after:** `origin` now holds the reviewed, approved content.
Local `master` and `origin/master` match.

---

## Scenario 3 — Subsequent review cycles: Fetch, Diff, Merge (incremental)

Some time later, the AUR package updates again. **Fetch** runs the same
way as before — `git fetch --prune upstream` — pulling the new upstream
commits into `upstream/master`.

**Diff** — now that local `master` has real content, the comparison uses
the actual range, not the empty-tree fallback:

```sh
git log --pretty=... master..upstream/master
git diff master..upstream/master
```

This shows only what actually changed since the last approved review —
an incremental diff, not a full re-listing of the whole package.

**Human reviews, approves.**

**Merge (subsequent path)** — per `phase05.md`'s design:

```sh
git rev-parse --verify --quiet master
# returns master's current SHA

git rev-parse <that-sha>^{tree}
# some real tree hash, NOT the empty-tree hash

git merge --ff-only upstream/master
git push origin master
```

Because `master`'s tree isn't the placeholder anymore, this takes the
fast-forward path instead of the reset-and-first-push path.
`--ff-only` succeeds here specifically because `master` genuinely is an
ancestor of `upstream/master` at this point (it was set exactly equal to
some earlier point in `upstream/master`'s history by the previous
Merge) — this is the same command that fails when run against a
first-import package's unrelated orphan history, which is exactly why
the branch above exists. The push at the end is what keeps `origin`
current — without it, `origin` would silently fall behind after the
very first update, and since packages are always built from `origin`,
every build after that point would use stale content.

**State after:** `origin` reflects the latest approved review. This
scenario repeats for every future update.

---

## What doesn't happen: Rejection

If the human rejects a review instead of approving it, **no git command
runs at all.** The decision is recorded (application-level, not a git
operation); the local repository, `origin`, and `upstream` are all left
exactly as they were. A future upstream commit can still be reviewed
independently later — rejecting one version doesn't block reviewing a
different, later one.

---

## Where this stops

This document covers everything `internal/git` (the `Provider`
interface) is responsible for: Import, Fetch, Diff, Merge. Build,
Publish, and Install are separate stages, owned by separate modules
(`internal/build`, `internal/publish`), operating on `origin`'s content
*after* Merge has placed it there — out of scope here, and not something
this document should be extended to cover; see `roadmap.md`'s phase
breakdown instead.

---

## Source mapping — which code implements each step

| Step | Function | File |
|---|---|---|
| Create Gitea repo | `giteaForge.CreateRepository` | `gitea.go` |
| Clone + configure remotes + orphan master | `provider.Import` | `import.go` |
| Register package | `RepositoryLocator.Register` (called from `Import`) | `import.go` calls into `internal/catalog` |
| Fetch | `provider.Fetch` | `fetch.go` |
| Resolve local master's SHA | `provider.resolveLocalMaster` | `diff.go` |
| Check for placeholder (empty) master | `provider.isEmptyMaster` | `diff.go` — **per `phase05.md`, not yet implemented** |
| Diff | `provider.Diff`, `commitsInRange`, `filesInRange` | `diff.go` |
| Merge | `provider.Merge` | `merge.go` — **per `phase05.md`, not yet implemented** |
