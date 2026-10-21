# Composition Root
>
> Last updated: 2026-07-24
> Status: Active
> Companion docs: developer-guide.md, architecture.md, interfaces.md
>
## Purpose

This document defines how aur-curator is assembled at application startup.

It answers:

- Which concrete implementations satisfy each architectural interface?
- In what order are objects constructed?
- Which objects own other objects?
- Where should dependency injection occur?

This document deliberately does **not** describe application behavior.
That belongs in `developer-guide.md`.

---

## Design Principles

### One Composition Root

The application has exactly one composition root:

```text
cmd/aur-curator/main.go
```

Only this package knows about concrete implementations.

Every other package depends only on interfaces.

No package may construct another subsystem internally.

---

### Constructor Injection

Dependencies are supplied explicitly through constructors.

Example:

```go
provider := git.New(runner, locator, forge)
```

not

```go
provider := git.New()
```

followed by mutating setters.

Construction should produce a fully usable object.

---

### Depend on Interfaces

Subsystems own the interfaces they consume.

For example:

```text
internal/git
    Runner
    RepositoryLocator
    Forge
```

are defined by `internal/git`, not by the packages implementing them.

This follows normal Go practice:

> consumers define interfaces.

---

### Singleton Lifetime

Unless stated otherwise, every service is constructed exactly once during
application startup.

Examples:

- Git Provider
- Review Engine
- Package Catalog
- Builder
- Publisher

These are long-lived application services.

Review records, Build records, Findings and similar domain entities are
created as needed and are not singletons.

---

## Startup Sequence

The application is assembled in the following order.

```text
Configuration
        │
        ▼
Infrastructure
        │
        ▼
Persistence
        │
        ▼
Services
        │
        ▼
UI
        │
        ▼
Bubble Tea Program
```

Each layer may depend only on layers above it.

---

## Construction Order

### 1. Load Configuration

Configuration is loaded first.

Typical values include:

- workspace root
- Gitea connection
- builder configuration
- UI preferences

No other subsystem should read configuration directly.

Configuration is passed into constructors.

---

### 2. Construct Infrastructure

Infrastructure provides low-level capabilities.

Examples:

```text
ExecRunner
HTTP client
Filesystem
Clock (future)
```

These objects contain no business logic.

---

### 3. Construct Persistence

Persistence implementations are created next.

Examples:

```text
JSONReviewStore
SQLiteReviewStore (future)
BuildStore
```

Only one implementation of each persistence interface is active.

Consumers remain unaware of the backend.

---

### 4. Construct Service Implementations

Business services are assembled from infrastructure and persistence.

Example:

```text
RepositoryLocator
            │
            ▼
Git Provider
```

```text
ReviewStore
Analyzer Manager
Policy Engine
            │
            ▼
Review Engine
```

Builder and Publisher follow the same pattern.

---

### 5. Assemble Dependencies

All service interfaces are gathered into a single value.

Conceptually:

```go
deps := ui.Dependencies{
    Git:        gitProvider,
    Catalog:    catalog,
    Reviews:    reviewEngine,
    Analyzers:  analyzerManager,
    Policy:     policyEngine,
    Builder:    builder,
    Publisher:  publisher,
    ReviewDB:   reviewStore,
    BuildDB:    buildStore,
    Config:     config,
}
```

No subsystem receives more dependencies than it requires.

---

### 6. Construct the UI

The Bubble Tea application is created once.

```go
app := ui.NewApp(deps)
```

Sub-models receive the same dependency set through the root model.

No screen constructs services itself.

---

### 7. Start Bubble Tea

Finally:

```go
program := tea.NewProgram(app)
```

The composition root's responsibility ends once the program starts.

---

## Dependency Graph

```text
                         Configuration
                               │
         ┌─────────────────────┴─────────────────────┐
         │                                           │
         ▼                                           ▼
  Infrastructure                              Persistence
         │                                           │
         └─────────────────────┬─────────────────────┘
                               ▼
                          Application Services
                               │
                               ▼
                        ui.Dependencies
                               │
                               ▼
                             App
                               │
                               ▼
                         Bubble Tea
```

Dependencies always point downward.

No lower layer constructs an upper layer.

---

## Ownership

| Object | Owner |
| ---------- | ------- |
| Configuration | main |
| ExecRunner | main |
| RepositoryLocator | main |
| Forge | main |
| Git Provider | main |
| Review Store | main |
| Build Store | main |
| Analyzer Manager | main |
| Policy Engine | main |
| Review Engine | main |
| Builder | main |
| Publisher | main |
| App | main |

Ownership transfers only through constructors.

No global singletons should exist.

---

## Testing

The composition root is intentionally small.

Most tests instantiate individual services directly.

Example:

```go
provider := git.New(
    fakeRunner,
    fakeLocator,
    fakeForge,
)
```

Tests should avoid constructing the complete application unless
specifically performing integration testing.

---

## Future Evolution

Adding a new subsystem should require only two changes:

1. construct the implementation in the composition root
2. inject the corresponding interface where needed

Existing services should never construct the new subsystem themselves.

This preserves loose coupling and keeps dependency direction consistent
throughout the application.
