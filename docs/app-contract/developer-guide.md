# Developer Guide: aur-curator

> Last updated: 2026-10-23
> Status: Active
> Companion docs: `architecture.md`, `interfaces.md`, `domain.md`,
> `workflow.md`, `ui-overview.md`; repository root `DEVELOPMENT.md`
>
> How the application is put together: the composition root, the Bubble Tea
> application, persistence, the operations queue, and the implementation
> rules that came from real defects. `architecture.md` and `interfaces.md`
> state the principles and contracts; `DEVELOPMENT.md` has the code layout,
> make targets and workflow.
>
> This guide describes structure and rules and points at the code. It does
> not copy struct definitions: the code is authoritative, and copied
> listings were what made earlier versions of this guide go stale.

---

1. [Composition Root](#composition-root)
2. [Application Structure](#application-structure)
3. [Screens](#screens)
4. [Messages](#messages)
5. [Operations Queue](#operations-queue)
6. [Lifecycle and Action Availability](#lifecycle-and-action-availability)
7. [Persistence](#persistence)
8. [Implementation Conventions & Gotchas](#implementation-conventions--gotchas)
9. [Known Gaps](#known-gaps)

---

## Composition Root

The application is assembled in two places, and only there:

1. **`cmd/aur-curator/main.go`** loads the configuration, sets up logging,
   and builds the services every command needs: the Package Catalog, the
   Gitea forge and the Git provider. It then dispatches to a command.
2. **`internal/deps.NewDependencies`** builds everything else from the
   configuration and those two services, and returns them as one
   `deps.Dependencies` value. Every command that needs more than Git calls
   it, so the TUI and the CLI commands are wired identically.

No other package constructs a service. Screens, engines and stores receive
what they use through constructors.

### Startup order

```text
config.Load(path)                        WorkspaceConfig (plain function, no provider interface)
  → trace / debug sinks                  --trace, --debug-log, AUR_CURATOR_TRACE, AUR_CURATOR_DEBUG
  → catalog.NewFilesystemCatalog(root)
  → git.NewGiteaForge(baseURL, org, token)
  → git.New(catalog, ExecRunner{}, forge, root)
  → command:
       TUI       lock → NewDependencies → recover interrupted builds → ui.NewApp → tea.Program
       import    lock → provider.Import → NewDependencies → EnsureReview [→ RunAnalyzers]
       remove    lock → NewDependencies → confirm → forge + stores + files
       check     lock → NewDependencies → consistency checks
       update    lock → NewDependencies → AUR check → prompts → FetchAndReview
```

Every command takes `<rootPath>/.aur-curator/workspace.lock` first: one
running aur-curator per workspace.

### What `NewDependencies` builds

| Field | Value | Notes |
| --- | --- | --- |
| `Git` | the provider from `main.go` | interface `git.Provider` |
| `Catalog` | the catalog from `main.go` | interface `catalog.PackageCatalog` |
| `ReviewDB`, `BuildDB`, `ArtifactDB` | JSON stores under `<rootPath>/.aur-curator/` | interfaces declared in `internal/domain` |
| `Analyzers` | `analyzer.New(nil)` | `analyzerConfig` is not used |
| `Policy` | `policy.NewEngine(domain.PolicyConfig{})` | no user overrides are loaded yet |
| `Reviews` | `*review.Engine` | concrete type |
| `Builder` | `build.MakepkgBuilder{}` | always makepkg; `builderBackend` is not read |
| `BuildEngine` | `*build.Engine` (builder, build and artifact stores, Git, paths) | concrete type; screens reach the stores through it too |
| `Repo` | `domain.PackageRepository{Location: repositoryPath}` | |
| `Publisher` | `publish.NewPublisher(...)` | |
| `AUR` | `*aurinfo.Client` | AUR RPC version checks |
| `Config` | the whole `WorkspaceConfig` | read by App and screens (paths, `AutoMerge`) |
| `RepoStore` | `nil` placeholder | unused |

### Principles

- **Constructor injection.** A constructor returns a fully usable value;
  no setters after construction.
- **Consumers define interfaces.** `internal/git` declares `Runner` and
  `Forge`; the store interfaces live in `internal/domain` beside the
  structs they persist.
- **Singleton services, transient records.** Services are built once per
  process. Reviews, builds, findings and artifacts are created as needed.
- **No global state** other than the logging sinks (`trace`, `debug`),
  which are set once in `main.go`.
- **Adding a subsystem** means constructing it in one of the two places
  above and passing it to whatever uses it — never constructing it inside
  another service.

---

## Application Structure

The TUI is one Bubble Tea model, `ui.App` (`internal/ui/app.go`). It owns
navigation, the operations queue and the chrome around the screens: toast,
error line, header, Operations Panel and Status Bar. Each screen is a
sub-model in `internal/ui/screens` that renders only its own area. **Only
App changes the active screen; no screen reads or writes another screen's
state.**

### `Update`

`App.Update` calls `update`, then `syncScreenSize`, which re-forwards the
content-area size to every screen whenever it changed. Chrome changes (a
toast appearing, the panel growing) therefore resize every screen through
one path. `update` handles messages in this order:

1. **Keys App owns.** While the quit-confirm modal is open it gets every
   key. `Ctrl+C` / `Ctrl+Q` quit (through the same guard as below).
   `Ctrl+T` cancels the active operation, or, if none is running, drops the
   pending ones.
2. **Navigation:** `NavigateMsg`, `BackMsg`, `ReviewDecisionBackMsg`,
   `QuitRequestedMsg`.
3. **Toast and error:** `ToastMsg`, the startup notice, toast expiry,
   `ErrorMsg`.
4. **Operation results** (`OperationCompleted/Failed/CancelledMsg`): finish
   the active operation, update its lane, start the next one, show a toast.
   Not returned early: the message also continues to step 6.
5. **Operation requests** (`*RequestedMsg`): enqueue and start if idle (see
   § Operations Queue).
6. **Delivery to screens:**
   - operation results go to **every** screen, so each can refresh its
     derived state whichever screen is active;
   - `PackagesLoadedMsg`, `ReviewDataMsg` and `AURUpdateMsg` always go to
     the Dashboard, even when another screen is active (#286);
   - everything else goes to the active screen only.

### Navigation

- `NavigateMsg` pushes the current screen onto `history` and switches.
  From the Nav Menu (`FromMenu: true`, digit keys) it clears `history`
  instead: menu jumps are absolute, `Esc` does not walk back through them.
  Navigating clears the toast and error.
- `BackMsg` pops `history`. `Changed: true` means the screen being left
  changed something: Package Details reloads, and the Dashboard reloads that
  package's row.
- `ReviewDecisionBackMsg` is the Review screen's Back after Approve/Reject.
  It carries the saved Review, so the Dashboard updates the row from memory
  instead of re-reading it.
- **Quit** always goes through `requestQuit`: it quits at once if no
  operation is running, otherwise it asks first (#179). Screens emit
  `QuitRequestedMsg` and never return `tea.Quit` themselves (#300).
- The Review screen is created on first navigation to a package and reused
  (with `Refresh`) when the same package is opened again.

### `View`

Top to bottom: a reserved toast line, the error line (if any), the header,
the active screen's body, the Operations Panel and the Status Bar. The body
is cut to the rows left for it, so a screen that renders too tall can never
push the header or toast off the top (#329).

---

## Screens

Screen IDs are `msgs.Screen` (`internal/ui/msgs/msgs.go`):

```text
Dashboard ([1])
 ├── Package Details        tabs: Summary · Commits · Build Log · Artifacts
 │     └── Review           (Enter; a separate screen, not a tab)
 ├── Repository Status ([2])
 ├── Settings ([3])         stub: the menu key does nothing yet
 └── Help ([4])
```

| Screen | File | Notes |
| --- | --- | --- |
| Dashboard | `screens/dashboard.go` | Summary cards filter the package list (`^PgDn`/`^PgUp`), `^O` sorts. Row actions: Fetch, Merge, Build, Confirm, Publish, Refresh. |
| Package Details | `screens/detail.go` | Tabs switch with `^PgDn`/`^PgUp` (or `^→`/`^←`). Same actions as the Dashboard for one package; `End` on Build Log reloads the log and follows it. |
| Review | `screens/review_screen.go` and siblings | Diff tabs, Findings, Run Analyzers, Approve/Reject modals (`tui-review.md`). |
| Repository Status | `screens/repostatus.go` | Read-only view of the local pacman repository. |
| Help | `screens/help.go` | Renders `internal/help` (shared with `--help`). |

The Nav Menu (`1` Packages, `2` Repository, `3` Settings, `4` Help, `q`
Quit) appears on every screen except Review, which is a focused task that
ends in a decision and is left with `Esc`. The Build confirmation flow (local commits, dirty tree, rebuild,
`workflow.md`) is one shared component, `BuildGate`, used by both the
Dashboard and Package Details.

---

## Messages

Kept as separate types so `Update` stays a flat switch (see § Distinct
message semantics get distinct types).

| Category | Types | Where |
| --- | --- | --- |
| Navigation | `NavigateMsg` (`To`, `Package`, `FromMenu`), `BackMsg` (`Changed`), `ReviewDecisionBackMsg`, `QuitRequestedMsg` | `internal/ui/msgs` |
| Operation requests | `FetchRequestedMsg`, `AnalyzeRequestedMsg`, `MergeRequestedMsg`, `BuildRequestedMsg`, `ConfirmRequestedMsg`, `PublishRequestedMsg` — each `{Pkg, TraceCtx}` | `internal/ui/msgs` |
| Operation results | `OperationCompletedMsg` (`Result`, optional `Review` / `Build`), `OperationFailedMsg` (`Kind`, `Err`), `OperationCancelledMsg` | `internal/ui/msgs` |
| App-level | `ToastMsg`, `AURUpdateMsg`; `ErrorMsg`, `clearToastMsg`, `startupNoticeMsg` | `msgs`; the last three in `internal/ui` |
| Screen data | `PackagesLoadedMsg`, `ReviewDataMsg` (routed by App to the Dashboard); `detailLoadedMsg`, `buildLogLoadedMsg`, `artifactLoadedMsg`, `PackageSelectedMsg`, … | `internal/ui/screens`, beside the screen that uses them |

Every request and result carries a `TraceCtx`, so one operation's trace
lines share an ID from key press to completion.

**Approve and Reject are not operations.** The Review screen calls
`Reviews.RecordDecision` itself and returns with `ReviewDecisionBackMsg`;
nothing is queued. With `autoMerge` on, Merge follows a successful Approve
automatically.

**The Review screen shows what is stored (#399).** Analyze, Approve and
Reject carry the ID of the review on screen, and the Review Engine refuses
them with `*review.ReviewChangedError` when the stored review has another
ID. The screen never takes a review from an operation's `Result`: after
any completed or failed operation for its package it reloads review and
diff through `loadReviewCmd` (without setting `loading`). A reload that
brings a different review shows a "replaced" toast; a refusal keeps the
screen open and shows the opFailure banner instead of going back. The
reload only lands while Review is the active screen — App routes
`reviewLoadedMsg` to the active screen only — which is enough, because
opening Review always reloads (`Init`).

---

## Operations Queue

`internal/ui/ops.go`. All work that runs git, makepkg or the publisher
goes through one strict FIFO with **one active operation at a time**; there
are no concurrent builds.

| Kind | Panel lane | What `executeOperation` runs (`app.go`) |
| --- | --- | --- |
| `OpFetch` | Fetch | `review.FetchAndReview`, under one 60-second timeout (#409): fetch; if the stored review is not for `upstream/master`, start a new Review (#405) |
| `OpMerge` | Fetch | requires an Approved review; `Git.Merge(review.UpstreamCommit)` |
| `OpAnalyze` | Analyze | `RunAnalyzers` on the review the screen showed (`Operation.ReviewID`); no fetch, no new review (#399) |
| `OpBuild` | Build | requires Approved and the reviewed commit merged (`Git.IsMerged`, #406); `BuildEngine.RunBuild` |
| `OpConfirm` | Publish | requires a successful, unconfirmed last success built from local HEAD; sets `Confirmed` |
| `OpPublish` | Publish | requires a confirmed last success built from local HEAD; `Publisher.Publish`, sets `Published` |

**One operation per package.** `Operation.ID` is the package name, and
`Enqueue` refuses an ID already pending or active ("Already queued"), so a
package can never have two operations queued, of any kind. When a request
is queued behind others, a toast shows how many are ahead.

### Flow

1. A `*RequestedMsg` reaches `App.update`, which wraps its `TraceCtx` with
   `context.WithCancel` and calls `ops.Enqueue`.
2. `drainQueueCmd` dequeues the head **synchronously, on the App value
   being returned** (see § Value-receiver discipline), marks its lane
   started, and returns `executeOperation(op)` as a `tea.Cmd`. That Cmd
   only does I/O; it never touches queue state.
3. The Cmd returns a Completed, Failed or Cancelled message. App clears the
   active operation, updates the lane (Failed also writes `errors.log`;
   Cancelled writes the activity log), then calls `drainQueueCmd` again.
4. `Ctrl+T` calls the active operation's cancel function; the operation
   returns `OperationCancelledMsg`. A cancel that no longer matches the
   active operation is ignored (#335).

### Panel

The Operations Panel shows four fixed lanes (Fetch, Analyze, Build,
Publish), each with a status (idle, running, completed, failed, cancelled),
its last package and its last log line. Lane state is a read-side view of
the one queue: queue depth is computed from `pending` on every read, never
stored. The Status Bar shows the active operation and the pending count.

---

## Lifecycle and Action Availability

`lifecycle.DeriveState` (`internal/lifecycle`) computes a package's state;
it is a pure function and the state is never stored. Inputs: review status
(or no review), whether the reviewed commit is merged
(`Git.IsMerged`), local HEAD, and the last successful build.

```text
NotReviewed · ReviewPending · Approved · Merged · Built · Confirmed · Published · Rejected
```

The last successful build only counts when its `ReviewedCommit` equals
local HEAD. After a new merge, an older build no longer makes the package
Built/Confirmed/Published, and an unknown HEAD counts as "no build".

Which actions are available in which state is defined in **`workflow.md`
§ Action Availability**, the single source of truth for the rule. Today
each screen implements its own predicates against that table
(`isMergeEnabled`, `isDashboardBuildEnabled`, `isConfirmEnabled`, … in
`dashboard.go` and `detail.go`); a mismatch between them is a bug. Moving
to one shared derivation that every screen calls is proposed for v2
(#404).

Invalid actions stay visible and dimmed: with a short reason on Package
Details (`(requires …)`), without it on the Dashboard
(`tui-package-details.md` § Action Bar).

---

## Persistence

aur-curator persists only what cannot be derived (`architecture.md`
§ Store Only Irreducible State): review decisions, build history and
build artifacts. Diffs, commit history, merge status and lifecycle state
are always computed live — from Git, or by `lifecycle.DeriveState` from
the stored records — and never written to a store.

### Stores

There are three store interfaces, each with one JSON implementation:

| Interface | Declared in | Persists | Implementation |
| --- | --- | --- | --- |
| `ReviewStore` | `internal/domain/review.go` | the current `Review` per package (decision, notes, findings, frozen `RiskAssessment`) | `internal/store/json/review_store.go` |
| `BuildStore` | `internal/domain/build.go` | `Build` records: the latest attempt and the last success (status, times, log location, reviewed commit, confirmation, publication) | `internal/store/json/build_store.go` |
| `ArtifactStore` | `internal/domain/artifact.go` | `Artifact` records (filename, version, architecture, checksum, size) | `internal/store/json/artifact_store.go` |

Stores are storage only. They never query Git, never derive or validate
state, and never compute a review's or package's status: that belongs to
Review Engine and `internal/lifecycle`. Every store is reached through its
interface, so screens and engines can be tested with fakes.

### Interface placement

All three store interfaces are declared in `internal/domain`, next to the
structs they persist (`domain.md`). `internal/domain` holds no
logic, but an interface declaration is not logic, and keeping the contract
beside its struct means the domain package defines both the shape of a
record and how it may be stored. `ArtifactStore` was moved there from
`internal/store` in #394. New store interfaces belong in `internal/domain`.

### Backend

The only backend is JSON files under a single workspace-level root,
`<rootPath>/.aur-curator/`, never inside a package's own Git repository:

```text
.aur-curator/
├── packages/<package>/
│   ├── review.json          ReviewStore: the current Review
│   ├── build.json           BuildStore: the latest Build attempt
│   └── last-success.json    BuildStore: the latest successful Build
├── artifacts/<artifact-id>/
│   └── artifact.json        ArtifactStore: one Artifact record
├── builds/<build-id>/
│   ├── build.log            build output log
│   ├── work/                disposable build checkout (see git-command-walkthrough.md)
│   └── *.pkg.tar.zst        built package files
├── activity.log             always-on lifecycle log
├── errors.log               always-on failure log
├── trace.log                when tracing is on
└── workspace.lock           one running aur-curator per workspace
```

Store records are keyed differently: reviews and builds by package name,
artifacts by artifact ID (found by package through `Artifact.PackageName`,
see below). Build output directories are keyed by build ID; only the
directories referenced by a package's latest attempt and last success are
kept.

Plain files are simple, human-inspectable and sufficient at the scale this
project targets (around a dozen packages). The interfaces exist so the
backend could change (e.g. to SQLite) without touching engines or the UI;
there is no plan to do so, and only one backend is ever active.

### Review: current, not historical

`ReviewStore` holds exactly one Review per package. Fetching a new
upstream commit replaces the stored Review — Pending or decided — with a
new Pending one, and the superseded review's notes, findings and rationale
are not kept (`domain.md` § Review Scope: Current, Not Historical). Git
history remains the record of what was reviewed and merged.

### FindingsStatus

Storage must distinguish three findings states, matching the Review screen
(`tui-review.md` § Tab Bar):

| FindingsStatus | Findings | Meaning |
| --- | --- | --- |
| `not_run` | nil/empty | Analyzers were never run for this review |
| `run` | empty slice | Analyzers ran, found nothing |
| `run` | non-empty slice | Analyzers ran, found N issues |

`not_run` must never be stored the same way as `run` with an empty slice:
the Approve flow and the Risk column depend on the difference.

### Artifact's denormalized PackageName

`Artifact.PackageName` duplicates what is reachable through
`Artifact.BuildID → Build.PackageName`. It is kept deliberately, because
Package Details looks artifacts up by package, and joining through
`BuildStore` for every lookup is not worth the purity
(`domain.md` § Artifact).

### Write before signalling

Any operation that writes a store must finish the write before it returns
the message that reports completion. Screens re-derive state from the
stores as soon as that message arrives; a completion message sent before
the write lands makes them derive from stale records. Confirm and Publish
(`app.go`) save the last-success record before returning
`OperationCompletedMsg`; every new operation must do the same.

---

## Implementation Conventions & Gotchas

### Bubble Tea rules

These apply to everything under `internal/ui`, `internal/widgets` and
`internal/theme`.

- **One model per screen; App routes.** A screen never navigates, quits
  or reads another screen; it emits a message and App acts on it.
- **No goroutines in `Update`.** All async work is a `tea.Cmd` returned
  from `Update`; every call into Git, stores, the builder or the publisher
  happens inside such a Cmd.
- **No I/O in `View`.** `View` renders state already loaded by a Cmd and
  delivered as a message.
- **`Update` returns new state** rather than mutating shared structures.
  Queue bookkeeping in App is ordinary state transition, done on the value
  being returned (next rule).
- **Styling lives in `internal/theme`; reusable components in
  `internal/widgets`** (tab bar, action bar with dimmed actions, modals,
  content pane, nav menu, layout helpers). Prefer a Bubbles component where
  one exists.
- **Key bindings use `bubbles/key`.**
- **`tea.Batch` for independent commands, `tea.Sequence` when order
  matters** (e.g. a save that must land before navigating away).
- **Sizes come from App.** Every screen handles `tea.WindowSizeMsg`; App
  sends each screen the content-area size, not the terminal size, and
  re-sends it whenever the chrome changes.
- **Every exported identifier has a GoDoc comment; `gofmt`; table-driven
  tests** for branchy pure functions (lifecycle derivation, action
  predicates, findings states, layout arithmetic).
- **Keep files small.** Split a screen into several files before it grows
  hard to review; `dashboard.go`, `detail.go` and `app.go` are already past
  that point (§ Known Gaps).

The rules below each come from a real defect that passed `go test`,
`golangci-lint` and review and was only caught in a manual run. They are
hard rules, not style preferences.

### Value-receiver discipline on `App`

**Rule:** every method on `App` that is called from `Update` and is
expected to affect persisted state must use a value receiver
(`func (a App) ...`), never a pointer receiver (`func (a *App) ...`).

**Why:** Bubble Tea persists whatever `App` value `Update` returns as the
new live model. A pointer-receiver method called from inside `Update`
operates on the address of `Update`'s local parameter — a *different* copy
from whatever gets returned as the new model. If that method's job is to
schedule a `tea.Cmd` closure that mutates state later (e.g. draining a
queue), the mutation lands on an orphaned copy that Bubble Tea never sees
again.

This was #50's root cause: `OperationsQueue.Dequeue()` never removed
anything from the live model, because `drainQueueCmd` had a pointer
receiver. The queue looked idle from the outside but silently re-executed
the same operation forever. The bug only shows across the boundary between
synchronous `Update` mutation and asynchronous `tea.Cmd` execution, which a
unit test calling `Update` directly won't exercise unless written to.

**Concretely:** a method that both mutates `App` and returns a `tea.Cmd`
has the signature `func (a App) foo(...) (App, tea.Cmd)` — mutate the local
`a`, return it alongside the Cmd. Every call site uses the returned value
(`a, cmd := a.foo(...)` or `return a.foo(...)`), never `a.foo()` with the
App discarded. `drainQueueCmd`, `setToast` and `syncScreenSize` all follow
this shape.

### Distinct message semantics get distinct types

**Rule:** two messages that mean different things — even if they'd
naturally share most of their fields — must be two separate Go types with
two separate `case` arms in `Update`, never one type dispatched on a shared
discriminator field.

**Why:** merging a "set" signal and a "clear" signal into one type (e.g.
`ToastMsg{ID int, Text string}`, dispatched by checking whether `ID`
matches a current generation counter) makes it possible for a "set"
delivery to be misrouted into the "clear" branch. This happened: a toast
was set synchronously via `setToast`, then also redundantly delivered
through a batched Cmd emitting `ToastMsg{ID: N, Text: reason}`; that
redelivery matched the current generation and was treated as a dismiss —
clearing the toast before it ever rendered.

**Concretely:** `ToastMsg{Text}` only ever means "show this";
`clearToastMsg{id}` only ever means "clear if this generation is still
current". `setToast` is the one place toast state is written, called
directly wherever an App value is at hand, never round-tripped through a
message. The same reasoning made `OperationCancelledMsg` its own type
rather than a flag on `OperationFailedMsg`.

### Key bindings

**Rule:** actions use plain letter keys (`f`, `m`, `b`, `c`, `p`, `r`, `a`).
`Ctrl` combinations are for navigation and App-wide keys (`^PgUp`/`^PgDn`
tabs and cards, `^O` sort, `^T` cancel, `^C`/`^Q` quit), and for Reject
(`^X`), so that a rejection is never taken by a stray keypress. While a text input
has focus (the Approve notes and Reject reason modals), no plain-letter
binding may fire: the input gets the keys.

**Why:** the Review screen once had an always-focused Reviewer Notes field,
so typing a note could trigger actions (`x` rejected, #41), which led to
`Ctrl`-modified action keys on that screen only. Since #22 moved notes into
the Approve modal, Review uses plain letters like every other screen.

### Tab/Shift+Tab reserved for focus, not content-switching

**Rule:** `Tab`/`Shift+Tab` only move focus between focusable elements
(fields, scrollable panes, buttons). Switching tabs or views within a
screen uses a distinct binding (`^PgDn`/`^PgUp`), shown in the action bar.
Any widget that consumes its own keys (a viewport scrolling on
`PgUp`/`PgDn`, a text input) is a full member of the screen's focus cycle;
"read-only but focusable" is a legitimate focus target.

**Why:** an earlier Review implementation had no focus model, so every key
not intercepted earlier fell into the notes field. The fix that introduced
focus state also repurposed `Tab` from focus to tab-switching as a side
effect, without that ever being decided. Changing what `Tab` means is a
design decision, never a bugfix side effect.

### Scrollable-content convention

**Rule:** any pane or tab that can hold variable-length content renders
through a bounded, scrollable widget (`widgets.ContentPane` /
`bubbles/viewport`), never as a plain string joined into the screen's
sections. This is judged per tab, not per screen.

**Why:** #76 ("Review screen chops the top of the viewport") was first fixed
only for the Diff tabs. The Findings tab rendered through a separate
function with no viewport and no height bound, so the same bug stayed fully
reproducible there — and more likely, since findings grow with real
analyzer output.

**Concretely:** a screen with tabs feeds every tab's content through the
same bounded pane, re-populating it on tab switch. Package Details does
this through `refreshContent`, the only place tab content is set.

### Single source of truth for section layout

**Rule:** a screen's sections (nav menu, header, warnings, tab bar, content,
action bar, …) are built by one function that both `View` and the height
calculation call — never assembled independently in two places.

**Why:** the #76 fix measured section strings built in one helper while
`View` built its own copy of the same sections. The two agreed by
coincidence; the next edit to one would have sized the pane against
content that isn't what's drawn, with no compiler error.

**Concretely:** each screen has `fixedPartsAboveContent` /
`fixedPartsBelowContent` (Dashboard: `fixedPartsAboveList`), used by both
`View` and the size calculation, and `widgets.FlexHeight` turns the
remaining height into the pane size. No hardcoded row counts.

### Context lifetime: never defer cancel() on a context embedded in an outgoing Msg

**Rule:** if a context is going to be embedded in a message returned from a
`tea.Cmd` — for reuse by work that runs later, when `Update` handles that
message — do not `defer cancel()` in the closure that created it. Let its
own timeout (`git.GitTimeoutCtx`) be the cleanup.

**Why:** `defer cancel()` fires when the creating closure returns, not when
the context's last use ends. A context created, put in a message's
`TraceCtx` and returned is already cancelled by the time the work spawned
from that message runs. Code that fails closed on cancellation then
silently returns wrong answers: the Dashboard Refresh bug, where `IsMerged`
read a dead context as "not merged", came from exactly this.

**Concretely:** `defer cancel()` is correct when the context only bounds
work inside the creating closure. If it escapes — into a message, onto the
model, into later work — discard the cancel deliberately and say why in a
comment (the Dashboard's Refresh handler does), or tie cancellation to the
point where the last consumer finishes.

---

## Known Gaps

Places where the code does not yet follow this guide or the docs. Each is
known; fix it under its own issue, not as a side effect.

- **I/O in the Dashboard's `Update`.** `UpdateFromReview` runs git calls
  (`IsMerged`, `LocalHeadCommit`) synchronously, and `buildSummary` /
  `buildEntries` read the build store, inside `Update`. Package Details does
  its loading correctly in `loadDetailCmd`. Belongs with #404.
- **Action predicates are per screen** (§ Lifecycle and Action
  Availability), and `workflow.md` § Action Availability still lists
  Install and per-screen Analyze, which no longer exist.
- **Large files.** `screens/dashboard.go` (~1,360 lines), `screens/detail.go`
  (~1,170) and `ui/app.go` (~1,030, with six near-identical request cases in
  `update`).
- **Timeout errors don't say so outside Fetch.** Analyze, Merge, Fetch and
  the AUR check have a 60-second timeout on git calls (Build has none by
  design, #211), but only Fetch labels a timeout as one (#409); the others
  report git's `signal: killed` (#417).
- **Unused or unread pieces:** `Dependencies.RepoStore`, `OperationLogMsg`
  and `OperationsQueue.AppendLog` (#407); the `builderBackend`,
  `analyzerConfig` and `uiPreferences` config keys (#408); `PolicyConfig`
  overrides; the Settings screen.
- **`OperationCompletedMsg.Result` is `any`**, narrowed by kind at each
  use. Revisit if per-kind result types become worth it.
