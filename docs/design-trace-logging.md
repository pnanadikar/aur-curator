# Trace Logging — Design (finalizes #164)

> Supersedes #164's original issue body's open-questions framing. This
> is the decided design. Placement of actual `trace.Log` call sites
> happens gradually, call-by-call, as real work (starting with #163)
> needs them — not as a big up-front instrumentation pass.

## Why (the actual problem being solved)

Not "we need better logging" in the abstract. The specific, repeated
failure mode this session: Bubble Tea's async `tea.Cmd`/message model
separates *cause* (a keypress, a handler) from *effect* (a message
processed later, on a different call stack) in time. That gap is
exactly where several real bugs lived — #163's discarded
Review/Build data, the Unpub filter guard silently failing, #95's
`WindowSizeMsg` height mismatch, SYNC-001's stale-summary-card class.
Static code reading proved repeatedly unreliable at finding these
(Stage 6's "looks correct" report was wrong; Stage 6b's live trace
found the real bug). And each time, the fix was ad hoc, disposable
debug logging written from scratch — costing a full investigation
cycle per bug, every time.

**The goal:** a permanent, always-present facility that reconstructs
"what actually happened, in order, when I did X" — on demand, without
writing new code to investigate. The bar: something to check *before*
reaching for a debugger, not a replacement for one.

## Debug vs. Trace — two distinct tools, not levels of one

- **`--debug-log`** (existing, unchanged): ad hoc, disposable
  `log.Printf` statements a developer adds mid-investigation and
  deletes once done. Stays exactly as it is.
- **`--trace`** (new, this design): permanent instrumentation at fixed
  structural points, always in the code, silent unless the flag is on.
  You don't add trace logging to investigate something — it's already
  there; you turn it on and read it.

No level hierarchy between them (no "trace implies debug" or
verbosity sub-levels within trace). Binary on/off only — the
least config surface that does the job, appropriate for the app's
scale (~10-12 packages, personal tool). Revisit only if this genuinely
becomes noisy in practice — don't pre-build for a scale problem that
doesn't exist yet.

## Core mechanism — correlation ID via `context.Context`

Every user-initiated action (a keypress, not an internal message) mints
a trace ID at its origin point, before any `tea.Cmd` fires. That ID
rides on `context.Context` — the same mechanism several call sites
already use (`LoadReviewCmd`, `RunBuild`, etc.) — so it survives the
async gap by construction rather than needing a new parameter threaded
everywhere by hand.

```go
package trace

type ctxKey struct{}

// Start mints a new trace ID and returns a context carrying it,
// logging a "start" line. Called once, at an action's origin (a key
// handler), before any tea.Cmd is fired.
func Start(ctx context.Context, action string) context.Context

// Log records one line tagged with whatever trace ID is on ctx.
// No-op-safe if ctx carries none — always safe to call anywhere.
func Log(ctx context.Context, event string, kv ...any)
```

**Structural change required for this to work:** call sites that
currently do `context.Background()` inside a `tea.Cmd` closure need to
receive `ctx` from the enclosing scope instead, so the closure captures
the tagged context rather than minting a fresh untagged one. This is
the one piece of plumbing every future instrumented call site depends
on — worth doing early, even before most `trace.Log` calls exist.

## One log line = flow event + state, not two mechanisms

Considered and rejected: a separate "debug state" mechanism distinct
from "trace flow." In practice, internal state in this app is never
free-floating — it only changes in response to some event (a message
handled, a value computed). So there's no natural moment to log "just
state" that isn't already anchored to a flow event. One call, one line,
event names *when/why*, key-value pairs carry *what*:

```
t7  00:00.012  key       action=refresh
t7  00:00.014  cmd       fired=LoadReviewCmd pkg=yay
t7  00:00.041  store     op=ReviewStore.Get pkg=yay found=true
t7  00:00.043  git       op=CommitsBehind pkg=yay result=2
t7  00:00.089  msg       type=ReviewDataMsg pkg=yay
t7  00:00.090  dashboard lifecycle[yay]=Approved buildEntries=true
```

`grep t7 trace.log` reconstructs the full causal story in order,
regardless of what else interleaved. Relative timestamps (since trace
start, not wall-clock) make async gaps themselves visible — e.g. the
27ms between `cmd fired` and `store` *is* the boundary a bug like #163
lives in.

Format: plain text, key=value (logfmt-style) — greppable and readable
while tailing live during a manual test, no JSON/marshaling overhead
needed at this scale.

## Placement — guidance, not a schema, added gradually

Five categories, used as a checklist for where a `trace.Log` call earns
its keep — not an enforced/exhaustive list, and not a mandate to
instrument everything up front:

1. **key/cmd** — user-action origin points and the `tea.Cmd`s they fire.
2. **msg** — message types as they're handled in `App.Update`/each
   screen's `Update` (type + identifying fields, not full struct dumps).
3. **store** — `ReviewStore`/`BuildStore` reads/writes (which store,
   pkg, outcome).
4. **queue** — Operations Queue transitions (enqueue/start/complete/
   fail, op kind + pkg).
5. **handoff** — any point where a value is computed in one place and
   consumed in another, logged as *both* sides of the crossing. This is
   the category #95's `WindowSizeMsg` bug belongs to (computed height
   vs. forwarded height, in one line) and the category #163's
   discarded-Review/Build-data class belongs to once fixed — the
   pattern generalizes beyond just window sizing.

Start instrumentation with whatever #163 actually needs (Merge/Build/
Approve's message construction, Dashboard's consuming handlers) —
that's the first real placement pass, not a separate exercise.

## Disposable vs. promote — the actual rule for #163-style throwaway prints

Default: ad hoc `--debug-log` prints stay disposable, deleted once a
bug's understood — most of them are genuinely one-off curiosity about
a specific run, and keeping them is just noise for the next
investigation.

**Promotion signal:** if a throwaway print sits at one of the five
structural boundaries above (a message handler, a store call, a cmd
origin, a handoff point) rather than an arbitrary internal variable,
promote it into a permanent `trace.Log` call instead of deleting it —
because that boundary is exactly where a *different* future bug is
also likely to need visibility. The check before deleting a debug
print: "is this observing a structural boundary we've already named?"
If yes, promote. If no, delete as normal.

## Config

- `--trace` flag, separate from `--debug-log`.
- Single sink: `trace.log`, written to `.aur-curator/` (centralized
  storage location per #142).
- No rotation/retention policy for now — single file, no cleanup logic.
  If this ever needs pruning, fold into #130 (purge old build
  logs/artifacts) when that's picked up, not designed preemptively here.

## Scope of #164 itself

- New `internal/trace` package: `Start`/`Log` API, the `ctxKey`
  mechanism, the `--trace` flag wiring, the file sink.
- The `context.Background()` → threaded-`ctx` plumbing change at
  existing `tea.Cmd` call sites that will need to carry a trace ID.
- **Not in scope for #164 itself:** exhaustive instrumentation of all
  five categories everywhere. Placement happens incrementally,
  starting with #163's needs.

## Field Conventions by Category

> Append this section to `design-trace-logging.md`, after the section
> describing the five placement categories (key/cmd, msg, store, queue,
> handoff). This is naming discipline, not an enforced type — `trace.Log`
> intentionally keeps `event` and its kv pairs free-form (see `internal/
> trace`'s GoDoc). These conventions exist so that instrumentation added
> incrementally, packet by packet, stays consistent enough to `grep`/
> correlate across categories and across issues, rather than each future
> packet inventing its own field names ad hoc.

### Cross-cutting

- `pkg=<PackageName>` — nearly everything in this application is scoped
  to a single package. Include it on every line where applicable, even
  within a single-package trace, so multi-package operations (e.g. a
  Dashboard-wide Refresh firing one command per curated package) can be
  filtered and separated by package after the fact.

### 1. key/cmd — key press → command dispatch

- `key=<key pressed>` — on the originating key-handler line.
- `cmd=<command/function name>` — e.g. `LoadReviewCmd`, `BuildCmd`.
- `op=<OperationKind>` — when the command is routed through the
  Operations Queue (`OpBuild`, `OpFetch`, `OpMerge`, etc.); omit when
  the command bypasses the queue (e.g. synchronous in-memory filter ops).

### 2. msg — message arriving back in `Update()`

- `type=<Msg type name>`
- `pkg=<PackageName>`
- `err=<error string, or "nil">` — always present, even on success, so
  its absence is never ambiguous with "no error occurred."
- `hasValue=<true/false>` — whether the message already carried a known
  domain value (e.g. `*domain.Review`, `*domain.Build`) versus arriving
  empty and requiring a downstream fetch. This field is specifically
  aimed at making #163-shaped debt visible in the log: a message that
  discards a known value and forces a redundant store re-fetch should
  show up as `hasValue=true` immediately followed by an unexpected
  `store` category `source=disk` line for the same `pkg`.

### 3. store — persistence layer (ReviewStore, BuildStore, catalog)

- `op=<get/save/delete>`
- `pkg=<PackageName>`
- `source=<disk/cache/memory>` — the single highest-value field for
  spotting redundant-fetch debt: lets a reader see directly why a value
  already in hand was re-read from disk, without reading code.
- `hit=<true/false>` — for cache-style lookups, if/when a cache layer
  exists. Omit entirely for stores with no cache (don't log a
  meaningless `hit=false` on every call).

### 4. queue — Operations Queue lifecycle

- `op=<OperationKind>`
- `pkg=<PackageName>`
- `state=<enqueued/started/completed/failed>`
- `depth=<queue length at this moment>` — cheap to capture, useful for
  spotting queue buildup or confirming the one-at-a-time invariant
  (`developer-guide.md`: no concurrent builds).

### 5. handoff — navigation/screen transitions

- `from=<screen name>`
- `to=<screen name>`
- `via=<triggering Msg type>` — e.g. `BackMsg`, `PackageSelectedMsg`.

### Naming Rules

- **Same key, same meaning, everywhere.** `pkg` always means
  `PackageName` — never `package` in one call site and `pkgName` in
  another. A drifting key name silently breaks `grep pkg=` across the
  log.
- **Booleans are the literal strings `true`/`false`.** Not `1`/`0`, not
  `yes`/`no`. One canonical spelling across every category.
- **Errors are always a field, never folded into `event`.** `event`
  values must stay enumerable (`grep event=cmd`, `grep event=msg`, ...);
  free-text error content belongs in an `err=` value, not appended to
  or substituted for the event name.

### Scope Note

This section defines *conventions for instrumentation added in future
packets* (starting with #163). It does not itself add any `trace.Log`
call sites — per #164's packet, broad instrumentation across these five
categories is explicitly out of scope for #164 and happens incrementally,
issue by issue, starting with #163's specific needs.
