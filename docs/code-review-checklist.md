# Code Review Checklist: aur-curator

> Purpose: a repeatable procedure for reviewing an implementation slice
> against this project's canonical docs. Written so a resource-limited
> local model can follow it mechanically — but see "Limits of a checklist"
> at the end before trusting its output unsupervised.

---

## Canonical document precedence

If two documents disagree, prefer canonical over derived documentation.
If two canonical documents disagree, stop and report the inconsistency 
rather than choosing one.

## 0. Before reading any code

- Identify which canonical doc defines the **scope** of what's being
  reviewed (for a vertical slice, that's `developer-guide.md` § Vertical
  Slice — Current Scope). Read its "Explicitly out of scope" list first.
  Anything on that list is not a defect if it's missing or stubbed.
- Note whether a Go toolchain is available. If not, say so up front and
  review statically — don't imply things were compiled/run when they
  weren't. Recommend `go build ./...`, `go vet ./...`, `go test ./...` as
  a follow-up the author should run.
- Confirm `go.mod`/`go.sum` are present. If absent, that's a Low finding
  on its own (can't confirm module path, Go version, pinned deps).

## 1. Scope conformance

- For each file/function, ask: does `developer-guide.md`'s scope section
  claim this? If it's marked "implementer's discretion," a stub or
  placeholder is fine — don't flag it as incomplete.
- For anything *not* mentioned in scope at all (e.g. Import flow, Findings
  subview), check whether it's a tracked open item elsewhere (roadmap,
  memory, a comment in the code itself) before flagging it as a gap. If it
  is tracked, note it as **Info**, not a defect.

## 2. Trace the state machine — don't just read functions in isolation

This is the highest-value check for a Bubble Tea app and the one a
pattern-matching read is most likely to miss.

- For every `Update()`, list every `tea.Msg` type it switches on, and for
  each one, walk what happens to *every* stateful field (`m.err`,
  `m.loading`, `m.modal`, focus flags) — not just the field the message
  type obviously touches.
- When a new failure/error state is introduced (e.g. `m.err`), check every
  *other* guard clause in the same `Update()` to confirm it also accounts
  for that state. A common bug shape: a guard is added where the error is
  set, but sibling code paths (later key handling, other message types)
  only check the original guard (`m.loading`) and not the new one.
- When two widgets can both receive the same `tea.Msg` (e.g. a text input
  and a scrollable viewport both getting forwarded a `tea.KeyMsg`), check
  whether they have real focus exclusivity or whether they're both live at
  once. If both are live, walk their default keymaps for overlapping keys
  (viewport's `j`/`k`/`g`/`G`/arrows are a classic collision with any
  adjacent free-text field).
- **Standard fix, not a one-off:** Bubble Tea has no built-in focus
  manager — it's a bare Elm-architecture loop with no opinion about focus
  across sub-widgets. Some bubbles (`textinput`, `textarea`) happen to
  carry their own internal `Focused()`/`Focus()`/`Blur()` because they
  need it to render a cursor; others (`viewport`, most custom widgets)
  have no focus concept at all. Don't branch on a widget's own
  `.Focused()` to decide message routing — that only works for the
  subset of widgets that expose it, and won't generalize as more panes
  are added. Instead, the screen should track **one explicit focus field**
  (an enum: `focusNotes`, `focusDiffPane`, etc.) as the single source of
  truth, route every `tea.KeyMsg` to the owner named by that field first,
  and only match global single-key shortcuts when the owner isn't a
  free-text widget. Every place a widget is currently `.Focus()`'d/
  `.Blur()`'d should also update this field at the same call site. This is
  the standard idiom in multi-pane Bubble Tea apps (Soft Serve, Glow,
  etc.) — check for it by name (`focus`/`activePane`-style field) rather
  than accepting ad hoc `.Focused()` checks scattered across the code.
- When a key is "reserved" for a not-yet-implemented action (e.g. a
  dimmed `A Run Analyzers`), check where in the handler chain it's
  intercepted. If it's intercepted *before* being forwarded to a text
  input that shares the same character, that's a bug even though the
  "dimmed action = no-op" convention technically justifies swallowing it —
  the convention was written for action buttons, not characters that also
  need to be typeable.

## 3. Domain conformance

- For every stored field written in code, find its definition in
  `domain.md` and `go-domain-structs.md` and check the description
  matches how the code populates it — not just that the field exists with
  the right type.
- Specifically check: is the field ever left at a default/zero value in a
  situation the domain doc implies it should be populated (or vice
  versa)? `rationale`/`reviewerNotes` in this codebase is a live example —
  the struct is correct, but *when* each field gets written doesn't fully
  match what `domain.md` says.
- If two canonical docs describe the same field differently (e.g.
  `domain.md` vs `tui-review.md` on what `rationale` maps to), don't
  silently pick one and grade the code against it. Flag the doc ambiguity
  itself as a finding, and note which interpretation the code currently
  implements.

## 4. Trust-model / data-integrity impact (project-specific)

This project's core invariant is: **never record a trust decision that
wasn't actually backed by a real review.** Any code path that can produce
a persisted `Review`/`Build`/`Artifact` record without the corresponding
upstream data actually having been loaded/verified is a **High** finding
regardless of how unlikely the path is to trigger in practice.

- Check every place a decision (Approve/Reject/Confirm/etc.) can be
  triggered, and confirm it's unreachable unless the data it's deciding
  about was successfully loaded.
- Check persistence writes (`ReviewStore.Save`, etc.) for atomicity if the
  store is meant to be the durable/irreducible record per
  `architecture.md` — a plain `os.WriteFile` to the live path is a Medium
  finding for anything framed as an audit trail.

## 5. Symmetry check

- When two code paths look like they should be structurally parallel
  (e.g. `openApproveModal` vs `openRejectConfirmModal`), diff them against
  each other line by line. Most differences are benign, but occasionally
  one path is missing something the other has (a `Focus()` call, a nil
  check) — confirm each difference is intentional rather than assuming
  symmetry was maintained.

## 6. Test coverage vs. actual bug surface

- Existing tests passing doesn't mean the specific bug you're worried
  about is covered — check what each test actually asserts, not just that
  a test with a relevant-sounding name exists.
- Note gaps explicitly (e.g. "no test exercises the load-error + approve-
  key path") rather than assuming untested code is untested *because*
  it's fine.

## 7. Severity rubric (project-specific ordering)

1. **Critical** — breaks the core trust invariant outright (auto-approve,
   auto-merge, auto-install, or any bypass of explicit user decision).
2. **High** — real, user-reachable bug: data integrity risk, or a
   documented UI element that doesn't actually work as specified (can't
   type a character, action fires when it shouldn't).
3. **Medium** — conformance gap or unresolved ambiguity between docs and
   code; not user-broken today but will cause a wrong decision later.
4. **Low** — style/maintainability; won't cause incorrect behavior.
5. **Info** — tracked/known gap, or a note for a future slice; not
   actionable now.

## 8. Output format

- Group findings by severity, each with: file/function, a short code
  excerpt if it clarifies the bug, what's wrong, and a concrete
  suggestion (not just "this looks wrong").
- Always include a **"What's working well"** section. A review that's
  100% findings is less useful than one that also says which parts are
  solid — it calibrates how much the author should trust the rest of the
  code that *wasn't* flagged.
- End with a short, explicitly ordered "fix this first" list — severity
  tags alone don't tell the author what to actually do Monday morning.

---

## Limits of a checklist

Some of the highest-value findings above (§2 in particular) require
actually simulating execution — mentally running the state machine
forward across multiple messages — not just scanning for a pattern. A
less capable agent can follow the *procedure* (list the messages, list
the fields, check each guard) but may still miss a bug if it can't hold
the full trace in mind at once. Treat this checklist as reducing the
floor, not replacing careful tracing for anything touching the review/
decision path — that code should probably always get a second, careful
pass regardless of what a checklist-driven pass finds.

When a check in this document surfaces a genuine ambiguity *between two
canonical docs* (not just a code-vs-doc mismatch), the right move is to
flag it and stop — not to silently resolve it one way and grade the code
against that guess. Doc-vs-doc conflicts are a human decision.
