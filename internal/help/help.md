# aur-curator

aur-curator is a review-gated workflow tool for the Arch User Repository
(AUR). It provides a TUI dashboard to import, review, merge, build,
confirm, and publish curated packages into a local pacman repository.

aur-curator never installs or uninstalls packages. Once a package is
published, install and upgrade it with plain pacman from the local
repository (`pacman -S <pkg>`, `pacman -Syu`).

It also ships CLI subcommands for one-shot operations that do not require
the TUI.

## Dashboard

One row per curated package, under a header row naming the columns:

| Column | Shows |
| --- | --- |
| Package | The package name; `>` marks the selected row |
| State | The lifecycle state, then its versions (below) |
| Next Action | What the state calls for next, or `—` |
| Available | The AUR's version, when it differs (below) |
| Published | `Yes`, `Older` (an older version is) or `No` |
| Risk | Highest finding severity, `NONE`, or `Not Run` |
| Local | Commits on your `master` not on the AUR (`?`: unknown) |
| Behind | `↑N`: AUR commits fetched, not merged yet |

State's versions:

| State | Example | Version |
| --- | --- | --- |
| Review Pending | `Review Pending 1.10.0-1 → 1.11.0-1` | before → reviewed |
| Review Pending, first review | `Review Pending → 1.11.0-1` | reviewed |
| Approved | `Approved 1.10.0-1 → 1.11.0-1` | before → reviewed |
| Approved, first review | `Approved → 1.11.0-1` | reviewed |
| Rejected | `Rejected 1.11.0-1` | reviewed |
| Merged to Published | `Merged 1.11.0-1` | on your `master` |

A version that cannot be read is left out.

Available:

| Value | Meaning |
| --- | --- |
| `y` | The AUR has `y`, not the version you reviewed |
| `—` | Up to date |
| `not on AUR` | Not on the AUR |
| `?` | Could not compare, or the check failed |
| `…` | Check still running |

Keys:

- `↑`/`↓` or `k`/`j`: move, and page through the list
- `Enter`: open the package
- `?`: list every key

## Review findings

Running the analyzers (`a` on the Review screen) reads the change being
reviewed and points at the parts most likely to matter. Nothing is run:
the PKGBUILD is read as text. Findings point at changes; they are not a
verdict. A change with no findings still has to be read: the diff is
the review.

| Analyzer | Reads | Reports (default severity) |
| --- | --- | --- |
| Shell | lines the change adds to the PKGBUILD and `.install` files | commands that run other code (`eval`, `bash -c`, `$(…)`, backticks, `source`, a pipe into `sh`) or use the network (`curl`, `wget`, `git clone`, `nc`, `/dev/tcp/`): High. The same on a line starting with `#`: Low, as "Comment line"; usually a comment, but such a line runs inside a multi-line string or heredoc, so read it in the diff. A `.install` file added or changed (pacman runs it as root): Medium |
| URL | `source` arrays of the old and new PKGBUILD | a download URL the old file did not have: High |
| Depends | dependency arrays of the old and new PKGBUILD | packages added to `depends`, `makedepends`, `checkdepends` or `optdepends`: Medium |
| Checksum | `validpgpkeys`, checksum arrays, `pkgver` and `pkgrel` of the old and new PKGBUILD | every PGP key removed: High; a key added or replaced: Medium; a checksum replaced by `SKIP` or emptied: High; checksums changed: Low; version changed: Low |
| Pkgbuild | arrays of the new PKGBUILD | an array the analyzers could not read: High. Check those lines in the diff |
| AUR | the package's AUR record, read while the analyzers run | the maintainer or co-maintainers differ from the last review that recorded them (a new maintainer, orphaned, adopted): Medium. An adoption can be legitimate; read the diff with it in mind |

The AUR check is skipped, and the Findings tab says why, when there is no
earlier record (a first review), when the AUR could not be read, or when
the earlier reviews were approved without analysis. A first review also
shows when the package was first submitted, its votes and popularity: a
days-old package with no votes deserves a slow read.

Arrays are compared entry by entry, wherever they are in the file and
however they are quoted: moving or reformatting an entry is not a
finding. What they cannot see:

- Variables are not expanded. A `source` entry written as
  `${url}/x.tar.gz` reads the same when `url=` changes: check that line
  yourself. Brace patterns (`qt5-{base,svg}`) are compared as one word.
- Every assignment counts, in any `if` or `package_*()` function, even
  where only one would run.
- Checksums are compared by position: when sources are added or
  removed, a changed checksum can read as removed or the other way
  round. A deleted checksum array is not reported (makepkg refuses to
  build sources no checksum verifies), so a switch to a weaker remaining
  algorithm shows only in the diff.

A control character (`\x1b`, `\r`, …) or an invisible bidi or zero-width
character (`\u202e`, `\u200b`, …) in the diff, a finding or a commit is
shown as a red escape, not passed to the terminal: such characters can
hide a line or show text over it. Treat one in a PKGBUILD as suspect.

## Usage

```
aur-curator [flags] <config-file>
aur-curator [flags] <config-file> import <package-name>
aur-curator [flags] <config-file> update
aur-curator [flags] <config-file> outdated
aur-curator [flags] <config-file> check
aur-curator [flags] <config-file> remove <package-name>
```

With no command, the TUI starts. A mistyped command or flag is reported
before the config file is read.

## Commands

**`import <package-name>`**
Import a package from upstream (AUR). Clones the repository into the
workspace, creates a Review record, and — if `AutoAnalyzeAfterImport` is
enabled — runs analyzers to produce findings. Exits after import.

**`update`**
Check each package's reviewed commit against the AUR and offer a fetch
where the AUR has moved on. A package whose fetch would discard something
(a pending review, or an approved review not yet merged) is asked about
on its own, with a warning: y fetch, n skip, q skip all remaining.
Everything else with a different AUR version (merged, or rejected) is offered
one at a time, after a list when there is more than one: y fetch, n skip,
a fetch this and all remaining, q skip all remaining. End of input counts
as q. Never installs anything.

Ends with a one-line summary. Every package that could not be checked or
fetched is named with the reason; so is every package whose version
cannot be compared (a computed or missing version, or not on the AUR). Exit
status: 0 nothing to review; 10 at least one review is pending; 1 a check
or fetch failed.

**`outdated`**
List the packages the AUR has a different version for, one per line, and
change nothing: `<name> <reviewed> -> <aur>`, marked
`(older review pending)` or `(approved, not merged)` when a fetch would
discard something. For status bars and scripts. Packages whose version cannot be compared are left out;
a failed check is named on stderr. Exit status: 0 whether or not anything
is listed; 1 a check failed or the AUR could not be reached. Like every
command, it does not run while the TUI is open.

**`check`**
Run internal consistency checks: catalog ↔ store cross-validation,
dangling artifact references, orphaned artifacts, missing binaries, and
illegal lifecycle states. Auto-fixes what it can (orphan cleanup, dangling
refs); reports-only findings cause a non-zero exit.

**`remove <package-name>`**
Tear down a fully curated package. Prompts for confirmation, then deletes
the Gitea repository, removes build binaries, deletes store records
(reviews, builds, artifacts), and removes the local clone. Does not touch
the shared local pacman repository, and does not uninstall the package: if
it is installed, the summary says so and how to remove it
(`sudo pacman -Rs <package-name>`).

## Flags

| Flag | Description |
| --- | --- |
| `--debug-log <path>`, `-debug-log <path>` | Write debug logs to the given file. |
| `--trace <path>`, `-trace <path>` | Enable trace logging to the given file. |
| `--help`, `-h` | Show a short summary of the commands, flags and environment variables, and exit. |
| `--help=full` | Show this guide and exit. |
| `--version`, `-version` | Print version and exit. |

`make install` also installs a fish completion (flags, config files,
commands, and curated packages for `remove`) when fish is installed.

## Environment variables

**`AUR_CURATOR_DEBUG`**
When set and `--debug-log` is not given, debug logs go to stderr. The flag
takes precedence.

**`AUR_CURATOR_TRACE`**
When set and `--trace` is not given, trace logs go to
`<workspace>/.aur-curator/trace.log`. The flag takes precedence.
