# UI Implementation

## Purpose

This document defines how the aur-curator user interface is implemented using Bubble Tea, Bubbles, and Lip Gloss.

Unlike `ui-overview.md`, which defines *what* the user interface does, this document defines *how* it is implemented.

The goals are:

- consistent architecture
- reusable components
- predictable interaction
- minimal implementation decisions
- identical behaviour across all screens

This document is normative. New UI code should follow these conventions rather than inventing new patterns.

---

## Architecture

The UI is composed of a single root Bubble Tea model.

```plaintext
Application
│
├── Dashboard
├── Package Details
├── Review
├── Build Log
├── Repository Status
└── Settings
```

Only one screen is active at any time.

Each screen owns its own Bubble Tea model.

Screens never communicate directly.

All navigation passes through the root application model.

---

## Directory Layout

```plaintext
internal/ui/

app.go
navigation.go
messages.go
theme.go

widgets/

    actionbar.go
    dialog.go
    statusbar.go
    spinner.go

screens/

    dashboard.go
    package_details.go
    review.go
    build_log.go
    repository_status.go
    settings.go
```

Shared widgets belong under `widgets`.

Screen-specific code belongs under `screens`.

Business logic never belongs under `ui`.

## Standard Screen Lifecycle

Every screen follows this pattern.

```plaintext
Init()

↓

Load data

↓

LoadedMsg

↓

Render

↓

User interaction

↓

Cmd

↓

CompletionMsg

↓

Refresh
```

No screen performs blocking work inside Update().

---

## Widget Ownership

The Root Model owns:

- navigation
- dialogs
- status bar

Each Screen owns:

- local state
- tables
- viewports
- text inputs
- focus

Widgets never own business logic.

---

## Standard Update Pattern

Update() should:

1. Handle messages.
2. Update child widgets.
3. Produce Cmd.
4. Return model.

Business logic belongs outside Update().

---

## Standard View Pattern

View() renders in this order:

1. Header
2. Body
3. Status Bar
4. Action Bar
5. Modal (if present)

All screens follow this layout.

---

## Responsibilities

## Root Model

The root model owns:

- current screen
- navigation
- global status bar
- active dialog
- background operation tracking
- global key handling

The root model does not contain business logic.

---

## Screen Models

Each screen owns:

- layout
- focus
- local widgets
- screen-specific commands

Screen models do not communicate directly.

---

## Domain Services

Git operations

Review storage

Analyzers

Builds

Publishing

remain outside the UI.

The UI invokes services through interfaces.

---

## Message Flow

Every background operation follows the same lifecycle.

```plaintext
Key Press

↓

Update()

↓

returns tea.Cmd

↓

Background Work

↓

tea.Msg

↓

Update()

↓

View()
```

The UI never polls for state.

All updates are message-driven.

---

## Navigation

Navigation always occurs through typed messages.

```plaintext
Dashboard

↓

Package Details

↓

Review

↓

Build Log
```

Screens request navigation.

Only the root model performs navigation.

---

## Focus Model

Exactly one component owns keyboard focus.

Focusable widgets implement:

```go
type Focusable interface {
    Focus()
    Blur()
    Focused() bool
}
```

The focused component displays:

- active border
- active title
- active cursor

Only the focused component processes keyboard events.

---

## Widget Catalogue

The following widgets are standard throughout the application.

| Widget | Implementation |
| ---------- | ---------------- |
| Package Table | bubbles/table |
| Findings Table | bubbles/table |
| Commit Table | bubbles/table |
| Diff View | bubbles/viewport |
| Build Log | bubbles/viewport |
| Spinner | bubbles/spinner |
| Help | bubbles/help |
| Action Bar | Custom |
| Status Bar | Custom |
| Dialog | Custom |

New widgets should only be introduced when existing widgets cannot satisfy the requirement.

---

## Theme

UI code uses semantic styles rather than concrete colours.

### Risk

- RiskCritical
- RiskHigh
- RiskMedium
- RiskLow
- RiskInfo

---

### Lifecycle

- Success
- Pending
- Warning
- Error
- Disabled

---

## Structural

- Border
- FocusedBorder
- Title
- Selected
- Subtle
- Hotkey

---

## Diff

- DiffAddition
- DiffDeletion
- DiffHeader
- DiffHunk
- DiffContext
- DiffLineNumber

UI code never references colour values directly.

---

## Tables

All tables share identical behaviour.

Navigation

- Up
- Down
- PageUp
- PageDown
- Home
- End

Selection

- single row
- optional multi-select

Columns define

- preferred width
- minimum width
- maximum width
- alignment
- truncation

Tables adapt to terminal width.

Lower-priority columns collapse before workflow-critical columns.

---

## Viewports

Viewports are used for:

- diffs
- logs
- analyzer output

Behaviour

- vertical scrolling
- horizontal scrolling
- no line wrapping

Standard navigation:

- j / k
- PageUp / PageDown
- Home / End

---

## Action Bar

The action bar displays available commands.

Example

```plaintext
[B]uild   [P]ublish   [I]nstall   [R]eview
```

Shortcut letters use the Hotkey style.

Unavailable actions remain visible but are dimmed.

---

## Status Bar

The status bar is global.

It displays:

- current package
- current repository
- background activity
- transient messages
- errors

Screens publish status messages.

Only the root model updates the status bar.

---

## Dialogs

Dialogs are owned by the root model.

A dialog temporarily receives exclusive keyboard focus.

```plaintext
Current Screen

↓

Dialog Opens

↓

Dialog Owns Focus

↓

Dialog Closes

↓

Focus Returns
```

Dialog types

- Information
- Confirmation
- Warning
- Error

Warning dialogs use stronger visual emphasis than confirmation dialogs.

Example:

Approving a package without running analyzers displays a Warning dialog.

---

## Progress

Long-running operations follow the same pattern.

```plaintext
Queued

↓

Running

↓

Completed

↓

Refresh
```

The UI remains responsive while work is executing.

Completed operations automatically transition to a dimmed appearance.

---

## Empty States

Every screen defines an explicit empty state.

Examples

- No packages
- No findings
- No builds
- No repository

Every empty state explains the next available action.

---

## Error Handling

Recoverable errors appear inline.

Destructive operations use modal dialogs.

Fatal errors terminate the operation but should leave the application usable.

---

## Screen Lifecycle

Every screen follows the same lifecycle.

```plaintext
Init()

↓

Load Data

↓

Loaded Message

↓

Render

↓

User Interaction

↓

Command

↓

Completion Message

↓

Refresh
```

No screen performs blocking work inside `Update()`.

---

## Diff Rendering

Every changed file is rendered independently.

Each file displays:

- filename
- file type
- patch

Colour conventions

- additions
- deletions
- hunk headers
- context
- line numbers

Long lines scroll horizontally.

Files are never concatenated into one viewport.

---

## Accessibility

Colour is never the sole indicator of meaning.

Every status includes text or symbols.

Examples

```plaintext
✓ Built

⚠ Warning

✗ Failed

● Pending
```

High-risk findings remain distinguishable in monochrome terminals.

---

## Screen Responsibilities

### Dashboard

Displays package summaries.

Owns

- package table
- filter controls

May navigate to

- Package Details

---

## Package Details

Displays one package.

Owns

- metadata
- repository summary
- action panel

May navigate to

- Review
- Build Log
- Repository Status

---

## Review

Owns

- commit table
- findings table
- diff viewport
- decision controls

May navigate back to Package Details.

---

## Build Log

Owns

- build viewport

---

## Repository Status

Owns

- repository information
- synchronization state

---

## Settings

Owns

- workspace configuration
- application preferences
