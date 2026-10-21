# Developer Guide: aur-curator

> Last updated: 2026-10-08
> Status: Active
> Companion docs: architecture.md, interfaces.md, go-domain-structs.md
>
> Implementation-facing documentation. Where `architecture.md` and `interfaces.md`
> describe contracts and principles, this document describes how those contracts
> are wired together in the Bubble Tea application.
>
> For bounded implementation tasks (e.g. "build the Review screen"), see the
> relevant file under `screen-packets/` or `interface-packets/` — those are
> self-contained and generated from this doc plus the domain contract.

---

1. [Application Structure](#application-structure)
2. [Dependency Injection](#dependency-injection)
3. [Review Store](#review-store)
4. [Views](#views)
5. [Msgs](#msgs)
6. [Operations Queue](#operations-queue)
7. [Implementation Conventions & Gotchas](#implementation-conventions--gotchas)
8. [Action Availability — Single-Source Derivation](#action-availability--single-source-derivation)
9. [Open Items](#open-items)

---

## Application Structure

The application is a single `tea.Model` (`App`) that owns navigation and
cross-screen state. Each screen in the nav tree (`ui-overview.md`) is a
sub-model that only knows how to render its own slice of state. `App` is the
only model allowed to change which screen is active.

```go
type Screen int

const (
    ScreenDashboard Screen = iota
    ScreenPackageDetail
    ScreenRepositoryStatus
    ScreenSettings
)

type App struct {
    screen  Screen
    history []Screen // back-navigation stack

    // shared, cross-screen state
    workspace domain.Workspace
    packages  []domain.Package
    ops       OperationsQueue // single global queue — no concurrent builds

    // one sub-model per screen
    dashboard  DashboardModel
    detail     PackageDetailModel
    repoStatus RepositoryStatusModel
    settings   SettingsModel

    toast Toast
    err   error

    // dependencies — see "Dependency Injection" below
    deps Dependencies
}
```

`App.Update` routes on `a.screen`, delegating the incoming `tea.Msg` to the
active sub-model and forwarding any `tea.Cmd` it returns. No sub-model may
read or write another sub-model's state directly.

---

## Dependency Injection

`App` is constructed with the architectural interfaces from `interfaces.md`
injected as plain struct fields — no service locator, no global context.
Each interface is a narrow contract; the concrete implementation (Git
provider, makepkg builder, etc.) is chosen at `main.go` and passed down once.

```go
type Dependencies struct {
    Git        git.Provider
    Catalog    catalog.PackageCatalog // renamed from RepositoryStore — see interfaces.md
    Reviews    review.Engine
    Analyzers  analyzer.Manager
    Policy     policy.Engine
    Builder    builder.Builder
    Publisher  publish.Publisher
    ReviewDB   domain.ReviewStore
    BuildDB    domain.BuildStore
}
```

**No `Config`/`ConfigurationProvider` field.** `WorkspaceConfig` is loaded
once, by a plain function, not injected as a service interface —
`go-domain-structs.md` § WorkspaceConfig is explicit that there is no
`ConfigurationProvider`, mirroring how `PolicyConfig` is passed directly
into Policy Engine's constructor rather than accessed through a service.
An earlier version of this document had a `Config ConfigurationProvider`
field here, contradicting that domain-level decision — removed; see
`domain-package-review.md` DOM-010.

Concretely, `main.go` calls `config.Load` once and passes only the
specific `WorkspaceConfig` fields each constructor actually needs — not
the whole config, and not an interface wrapping it — so a given
dependency can't reach for fields it has no business touching (same
"avoid knowledge of unrelated subsystems" principle from `interfaces.md`
§ Design Principles that already applies to the interfaces above):

```go
func main() {
    cfg, err := config.Load(configPath) // plain function, WorkspaceConfig
    if err != nil {
        // ...
    }

    deps := Dependencies{
        Catalog: catalog.New(cfg.RootPath),
        Git:     git.NewProvider(cfg.RootPath, cfg.GiteaBaseURL, cfg.GiteaOrg, cfg.GiteaToken),
        Builder: builder.New(cfg.BuilderBackend),
        // ...remaining dependencies constructed the same way, each
        // taking only the cfg fields it needs
    }

    app := NewApp(deps)
    // ...
}

func NewApp(deps Dependencies) App {
    return App{
        screen: ScreenDashboard,
        deps:   deps,
        // sub-models initialized with references to the same deps struct
        dashboard: NewDashboardModel(deps),
        detail:    NewPackageDetailModel(deps),
    }
}
```

Every `tea.Cmd` that talks to Git, Review, Build, or Publisher does so
through `a.deps.*`, never by constructing a concrete type inline. This keeps
sub-models independently testable with fake implementations of each
interface, per the Design Principles in `interfaces.md`.

---

## Persistence

aur-curator persists only what cannot be derived (see `architecture.md`
§ Store Only Irreducible State): review decisions, build history and
build artifacts. Diffs, commit history, merge status and lifecycle state
are always computed live — from Git, or by `lifecycle.DeriveState` from
the stored records — and never written to a store.

### Stores

There are three store interfaces, each with one JSON implementation:

| Interface | Declared in | Persists | Implementation |
| --- | --- | --- | --- |
| `ReviewStore` | `internal/domain/review.go` | the current `Review` per package (decision, notes, findings, frozen `RiskAssessment`) | `internal/store/json/review_store.go` |
| `BuildStore` | `internal/domain/build.go` | `Build` records (status, times, log location, reviewed commit, confirmation) | `internal/store/json/build_store.go` |
| `ArtifactStore` | `internal/store/artifact_store.go` | `Artifact` records (filename, version, architecture, checksum, size) | `internal/store/json/artifact_store.go` |

Stores are storage only. They never query Git, never derive or validate
state, and never compute a review's or package's status: that belongs to
Review Engine and `internal/lifecycle`. Every store is reached through
its interface (`Dependencies.ReviewDB`, `Dependencies.BuildDB`, …), so
screens and engines can be tested with fakes.

### Interface placement

`ReviewStore` and `BuildStore` are declared in `internal/domain`, next to
the structs they persist (`go-domain-structs.md`). `internal/domain`
holds no logic, but an interface declaration is not logic, and keeping
the contract beside its struct means the domain package defines both the
shape of a record and how it may be stored.

`ArtifactStore` is the exception: it is declared in `internal/store`.
This is a known inconsistency, not a deliberate second convention; a
follow-up issue moves it into `internal/domain` beside the other two.
New store interfaces belong in `internal/domain`.

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
│   └── *.pkg.tar.zst        built package files
├── activity.log             always-on lifecycle log
├── errors.log               always-on failure log
└── workspace.lock           allows one running aur-curator per workspace
```

Store records are keyed differently: reviews and builds by package name,
artifacts by artifact ID (found by package through
`Artifact.PackageName`, see below). Build output directories are keyed by
build ID; only the directories referenced by a package's latest attempt
and latest success are kept.

Plain files are simple, human-inspectable and sufficient at the scale
this project targets (around a dozen packages). The interfaces exist so
the backend could change (e.g. to SQLite) without touching engines or the
UI; there is no plan to do so, and only one backend is ever active.

### Review: current, not historical

`ReviewStore` holds exactly one Review per package. Fetching a new
upstream commit replaces the stored Review — Pending or decided — with a
new Pending one, and the superseded review's notes, findings and
rationale are not kept (`domain.md` § Review Scope: Current, Not
Historical). Git history remains the record of what was reviewed and
merged.

### FindingsStatus

Storage must distinguish three findings states, matching the Review
screen (`tui-review.md` § Tab Bar):

| FindingsStatus | Findings | Meaning |
| --- | --- | --- |
| `not_run` | nil/empty | Analyzers were never run for this review |
| `run` | empty slice | Analyzers ran, found nothing |
| `run` | non-empty slice | Analyzers ran, found N issues |

`not_run` must never be stored the same way as `run` with an empty
slice: the Approve confirmation flow depends on the difference.

### Artifact's denormalized PackageName

`Artifact.PackageName` duplicates what is reachable through
`Artifact.BuildID → Build.PackageName`. It is kept deliberately, because
Package Details looks artifacts up by package, and joining through
`BuildStore` for every lookup is not worth the purity
(`go-domain-structs.md` § Artifact).

### Write before signalling

Any operation that writes a store must finish the write before it
returns the message that reports completion. Screens recompute action
availability from the stores as soon as that message arrives (§ Action
Availability — The persistence-ordering invariant).

---

## Views

Maps directly to the nav tree in `ui-overview.md`:

```text
Dashboard
 ├── Package Details
 │     └── Build Log      (tab; not a trust decision, doesn't warrant a
 │                          separate screen)
 ├── Review               (sibling screen to Package Details, not a tab
 │                          within PackageDetailModel — reached via
 │                          NavigateMsg from Package Details' "Open
 │                          Review" action)
 ├── Repository Status
 └── Settings
``
**Confirmed:** Build Log is a tab within `PackageDetailModel`, not its own
`Screen`. Review is not in the same tab group — it's a sibling top-level
`Screen` (see nav tree above). Build Log has no domain actions, only
infrastructure actions (Start build, Cancel build), matching its entry in
`ui-overview.md`.
**Note on "Decision":** Approve/Reject and the reviewer's rationale are
entered on the Review screen, not a Package Details tab (see § Application
Structure — `App.review` is a top-level `screens.ReviewScreen`, not part of
`PackageDetailModel`). "Decision" is not a domain entity either way —
`Review.Status`, `Review.DecisionTime`, and `Review.Rationale` carry that
data directly (see `domain.md` § Review). There is no corresponding
`Decision` struct in `go-domain-structs.md`.

```go
type DashboardModel struct {
    rows   []DashboardRow // derived, rebuilt from packages on load
    cursor int
    search string
    filter Filter
    sort   SortKey
}

type DetailTab int

// Diff/.SRCINFO/.install/Patches/Findings tabs are NOT DetailTab values —
// they belong to ReviewScreen's own tab bar (widgets.TabBar, string-labeled
// via widgets.Tab{Label: ...}, not a Go const enum), since Review is a
// separate top-level Screen. DetailTab only covers tabs that actually stay
// on Package Details.
const (
    TabSummary DetailTab = iota
    TabCommits
   TabBuildLog
)

type PackageDetailModel struct {
    pkg domain.Package
    tab DetailTab

    // lazily populated per tab so switching tabs doesn't block on I/O
    // after the first fetch. Review is NOT here — Review is its own
    // top-level Screen (App.review, msgs.ScreenReview), reached via
    // NavigateMsg from this screen's "Open Review" action, not rendered
    // as a PackageDetailModel tab. Confirmed via source,
    // internal/ui/app.go:39/225/367-373 and
    // internal/ui/screens/detail.go:60.
    build *domain.Build
}

type RepositoryStatusModel struct {
    repo domain.PackageRepository
}

type SettingsModel struct {
    workspace domain.Workspace
    policy    policy.Engine
}
```

---

## Msgs

Four categories, kept distinct so `Update` methods stay flat switches
instead of nested conditionals.

### 1. Navigation

Handled exclusively by `App`; sub-models never navigate themselves.

```go
type NavigateMsg struct {
    To      Screen
    Package domain.PackageName // set when navigating to ScreenPackageDetail
}
type BackMsg struct{}
```

### 2. Data loaded

Results of `tea.Cmd`s that called into the injected interfaces.

```go
// PackagesLoadedMsg is name-only: PackageCatalog has no way to populate
// a full domain.Package's GitRepositoryID/CurrentReviewID/LatestBuildID
// from a directory listing alone. Assembling those, if/when something
// needs full domain.Package values, is separate, currently-unscoped
// work — don't fabricate zero-valued IDs to force domain.Package here.
type PackagesLoadedMsg struct{ Packages []domain.PackageName }
type ReviewLoadedMsg struct {
    Pkg    domain.PackageName
    Review domain.Review
}
type DiffLoadedMsg struct {
    Pkg  domain.PackageName
    Kind string // "PKGBUILD" | ".SRCINFO" | ".install" | "patch"
    Diff Diff
}
type FindingsLoadedMsg struct {
    Pkg      domain.PackageName
    Findings []domain.Finding
}
type BuildLoadedMsg struct {
    Pkg   domain.PackageName
    Build domain.Build
}
type RepoStatusLoadedMsg struct{ Status domain.PackageRepository }
```

### 3. User-triggered actions

Emitted by sub-models in response to key presses / clicks. `App` turns
these into queued `Operation`s (see below) rather than executing them
immediately — this is what enforces "never auto-build after review" at
the message-routing level rather than relying on view code to check state.

```go
type FetchRequestedMsg struct{ Pkg domain.PackageName }
type AnalyzeRequestedMsg struct{ Pkg domain.PackageName }
type BuildRequestedMsg struct{ Pkg domain.PackageName }
type PublishRequestedMsg struct{ Pkg domain.PackageName }

// DecisionRequestedMsg captures an Approve/Reject action. There is no
// domain.DecisionOutcome type — Decision is not a separate entity (see
// domain.md § Review). Outcome reuses domain.ReviewStatus, restricted to
// its two terminal values.
type DecisionRequestedMsg struct {
    Pkg     domain.PackageName
    Outcome domain.ReviewStatus // domain.ReviewApproved | domain.ReviewRejected
    Notes   string
}
```

### 4. Operation lifecycle

Drives both the global queue and the Operations Indicator.

```go
type OperationQueuedMsg struct{ Op Operation }
type OperationStartedMsg struct{ Op Operation }
type OperationProgressMsg struct {
    OpID string
    Pct  int
}
// OperationLogMsg carries a textual log-append event, distinct from
// OperationProgressMsg's numeric percent — see § Operations Queue
// § Operations Panel — Lanes for how both route into a single
// per-lane append-only Log.
type OperationLogMsg struct {
    OpID    string
    Message string
}
type OperationCompletedMsg struct {
    OpID   string
    Result any // narrowed by Op.Kind at the call site
}
type OperationFailedMsg struct {
    OpID string
    Err  error
}
```

### Error handling

```go
type ErrorMsg struct{ Err error } // synchronous failures -> modal
type ToastMsg struct{ Text string }
```

`OperationFailedMsg` always routes to `ToastMsg`; anything that fails
synchronously inline (bad config, invalid input) routes to `ErrorMsg`.
This mirrors the distinction in `ui-overview.md` between synchronous
domain actions and asynchronous infrastructure actions.

---

## Operations Queue

Concurrent builds are not supported in this version, so the queue is a
strict FIFO that `App` drains one operation at a time. Dequeue happens
**synchronously inside `Update`** (via `drainQueueCmd`) rather than
inside a `tea.Cmd` closure — see "Value-receiver discipline on `App`"
above for why this matters. A `tea.Cmd` must never be the thing that
mutates queue state, since `Cmd` closures run later, asynchronously,
over their own captured copies of `App`.

```go
type OperationKind int

const (
    OpFetch OperationKind = iota
    OpAnalyze
    OpBuild
    OpPublish
)

type Operation struct {
    ID   string
    Kind OperationKind
    Pkg  domain.PackageName
}

type OperationsQueue struct {
    pending []Operation
    active  *Operation
    lanes   map[Lane]*LaneState // lazily initialized per lane, see LaneState below
}
```

### Operations Panel — Lanes

The Operations Panel (`tui-dashboard-wireframe.md` § Operations Queue,
detailed in CMD-056) groups status by four fixed lanes rather than by
individual `Operation`. `Confirm` is **not** a lane — it is a synchronous
workflow gate (`DecisionRequestedMsg`-style, not queued), not an
Operation, and never appears in the panel.

```go
type Lane string

const (
    LaneFetch   Lane = "fetch"
    LaneAnalyze Lane = "analyze"
    LaneBuild   Lane = "build"
    LanePublish Lane = "publish"
)

// Lane maps an OperationKind to the lane it reports status to.
// There is no Install operation: aur-curator does not install packages
// (workflow.md § Installation, #351).
func (k OperationKind) Lane() Lane {
    switch k {
    case OpFetch:
        return LaneFetch
    case OpAnalyze:
        return LaneAnalyze
    case OpBuild:
        return LaneBuild
    case OpPublish:
        return LanePublish
    }
    panic("unhandled OperationKind")
}

type OperationStatus string

const (
    StatusIdle      OperationStatus = "idle"
    StatusRunning   OperationStatus = "running"
    StatusCompleted OperationStatus = "completed"
    StatusFailed    OperationStatus = "failed"
)

type LogEntry struct {
    Timestamp time.Time
    Message   string
}

// LaneState is one lane's visible state in the Operations Panel. Only
// one lane can be StatusRunning at a time — this is a read-side view
// over the same single-active-Operation FIFO queue, not a second
// execution model. Log is append-only; entries are never mutated or
// truncated in place. Percent-progress (OperationProgressMsg) is
// rendered as a formatted log line (e.g. "35%") rather than a parallel
// numeric field, so there is one feed per lane, not two shapes to keep
// in sync.
type LaneState struct {
    Lane       Lane
    Status     OperationStatus
    QueueDepth int // pending Operations whose Kind maps to this Lane
    Log        []LogEntry
}
```

`OperationsQueue` gains lane-tracking methods, all pointer-receiver
(consistent with `Enqueue`/`Dequeue`/`Complete` above — see
"Value-receiver discipline on `App`" for why this is fine for
`OperationsQueue` itself but not for `App`):

```go
// MarkStarted transitions op's lane to Running and appends a log entry.
func (q *OperationsQueue) MarkStarted(op Operation)

// AppendLog appends a log entry to the lane owning op's Kind without
// changing Status. Used for both OperationLogMsg and OperationProgressMsg.
func (q *OperationsQueue) AppendLog(op Operation, message string)

// MarkCompleted transitions op's lane to Completed and appends a final
// log entry.
func (q *OperationsQueue) MarkCompleted(op Operation, message string)

// MarkFailed transitions op's lane to Failed and appends the error as
// a log entry.
func (q *OperationsQueue) MarkFailed(op Operation, err error)

// LaneState returns the current state of a lane. Never nil — a lane
// not yet touched lazily initializes to StatusIdle with an empty Log.
func (q *OperationsQueue) LaneState(l Lane) LaneState
```

`OperationCompletedMsg`/`OperationFailedMsg` still carry only `OpID`
(no `Kind`) — the handler resolves `Kind`/`Lane` via `a.ops.active`
(still populated at that point, matches `OpID`) before calling
`ops.Complete()`, so no change to those message shapes is needed.

One new Msg, added to § Msgs, Operation lifecycle — a log line and a
percent tick are different events even though both can fire mid-
operation, so per this doc's own "distinct message semantics get
distinct types" rule they are separate types, not one type with an
optional field:

```go
type OperationLogMsg struct {
    OpID    string
    Message string
}
```

Flow:

1. A `*RequestedMsg` arrives at `App.Update`.
2. `App` calls `ops.Enqueue(Operation{…})` (rejecting duplicates by ID);
   if the enqueue succeeds, `App` immediately calls `drainQueueCmd`.
3. `drainQueueCmd` checks `ops.IsIdle()`. If not idle, it calls
   `ops.Dequeue()` (moving the head from `pending` to `active` on the
   **local** `App` value), then returns the same `App` alongside a
   `tea.Cmd` that invokes `executeOperation(op)` — the actual I/O.
4. The `tea.Cmd` resolves to `OperationCompletedMsg` or
   `OperationFailedMsg`; `App.Update` handles the result by calling
   `ops.Complete()` (clearing `active`), then calling `drainQueueCmd`
   again to pull the next pending item. This repeats until idle.
5. The Operations Indicator (Status Bar) renders `active` plus
   `len(ops.pending)` — it holds no state of its own, consistent with
   the derived-state principle in `ui-overview.md`.

```go
// drainQueueCmd checks whether the queue has pending work and, if so,
// dequeues it (mutating the returned App value) and returns a Cmd that
// executes the dequeued operation.
func (a App) drainQueueCmd() (App, tea.Cmd) {
    if a.ops.IsIdle() {
        return a, nil
    }
    op, ok := a.ops.Dequeue()
    if !ok {
        return a, nil
    }
    return a, a.executeOperation(op)
}

// executeOperation returns a tea.Cmd that runs op using the appropriate
// injected dependency.
func (a App) executeOperation(op Operation) tea.Cmd {
    return func() tea.Msg {
        switch op.Kind {
        case OpBuild:
            if a.deps.Builder != nil {
                if err := a.deps.Builder.Build(op.Pkg); err != nil {
                    return OperationFailedMsg{OpID: op.ID, Err: err}
                }
                return OperationCompletedMsg{OpID: op.ID}
            }
            return OperationFailedMsg{OpID: op.ID, Err: errors.New("builder not configured")}
        case OpPublish:
            if a.deps.Publisher != nil {
                if err := a.deps.Publisher.Publish(op.Pkg); err != nil {
                    return OperationFailedMsg{OpID: op.ID, Err: err}
                }
                return OperationCompletedMsg{OpID: op.ID}
            }
            return OperationFailedMsg{OpID: op.ID, Err: errors.New("publisher not configured")}
        // OpFetch, OpAnalyze follow the same shape
        }
        return OperationFailedMsg{OpID: op.ID, Err: errors.New("unknown operation kind")}
    }
}
```

**Key design note:** `drainQueueCmd` returns `(App, tea.Cmd)`, not just
`tea.Cmd`. The dequeue mutation (`ops.Dequeue()`) happens **synchronously**
on the local `a` value, which is returned to `App.Update` and persisted
as the new live model. The returned `tea.Cmd` only does I/O — it never
touches queue state. This pattern (value-receiver mutation + I/O-only
closure) is what CMD-030's bug violated, and it is the pattern every
method on `App` that returns a `tea.Cmd` must follow.

---

## Implementation Conventions & Gotchas

Lessons from real defects found during SN-06/SN-07/SN-08, captured here
so they don't recur. Each is a **hard rule**, not a style preference —
each one caused a real, hard-to-diagnose bug that shipped past `go
test`, `golangci-lint`, and manual review before being caught.

### Value-receiver discipline on `App`

**Rule:** every method on `App` that is called from `Update` and is
expected to affect persisted state must use a value receiver
(`func (a App) ...`), never a pointer receiver (`func (a *App) ...`).

**Why:** Bubble Tea persists whatever `App` value `Update` returns as
the new live model. A pointer-receiver method called from inside
`Update` (`a.someMethod()` implicitly becomes `(&a).someMethod()`)
operates on the address of `Update`'s local parameter — a *different*
copy from whatever gets `return`ed as the new model. If that method's
job is to schedule a `tea.Cmd` closure that mutates state *later* (e.g.
draining a queue), the mutation lands on an orphaned copy that Bubble
Tea never sees again.

This was CMD-030's root cause: `OperationsQueue.Dequeue()` never
actually removed anything from the live model, because `drainQueueCmd`
had a pointer receiver. The queue looked idle from the outside but
silently re-executed the same operation forever. It passed `go test`,
`golangci-lint`, and code review — it only surfaced during a live
manual walkthrough, because the bug only manifests across the boundary
between synchronous `Update` mutation and asynchronous `tea.Cmd`
execution, which a unit test calling `Update` directly won't naturally
exercise unless written specifically to check for it.

**Concretely:** if a method needs to both mutate `App` state and return
a `tea.Cmd`, its signature should be
`func (a App) foo(...) (App, tea.Cmd)` — mutate the local `a`, return
it alongside the `Cmd`. Every call site does `a, cmd := a.foo(...)` (or
`return a.foo(...)` directly) — never `a.foo()` with the returned value
discarded.

### Distinct message semantics get distinct types

**Rule:** two messages that mean genuinely different things — even if
they'd naturally share most of their fields — must be two separate Go
types with two separate `case` arms in `Update`, never one type
dispatched on a shared discriminator field.

**Why:** merging a "set" signal and a "clear" signal into one type
(e.g. `ToastMsg{ID int, Text string}`, dispatched by checking whether
`ID` matches a current generation counter) makes it structurally
possible for a "set" delivery to be misrouted into the "clear" branch.
This happened during SN-08: a toast was set synchronously via
`setToast`, then *also* redundantly delivered through a batched
`tea.Cmd` emitting `ToastMsg{ID: N, Text: reason}`; that redelivery
arrived back at `Update`, matched the current toast generation by ID,
and was treated as a dismiss — clearing the toast in the same cycle it
was set, before it ever rendered.

The fix: `ToastMsg{Text string}` (no ID — only ever means "show this")
and a private `clearToastMsg{id int}` (only ever means "clear if this
generation is still current") are two separate types. Nothing
constructs a `ToastMsg` from code that already has an `App` value in
hand to mutate directly — `setToast` is the sole place toast state is
written, called synchronously wherever needed, never round-tripped
through a message when a direct call is available.

### Modifier-key convention

**Rule:** a screen's keybindings use plain letter keys (`f`, `a`, `b`,
etc.) by default. `Ctrl`-modified keys are reserved *only* for screens
that have a focused text-input field where a genuine letter collision
would otherwise occur (e.g. typing "a" into Reviewer Notes on the
Review screen must not also trigger "Run Analyzers").

**Why:** most screens (Dashboard, Package Details) have no text-entry
field at all, so there's no collision to guard against — modifying
every key with `Ctrl` on those screens would just make the UI harder to
use for no reason. Decided explicitly during SN-06 scoping: `f`/`a` on
Dashboard use plain keys, unlike the Review screen's `Ctrl+A`, which
exists specifically because Reviewer Notes is a focused text field on
that screen (see REV-004).

### Scrollable-content convention

**Rule:** any pane or tab that can hold variable-length or potentially
multi-page content must render through a `bubbles/viewport` (or an
equivalent bounded, scrollable widget) — never as a plain string joined
directly into the screen's section stack. This applies per-tab, not
per-screen: a screen can have some tabs that are safely fixed-height
and others that need a viewport, and each is judged on its own content,
not on whether a sibling tab already has one.

**Why:** CMD-060 was filed as "Review screen chops the top of the
viewport" and initially fixed only for the Diff-family tabs — the fix
measured every other section's real rendered height via
`lipgloss.Height()` and sized `DiffPane`'s viewport accordingly. But the
Findings tab rendered through a separate function
(`renderFindingsContent()`) that built a plain `strings.Builder` with no
viewport and no height bound at all — so the same chopping bug remained
fully reproducible on Findings, and is *more* likely to occur there in
practice, since Findings content grows with real analyzer output while
Diff content is comparatively bounded. The lesson isn't "Diff needed a
viewport" — it's "any tab holding variable-length content needs one";
treating that as implicit per-tab rather than a screen-wide rule is what
let the second instance slip through the first fix.

**Concretely:** a screen with multiple tabs should share one measured
layout function between sizing and rendering (see next rule) and feed
*every* tab's content through the same bounded viewport, re-populating
its content on tab switch rather than giving only some tabs a viewport
and others a bare string.

### Single source of truth for section layout

**Rule:** a screen's rendered sections (header, summary bars, tab
content, footer fields, action bar, etc.) must be built by one function
that both `View()` and any height/size-measurement code call — never
reconstructed independently in two places, even if the two copies
currently produce identical output.

**Why:** the CMD-060 fix computed viewport height by measuring a set of
section strings assembled in a `layoutParts()`-style helper, while
`View()` assembled its own separately-written copy of the same
sections. The two agreed by coincidence at fix time. Nothing prevents
them from silently diverging the next time someone edits the action bar
text or adds a line to the header in only one of the two places — and
when they diverge, the viewport is sized against content that isn't
what's actually rendered, reintroducing exactly the chopping bug this
rule exists to prevent, with no compiler error to catch it.

**Concretely:** `View()` should call the same section-building function
the sizing code calls, not maintain a parallel literal construction of
the same strings.

### Tab/Shift+Tab reserved for focus, not content-switching

**Rule:** `Tab`/`Shift+Tab` are reserved exclusively for moving focus
between focusable elements on a screen (fields, scrollable panes,
buttons). They must never be repurposed to switch between tabs/views
within a screen — use a distinct binding for that (e.g. `Ctrl+PgUp`/
`Ctrl+PgDn`, with help text surfaced in the action bar).

A corollary to the scrollable-content rule above: any widget that
consumes its own keys to operate (a viewport consuming `PgUp`/`PgDn` to
scroll, a text input consuming letters to edit) must be a first-class
member of the screen's focus cycle — "read-only but focusable" is a
legitimate focus target, not a special case reserved for text-entry
fields only.

**Why:** an earlier Review screen implementation had Reviewer Notes
`Focus()`-ed unconditionally with no focus model at all, so every key
not explicitly intercepted earlier in `Update()` silently fell into the
notes field regardless of the active tab or user intent. The fix
introduced explicit focus state — but the same change also repurposed
`Tab` for tab-switching into a focus-cycle key without that being a
deliberate, confirmed design decision; it landed as a side effect of an
unrelated bugfix. `Tab`/`Shift+Tab` conventionally mean field-to-field
navigation in TUI/GUI applications generally; overloading them for
content-switching is a guiding-principle change and must be surfaced
and confirmed explicitly, not folded silently into a bugfix patch.

### Context lifetime: never defer cancel() on a context embedded in an outgoing Msg

**Rule:** if a `context.CancelFunc`'s context is going to be embedded in a
`Msg` returned from a `tea.Cmd` — for reuse by asynchronous work that runs
later, when `Update()` processes that message — do **not** `defer cancel()`
in the closure that created it. Let the context's own timeout
(`git.GitTimeoutCtx` or equivalent) be the sole cleanup mechanism instead.

**Why:** `defer cancel()` fires when the creating closure returns, not when
the context's last real usage ends. A context can be created, embedded in a
message via a field like `TraceCtx`, and returned — at which point the
closure that created it is done and `defer cancel()` fires — while the
actual downstream consumer of that context (e.g. a fan-out of per-item Cmds
spawned by that message's handler) hasn't run yet. Every one of those
downstream operations then inherits an already-cancelled context, and any
error-handling that fails closed on cancellation (treating "the command
never ran" the same as "the command returned false") silently produces
wrong results rather than an obvious crash. This is a live defect that
shipped: the P1 Refresh bug (`git/merged.go`'s `IsMerged` treating a dead
context the same as a genuine "not merged" answer) traced directly back to
this pattern, and is the same underlying defect class as #219's Bug 1
(premature context cancellation) recurring in a newer code path.

**Concretely:** if a context only needs to bound synchronous work fully
contained within the closure that creates it, `defer cancel()` is correct
and fine. If the context escapes that closure — embedded in a returned Msg,
stored on the model, or otherwise handed to work that runs after the
closure returns — do **not** defer its cancellation there; size its timeout
appropriately and let that be the only cleanup, or restructure so
cancellation is tied to the actual point where the context's last consumer
finishes.

---

## Action Availability — Single-Source Derivation

### The problem this solves

Every enable/disable bug traced this session — #217 (Dashboard frozen
after Build completes), the Dashboard/Package Details `Merge`/`Build`
mismatch — had the same root cause: a screen's message handler
computing button availability via its own partial, ad-hoc logic
instead of a single shared rule. `ReviewDataMsg`'s handler did a full,
correct recompute; `OperationCompletedMsg`'s handler took a shortcut
instead of calling the same path. Two independent implementations of
"is this action available" will drift, given enough time and enough
separate edits — this is not a one-off bug to patch, it's a structural
gap to close.

### The fix: tie availability to the package, not the screen

There is no "Dashboard's Build-button rule" or "Package Details'
Build-button rule." There is only **one fact**: for a given package
(`Pkg_curr`), which actions are currently available. Every screen that
renders action buttons for a package is a consumer of that one fact,
never an independent computer of it.

```go
GetActionAvailability(pkg PackageName) map[Action]bool
```

This is the only function any screen calls to determine button
enable/disable. No screen implements its own version, partial or
otherwise — not even for a "fast path" or a "we already have this data
handy" shortcut. If a shortcut is ever tempting, that's the signal to
make `GetActionAvailability` itself cheaper, not to bypass it.

### Three layers, composed in order

**1. Gathering functions** (have I/O, one per fact, reusable
independent of this feature):

```go
reviewStatus(pkg) -> (status domain.ReviewStatus, hasReview bool)  // reads ReviewStore
isMerged(pkg)      -> bool                                          // live git check
buildRecord(pkg)   -> *domain.Build                                 // reads BuildStore
```

These are exactly `lifecycle.DeriveState`'s existing inputs — nothing
new is introduced here, they're formalized as named, independent
functions rather than inline logic scattered per caller. Note they're
not single-purpose to this feature: `isMerged`, for instance, is
presumably already needed elsewhere (Dashboard's "commits behind"
column, `#209`'s `check` command) — consolidating these is a general
win, not just an action-availability fix.

**2. Pure mapping layer** (zero I/O, table-driven-testable in
isolation, matching `sprint-plan.md`'s own testing convention):

```go
state := lifecycle.DeriveState(status, hasReview, isMerged, build)  // already exists, unchanged
availability := ActionAvailability(state, build.Confirmed)           // new — see below
```

`ActionAvailability` is `workflow.md` § Action Availability's table,
expressed as code. That table is the single source of truth for the
*rule*; this function is its only implementation. If the rule ever
needs to change, it changes in exactly one place, and `workflow.md`'s
table is updated to match — the two must never be allowed to drift
apart.

**3. Orchestration** (the only thing any screen ever calls):

```go
func GetActionAvailability(pkg PackageName) map[Action]bool {
    status, hasReview := reviewStatus(pkg)
    merged := isMerged(pkg)
    build := buildRecord(pkg)
    state := lifecycle.DeriveState(status, hasReview, merged, build)
    return ActionAvailability(state, build.Confirmed)
}
```

This function has no logic of its own beyond composition. Nothing
screen-specific can leak into it, because there's nowhere for it to
go.

### The persistence-ordering invariant

`GetActionAvailability` only produces a correct answer if everything
it reads was already durably written before it runs. Concretely: any
`tea.Cmd` that performs a domain action (Merge, Build, Confirm,
Publish, ...) must complete its store write(s) synchronously **before**
constructing or returning the message that signals completion. This is
trivial to guarantee in Go — don't return the `Msg` until `Save()` has
returned — but it must be a checked invariant at every operation's
`Cmd`, not an assumption inherited from "it probably already works
that way."

### When `GetActionAvailability` is called

Every one of the following triggers calls it fresh, in full, with no
partial-update alternative:

- A screen gains focus (entering Dashboard, entering Package Details,
  navigating back to either from another screen).
- An operation affecting the currently-displayed package completes,
  regardless of which screen was active when it started (this is
  exactly the case #217 got wrong — the fix is that this trigger and
  "screen gains focus" call the *same* function, not two different
  ones).
- `R` Refresh is pressed.

There is no fourth case where a screen updates its own button state
by any other means.

### Testing implication

Because the mapping layer (`ActionAvailability`) has zero I/O, it's
directly table-driven-testable against `workflow.md`'s own table with
no git repo or store on disk required — every row of that table
becomes one test case, matching `sprint-plan.md`'s stated convention
for "any branchy pure function."

## Open Items

- `Operation` results are currently typed `any` and narrowed at the call
  site by `Kind`. Worth revisiting once each operation's result type is
  finalized — may be worth splitting into `OperationCompletedMsg[T]`-style
  per-kind messages if the `any` narrowing gets unwieldy.
- Settings / Policy Engine wiring is sketched (`SettingsModel.policy`) but
  not yet detailed — deferred until Policy Engine design work (build-time
  network allowlisting, category → severity overrides) is further along.

- **Package Responsibilities:** what `internal/theme`, `internal/widgets`,
  and `internal/ui/screens` each own/don't own is stated in `sprint-plan.md`
  § UI Coding Conventions ("Styling lives in `internal/theme`, reusable
  widgets in `internal/widgets`") rather than duplicated here, to avoid two
  sources of truth. **Flag before relying on it:** that section states it
  applies "Sprint 1 and Sprint 6 onward," but Sprint 6 onward is now
  governed by `sprint-plan-v2-highlevel.md` (the SN-series) — confirm
  whether `sprint-plan.md` § UI Coding Conventions has itself been
  re-validated under the SN-series plan, or predates it and needs a
  supersession note of its own, before treating it as settled.
