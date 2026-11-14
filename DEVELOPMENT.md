# Developing aur-curator

This is the starting point for working on aur-curator's code and
documentation. For installing and using aur-curator, see the
[README](README.md).

## Prerequisites

- Go 1.26.10 or newer (`go.mod` requires it: earlier 1.26 releases
  have known vulnerabilities in code aur-curator calls, #493).
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
| `make install` | Build and install to `~/.local/bin` (`PREFIX=` to change), and the fish completion when fish is installed. |
| `make test` | `go test ./...` |
| `make test-race` | `go test -race ./...` (needs cgo; slower). |
| `make test-sandbox` | Hostile fixture PKGBUILDs built in the real build sandbox: each attack must fail (#496). Needs bwrap and makepkg; run as a normal user. |
| `make lint` | `tools/tracecheck`, then `golangci-lint run ./...` (gosec included) |
| `make vuln` | `govulncheck` (pinned version) over `./...`; needs the network. |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w` across `cmd/`, `internal/` and `tools/`. |

`make lint` first runs `tools/tracecheck`, which fails on trace.Log calls 
 whose key/value arguments don’t pair up. The compiler can't catch
those, and they silently drop log fields.

### Before a change is merged

- `make build`, `make test` and `make lint` are clean. `make lint`
  includes gosec; a `//nolint:gosec` names the rule's reason not to
  apply, like every other `//nolint` (#493).
- New tests fail against the code before the fix, and pass after it.
- `make test-race` is clean when the change touches goroutines,
  `tea.Cmd` concurrency, or state shared between tests (for example
  parallel subtests).
- `make test-sandbox` is clean when the change touches
  `internal/sandbox`, `internal/build`'s makepkg path or the build
  environment (#496).
- The change has been run by hand against real packages. Passing tests
  are necessary but not sufficient: many defects in this project have
  only shown up in manual runs, especially around the TUI and around
  asynchronous operations.

### Manual testing

Some situations are hard to reach in daily use. These are ways to set
them up on purpose. The commands work in bash and fish unless marked
otherwise. Replace `<ws>` with your workspace (the config's `rootPath`)
and `<pkg>` with a package name. Each one says how to undo it.

#### Run the new build

Always run `./bin/aur-curator` from the repository after `make build`, not
the copy on your `PATH`, which is the last one installed.

#### A test workspace

For testing that needs packages in particular states, or a comparison
of the released build with the new one, use a second workspace that has
nothing to do with the one you use. **Never wipe your real workspace to
get test data:** its `repositoryPath` is the repository pacman installs
from, and its Gitea org holds your reviewed repositories.

A copy of your config with its own:

- `rootPath`, e.g. `~/aur-test`;
- `repositoryPath`, e.g. `~/aur-test-repo`, which is not in
  `pacman.conf`;
- `giteaOrg`, e.g. `aur-test`, created in Gitea first, so the test
  repositories never mix with your real ones.

**One package per state.** Import each package
(`./bin/aur-curator <test-config> import <pkg>`), then take it as far as
its row says in the TUI. Packages that build quickly keep this short.

| State | How |
| --- | --- |
| Review Pending | Import only |
| Rejected | Reject the review |
| Approved | Approve; do not merge |
| Merged | Approve, Merge |
| Built | … Build |
| Confirmed | … Confirm |
| Published | … Publish |

On top of those, as needed: a stale package (§ A stale package), local
commits (§ Local commits, on the Merged one), and failed reads (§ Failed
reads).

**Comparing two builds.** The released build is the one `make release`
installed (`aur-curator` on your `PATH`); the new one is `./bin/aur-curator`
(§ Run the new build). Both read and write the same workspace, one at a
time (the workspace lock). Look at a state with both before taking an
action, since an action changes the state for both. This works as long as
the change does not alter the on-disk format.

Undo: empty the test `rootPath` and `repositoryPath`, delete the
repositories in the test Gitea org, then import again. Only ever do this
with the test config.

#### Failed reads

To see how a failed merge check is reported (#462), on an Approved
package that is not merged:

- **An unknown reviewed commit:** back up
  `<ws>/.aur-curator/packages/<pkg>/review.json`, then set its
  `upstreamCommit` to `0000000000000000000000000000000000000001`. The
  Dashboard logs `[state] read merge state of "<pkg>"` in `errors.log`,
  and Package Details shows "Could not check merge state". `update` and
  `outdated` fail earlier, on the version read at that commit.
  Undo: copy the backup back.
- **No `master`:** `git -C <ws>/<pkg> branch -m master master-test`. Now
  `outdated` names the failed merge check and exits 1.
  Undo: `git -C <ws>/<pkg> branch -m master-test master`.

#### A stale package

To make the AUR check see a package as stale, point its review at an
older AUR commit whose version differs from the AUR's.

1. Back up the review:
   `cp <ws>/.aur-curator/packages/<pkg>/review.json /tmp/<pkg>-review.json`
2. List older commits that changed the PKGBUILD:
   `git -C <ws>/<pkg> log --oneline upstream/master -- PKGBUILD`
3. Check that one has a different version:
   `git -C <ws>/<pkg> show <commit>:PKGBUILD | grep -E '^(epoch|pkgver|pkgrel)='`
4. Get its full hash with `git -C <ws>/<pkg> rev-parse <commit>`, and the
   review's current one with
   `grep upstreamCommit <ws>/.aur-curator/packages/<pkg>/review.json`.
5. Replace the current hash with the older one. This changes both `id`
   and `upstreamCommit`:
   `sed -i 's/<current-hash>/<older-hash>/g' <ws>/.aur-curator/packages/<pkg>/review.json`
6. Start aur-curator and press `R`. Available shows the AUR's version.

Undo: copy `/tmp/<pkg>-review.json` back.

#### The AUR unreachable

Block outgoing traffic to the AUR's addresses with nftables, in a table
of its own (fish; needs root and `dig`):

```fish
sudo nft add table inet aurblock
sudo nft add chain inet aurblock out '{ type filter hook output priority 0; }'
for ip in (dig +short A aur.archlinux.org | grep -v '\.$'); sudo nft add rule inet aurblock out ip daddr $ip drop; end
for ip in (dig +short AAAA aur.archlinux.org | grep -v '\.$'); sudo nft add rule inet aurblock out ip6 daddr $ip drop; end
```

Then press `R`. Available and the AUR card show `?`, and the failure is
written to `<ws>/.aur-curator/errors.log`. The State column keeps its
versions, which come from git. Going offline works too, but blocks
everything else as well.

Undo: `sudo nft delete table inet aurblock`.

#### A fetch that times out

Point the package's `upstream` remote at an address that never answers:
`git -C <ws>/<pkg> remote set-url upstream https://10.255.255.1/x.git`,
then Fetch.

Undo: set it back with `git -C <ws>/<pkg> remote set-url upstream
https://aur.archlinux.org/<pkg>.git`.

#### Local commits

Add a commit to `master` that is not on the AUR:
`git -C <ws>/<pkg> commit --allow-empty -m "test: local commit"`. The
Local column shows `1`, and Build asks for confirmation.

Use a package you have merged. Until the first merge, `master` holds
only Import's empty placeholder commit, and the count is skipped, so an
empty commit on top of it shows nothing.

Undo, as long as it has not been pushed:
`git -C <ws>/<pkg> reset --hard HEAD~1`.

#### Dashboard paging

Paging needs more packages than fit on a page. A page holds as many rows
as the terminal has room for, down to one, so make the terminal shorter
until the list pages. Make it shorter still to see the "terminal too
small" line, which replaces the Dashboard when not even one row fits.

Undo: make the terminal taller again.

#### Example packages

| Case | Packages |
| --- | --- |
| Epoch in the version | `0wgram` (AUR `1:1.4.4-2`) |
| VCS, `pkgver=` near the top and `pkgver()` lower down | fastfetch-git, yay-git, pfetch-git, neovim-git |

To see where a package sets `pkgver` without importing it:
`curl -s "https://aur.archlinux.org/cgit/aur.git/plain/PKGBUILD?h=<pkg>" | grep -nE '^\s*pkgver'`

#### Config checks

To try a config change, run `./bin/aur-curator <copy> check` against a
copy of your real config rather than the config you use.

### Logging and debugging

| Mechanism | Enable with | Purpose |
| --- | --- | --- |
| Debug log | `--debug-log <path>`, or `AUR_CURATOR_DEBUG` (stderr) | Developer debugging output. |
| Trace log | `--trace <path>`, or `AUR_CURATOR_TRACE` (`<rootPath>/.aur-curator/trace.log`) | Correlated, per-operation tracing; each operation's lines share an ID. |
| Error log | always on (`<rootPath>/.aur-curator/errors.log`, not configurable) | Failures only, machine-parseable. |
| Activity log | always on (`activityLogPath`, default `<rootPath>/.aur-curator/activity.log`) | One human-readable line per lifecycle event. |

## Code layout

```text
cmd/aur-curator/        entry point, CLI commands, composition root;
                        cli.go: the command and flag table (#511)

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
internal/store/         store paths
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
internal/sandbox/       the build sandbox: the only package that knows bwrap
internal/sanitize/      cleans raw command output for display and logs
internal/activitylog/   always-on activity log
internal/errorlog/      always-on error log
internal/trace/         opt-in structured trace log
internal/debug/         debug log (--debug-log)
internal/version/       version string, set at build time

tools/tracecheck/       key/value argument checker for logging calls
contrib/completions/    fish completion, generated from cli.go (#511)
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
- **A released finding category is stored format.** Findings are saved
  with their category, so removing or renaming a category that has been
  in a release needs a migration that remaps stored findings
  (`docs/domain/risk-analysis.md` § Unmapped categories).

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

When a change alters documented behaviour, update the docs first: the
first commit on the issue branch, reviewed before any code, merged to
`main` together with the code so `main` stays releasable. Each change to
a canonical doc is reconciled across every other doc it affects.

## Git workflow

1. Every change starts with an issue.
2. Create a branch from `main` named `<type>/<issue>-<slug>`, e.g.
   `fix/392-config-load-direct` or `docs/415-post-v1-cleanup`. One branch
   per issue.
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

To release, from `main` with everything merged and pulled:

```sh
make release RELEASE=v1.2.3
```

It checks first, and stops before tagging anything if:
- `RELEASE` is not `vMAJOR.MINOR.PATCH`;
- you are not on `main`, the working tree has uncommitted changes, or
  `main` differs from `origin/main` after a fetch;
- the tag already exists, locally or on `origin`;
- `make test` fails;
- `make vuln` reports a vulnerability in code aur-curator calls (#493).

Then it tags `main`, pushes the tag to `origin` (Gitea), runs `make build`,
checks that `bin/aur-curator --version` prints the release, and runs
`make install`. If the build, the version check or the install fails, the
tag is already on `origin`: finish those steps by hand rather than
running `make release` again with the same version (#464).

When `make vuln` stops a release, read which module or Go release has
the fix (govulncheck's `Fixed in:`). For a module, `go get
<module>@<fixed version>` and `go mod tidy`; for the standard library,
raise the `go` line in `go.mod` (`go mod edit -go=<fixed version>`).
Merge that like any change, then release. If no fix exists yet, decide
from govulncheck's trace whether the release can wait.

By hand, the same release is:

```sh
make test && make vuln
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
make build
bin/aur-curator --version   # prints v1.2.3
make install
```

## AGENTS.md

`AGENTS.md` at the repository root is a short ruleset that AI coding
assistants read automatically. It restates rules from this file and from
the docs. It is not part of the documentation set, and the docs win if
the two ever disagree.
