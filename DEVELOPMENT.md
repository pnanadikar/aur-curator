# Developing aur-curator

This is the starting point for working on aur-curator's code and
documentation. For installing and using aur-curator, see the
[README](README.md).

## Prerequisites

- Go 1.26 or newer.
- [`golangci-lint`](https://golangci-lint.run/), for `make lint`.
- To run the app against real packages: Arch Linux with `base-devel`,
  `git` and `pacman`, plus a Gitea instance and organization you can
  create and delete test repositories in. The [README](README.md#configure)
  covers the configuration.

## Build, run and test

`make help` lists every target and the current version. The main ones:

| Target | What it does |
| --- | --- |
| `make build` | Optimized binary in `bin/aur-curator`. |
| `make debug-build` | Unoptimized binary (`bin/aur-curator-debug`) for a debugger. |
| `make run CONFIG=<path>` | Run from source against a config file. |
| `make install` | Build and install to `~/.local/bin` (`PREFIX=` to change). |
| `make test` | `go test ./...` |
| `make lint` | `tools/tracecheck`, then `golangci-lint run ./...` |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w` across `cmd/`, `internal/` and `tools/`. |

`make lint` first runs `tools/tracecheck`, which fails on trace.Log calls 
 whose key/value arguments don’t pair up. The compiler can't catch
those, and they silently drop log fields.

### Before a change is merged

- `make build`, `make test` and `make lint` are clean.
- New tests fail against the code before the fix, and pass after it.
- The change has been run by hand against real packages. Passing tests
  are necessary but not sufficient: many defects in this project have
  only shown up in manual runs, especially around the TUI and around
  asynchronous operations.

### Logging and debugging

| Mechanism | Enable with | Purpose |
| --- | --- | --- |
| Debug log | `--debug-log <path>`, or `AUR_CURATOR_DEBUG` (stderr) | Developer debugging output. |
| Trace log | `--trace <path>`, or `AUR_CURATOR_TRACE` (`<rootPath>/.aur-curator/trace.log`) | Correlated, per-operation tracing; each operation's lines share an ID. |
| Error log | always on (`<rootPath>/.aur-curator/errors.log`, not configurable) | Failures only, machine-parseable. |
| Activity log | always on (`activityLogPath`, default `<rootPath>/.aur-curator/activity.log`) | One human-readable line per lifecycle event. |

## Code layout

```text
cmd/aur-curator/        entry point, CLI commands, composition root

Domain and workflow
internal/domain/        domain structs (and store interfaces) only — no logic
internal/config/        config file loading
internal/catalog/       discovers curated packages under the workspace
internal/git/           Git provider and the Gitea API client
internal/review/        review engine
internal/analyzer/      analyzers that produce findings
internal/policy/        maps findings to severities and a risk assessment
internal/lifecycle/     derives a package's lifecycle state
internal/build/         build engine (makepkg backend)
internal/publish/       publishes artifacts to the local pacman repository
internal/repodb/        reads the local pacman repository database
internal/aurinfo/       AUR RPC queries for upstream versions
internal/store/         store paths and the ArtifactStore interface
internal/store/json/    JSON implementations of the three stores

TUI
internal/ui/            Bubble Tea app, screens and messages
internal/deps/          the Dependencies set injected into the app
internal/widgets/       reusable TUI components
internal/overlay/       centered overlay rendering (modals)
internal/theme/         Lip Gloss styles only
internal/help/          help text for the TUI and --help

Infrastructure
internal/instancelock/  one running aur-curator per workspace
internal/procgroup/     runs subprocesses in their own process group
internal/sanitize/      cleans raw command output for display and logs
internal/activitylog/   always-on activity log
internal/errorlog/      always-on error log
internal/trace/         opt-in structured trace log
internal/debug/         debug log (--debug-log)
internal/version/       version string, set at build time

tools/tracecheck/       key/value argument checker for logging calls
docs/                   design documentation
```

## Principles

The full set is in [`docs/domain/architecture.md`](docs/domain/architecture.md).
The ones that most often decide how code should be written:

- **Automate mechanics, never trust.** Nothing is merged, built or
  published without an explicit user decision.
- **Git owns repository state.** Commits behind, merge status and changed
  files are always asked of Git, never stored.
- **Store only irreducible state:** review decisions, build history and
  configuration. Everything else is derived on read.
- **Lifecycle state is derived**, never stored.
- **`internal/domain` holds structs only.** Logic goes in its own package.

[`docs/app-contract/developer-guide.md`](docs/app-contract/developer-guide.md)
§ Implementation Conventions & Gotchas lists the Bubble Tea rules that
came from real defects: value receivers on `App`, distinct message types
for distinct meanings, context lifetimes in messages, bounded scrollable
panes, and others. Read it before changing anything under `internal/ui`.

## Documentation

The docs in `docs/` are the source of truth for behaviour: code is
expected to match them, not the other way round.
[`docs/README.md`](docs/README.md) explains how they are organized and
which ones are canonical.

When a change alters documented behaviour, update the docs first, on
`main`, before the code branch is cut. Each change to a canonical doc is
reconciled across every other doc it affects.

## Git workflow

1. Every change starts with an issue.
2. Create a branch from `main` whose name starts with the issue number,
   e.g. `392-config-load-direct`. One branch per issue.
3. Commit on the branch. Reference the issue (`Refs #N`, or `Closes #N`
   on the commit or PR that completes it).
4. When build, tests, lint and the manual checks are clean, merge into
   `main` with `--no-ff`, so each change lands as an explicit merge
   commit, then delete the branch.

## Releases

aur-curator follows [semantic versioning](https://semver.org/); the
[README](README.md#versioning) states what counts as major, minor and
patch. The version is taken from the nearest `vMAJOR.MINOR.PATCH` tag by
`git describe` at build time.

To release, from an up-to-date `main` with everything merged:

```sh
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
make build
bin/aur-curator --version   # prints v1.2.3
```

## AGENTS.md

`AGENTS.md` at the repository root is a short ruleset that AI coding
assistants read automatically. It restates rules from this file and from
the docs. It is not part of the documentation set, and the docs win if
the two ever disagree.
