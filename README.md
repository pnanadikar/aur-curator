# aur-curator

aur-curator keeps a personally reviewed copy of the AUR packages you rely
on, and builds them into a local pacman repository.

It is **not** an AUR helper. An AUR helper trusts whatever the AUR serves
at install time. aur-curator treats the AUR as an upstream source of
package recipes: every change is shown to you as a diff, and nothing is
merged, built or published until you have approved it.

```text
AUR upstream
     │  fetch
     ▼
Review the changes  ──►  Reject: nothing happens
     │  approve
     ▼
Merge into your own Git repository (on your Gitea)
     │
     ▼
Build locally with makepkg  ──►  Confirm  ──►  Publish to a local pacman repo
                                                        │
                                                        ▼
                                              pacman -S / pacman -Syu
```

Guiding principle: **automate the mechanics, never automate trust.**
aur-curator fetches, diffs, analyzes, builds and publishes for you. Every
trust decision is yours.

aur-curator never installs or uninstalls packages. Once a package is
published, you install and upgrade it with plain pacman.

## Features

- Terminal UI dashboard showing every curated package, its lifecycle state
  and the next action to take.
- Review screen with diffs of `PKGBUILD`, `.SRCINFO`, `.install` and
  patches, compared against the last commit you reviewed.
- Analyzers that flag risky changes (new download URLs, removed checksum
  or PGP verification, shell execution) as findings. Findings are
  highlighted, never blocking: you decide. Help (§ Review findings) says
  what each analyzer reads and what it cannot see.
- A personal Gitea repository per package, so your reviewed history is an
  audit trail independent of the AUR.
- Local builds with `makepkg`, an explicit Confirm step, then publishing
  to a local pacman repository that any of your machines can use.
- CLI commands for import, update checks, consistency checks and removal.

## Requirements

- Arch Linux (or a derivative with pacman).
- `base-devel` (for `makepkg`), `git` and `pacman` (for `repo-add`).
- `bubblewrap` 0.8 or newer, and a kernel that lets an unprivileged
  user create user namespaces (Arch's stock kernel does): every build
  runs in a bubblewrap sandbox, and there is no way to build without
  it. `aur-curator <config-file> check` says whether the sandbox works.
- Go 1.26 or newer, to build aur-curator.
- A [Gitea](https://about.gitea.com/) instance, an organization on it to
  hold the curated package repositories, and an account that can create
  and delete repositories in that organization.

## Install

```sh
git clone https://github.com/pnanadikar/aur-curator.git
cd aur-curator
make install
```

This builds the binary and installs it to `~/.local/bin/aur-curator`
(make sure `~/.local/bin` is on your `PATH`). If fish is installed, it also
installs a fish completion to `~/.config/fish/completions/` (or under
`$XDG_CONFIG_HOME`). To install elsewhere:

```sh
sudo make install PREFIX=/usr/local
```

As root, the fish completion is not installed; copy
`contrib/completions/aur-curator.fish` to `~/.config/fish/completions/`
yourself.

Check it works:

```sh
aur-curator --version
```

## Configure

### 1. Gitea API token

In Gitea, go to **Settings → Applications** and create an access token
with these scopes:

| Scope | Used for |
| --- | --- |
| `write:organization` | Import: creating a package repository in your organization |
| `write:repository` | Remove: deleting a package repository |

Provide the token through the environment rather than the config file:

```sh
export AUR_CURATOR_GITEA_TOKEN=<your-token>          # bash/zsh
set -x AUR_CURATOR_GITEA_TOKEN <your-token>          # fish
```

The environment variable always takes precedence. A `giteaToken` key in
the config file also works; if you use it, keep the file private
(`chmod 600`). aur-curator warns at startup if the file is readable by
other users.

If Gitea rejects the token (401) or it lacks a scope or permission (403),
Import and Remove say so, include Gitea's own message, and name where the
token came from: the environment variable or the config file.

### 2. Git credentials for pushing

The API token above is used only for creating and deleting repositories.
When aur-curator merges a reviewed update, it pushes to your Gitea
repository with plain `git push` over HTTPS, so **git** must be able to
authenticate to your Gitea host without prompting.

Configure a git credential helper for the host, for example:

```sh
git config --global credential.helper libsecret   # or store, gopass, …
```

Then push once by hand to any repository on that host, so the helper
stores your credentials. Gitea accepts an access token as the password.

If your Gitea uses a self-signed or private CA certificate, add the CA to
the system trust store (`trust anchor <ca.crt>`), so both git and
aur-curator can connect.

### 3. The config file

Copy the example and edit it:

```sh
mkdir -p ~/.config/aur-curator
cp aur-curator.example.jsonc ~/.config/aur-curator/aur-curator.jsonc
```

Required settings:

| Key | Meaning |
| --- | --- |
| `rootPath` | Workspace: one Git repository per curated package, plus aur-curator's own state in `<rootPath>/.aur-curator/`. |
| `repositoryPath` | The local pacman repository that packages are published to. |
| `giteaBaseURL` | Your Gitea instance, e.g. `https://gitea.example.com`. `http://` is refused unless the host is `localhost` or a loopback address, because every request carries your token. |
| `giteaOrg` | The Gitea organization that holds the package repositories. |

Optional settings:

| Key | Default | Meaning |
| --- | --- | --- |
| `repoDBName` | `custom.db.tar.gz` | Repository database file name. The part before `.db` is the repository name in `pacman.conf`. |
| `builderBackend` | `makepkg` | Build backend. `makepkg`, always in the bubblewrap sandbox, is the only one; any other value is an error. |
| `autoMerge` | `false` | Merge as soon as you Approve a review, instead of waiting for you to press Merge. Nothing is ever merged without an Approve. |
| `autoAnalyzeAfterImport` | `false` | Run the analyzers right after an Import. |
| `activityLogPath` | `<rootPath>/.aur-curator/activity.log` | Human-readable log of every import, decision, merge, build, confirm and publish. |

The file is JSON with comments (`//` and `/* */`). Trailing commas are not
allowed. Paths must be absolute or start with `~/`. `repoDBName` must end
in `.db.tar.gz`, and `repositoryPath` must not be inside `rootPath` (or
the other way round). aur-curator warns about keys it does not know, and
refuses to start if a key differs from a known one only in case
(`AutoMerge`).

### 4. The local pacman repository

Create the repository directory. pacman downloads packages as an
unprivileged user, so the directory, and every directory above it, must
be readable by that user. A path outside your home directory avoids
problems:

```sh
sudo mkdir -p /srv/aur-curator/repo
sudo chown $USER: /srv/aur-curator/repo
```

Add the repository to `/etc/pacman.conf`. The section name must match
`repoDBName` without `.db.tar.gz` (`custom` by default), and must come
**before** the official repositories if you want your curated builds to
take precedence:

```ini
[custom]
SigLevel = Optional TrustAll
Server = file:///aur-curator/repo
```

`SigLevel = Optional TrustAll` is needed because aur-curator does not sign
packages yet. Your trust in these packages comes from your own review,
not from a signature.

Other machines can use the same repository, for example over HTTP or a
shared mount, with plain pacman. aur-curator only needs to run on the
machine that builds.

## Usage

```text
aur-curator [flags] <config-file>                        start the TUI
aur-curator [flags] <config-file> import <package-name>  curate a new package
aur-curator [flags] <config-file> update                 check curated packages against the AUR
aur-curator [flags] <config-file> outdated               list packages the AUR has something different for
aur-curator [flags] <config-file> check                  run consistency checks
aur-curator [flags] <config-file> remove <package-name>  stop curating a package
```

`aur-curator --help` lists the commands, flags and environment variables;
`aur-curator --help=full` (or `?` in the TUI) shows the full guide.

### Curating your first package

1. **Import** it:

   ```sh
   aur-curator ~/.config/aur-curator/aur-curator.jsonc import <package-name>
   ```

   This creates the Gitea repository, clones the AUR package into your
   workspace and opens a review. Nothing is pushed to Gitea yet.

2. **Start the TUI** and open the package:

   ```sh
   aur-curator ~/.config/aur-curator/aur-curator.jsonc
   ```

   The action bar at the bottom of each screen shows the available keys.
   Actions that are not possible in the package's current state are
   dimmed. Press `4` for Help.

3. **Review.** For a newly imported package the whole package is shown as
   added. Run the analyzers, read the diff and the findings, then
   **Approve** or **Reject**. Rejecting records the decision and does
   nothing else.

4. **Merge** the approved review (automatic if `autoMerge` is on). This
   commits the reviewed upstream state to your local repository and
   pushes it to Gitea.

5. **Build.** aur-curator builds the package with `makepkg` from a clean
   copy of exactly the commit you merged, in a sandbox that sees only
   the system (`/usr`, `/etc`, `/opt`, the pacman database) and the
   build's own directory: not your home directory, other disks, your
   other packages, the repository or your session's sockets. It still has the network. Toolchains installed
   only under your home directory (`~/.cargo/bin`, `~/go/bin`) are not
   visible to it, and language caches there are not reused.

6. **Confirm** the build. This is an explicit acknowledgement that the
   built package is what you want to publish; aur-curator does not
   publish anything you have not confirmed.

7. **Publish.** The package is copied into your local pacman repository
   and the repository database is updated.

8. **Install** it with pacman:

   ```sh
   sudo pacman -Sy <package-name>
   ```

### Day to day

- Run `aur-curator <config> update`, or Fetch in the TUI, to pick up new
  AUR commits. Each update opens a new review showing exactly what changed
  since the last one you reviewed.
- `update` ends with a one-line summary, and names every package it could
  not check or fetch, with the reason. Its exit status says whether there
  is anything to do:

  | Exit | Meaning |
  | --- | --- |
  | 0 | Nothing to review |
  | 10 | At least one review is pending — open the TUI |
  | 1 | A check or fetch failed (named above the summary) |

- `outdated` lists the packages the AUR has a different version for, one
  per line, without fetching or changing anything:

  ```text
  pfetch 1.11.0-1 -> 1.12.0-1
  yay 12.4.1-1 -> 12.4.2-1 (older review pending)
  ```

  It exits 0 whether or not anything is listed, and 1 when a check failed
  or the AUR could not be reached; errors go to stderr. Like every
  command, it does not run while the TUI is open. A status bar can count
  its lines, for example a waybar module:

  ```jsonc
  "custom/aur": {
    "exec": "aur-curator ~/.config/aur-curator/aur-curator.jsonc outdated | wc -l",
    "interval": 1800,
    "format": "AUR {}"
  }
  ```

  or a fish function for a shell greeting:

  ```fish
  function aur_outdated
      aur-curator ~/.config/aur-curator/aur-curator.jsonc outdated 2>/dev/null
  end
  ```

- Review, merge, build, confirm and publish as above.
- Upgrade with `sudo pacman -Syu`, as for any other repository.
- To stop curating a package, run `aur-curator <config> remove
  <package-name>`. This deletes its Gitea repository, its local clone and
  its aur-curator records. It does not uninstall the package and does not
  remove it from the local pacman repository.

### Reading a review in your own tools

On the Review screen, `d` opens the change in your own diff tool through
`git difftool`: the reviewed range, every file of the tab you are on, one
after another, with git's usual prompt. Your git configuration decides
which tool (`diff.tool`): git runs in the package's clone under
`rootPath`, so it reads your global configuration (`git config --global
diff.tool vimdiff`) or that clone's own, never the directory you started
aur-curator from. `v`, on Review and Package Details, opens the
tab's content in `$PAGER` (`less -R` if unset); `PAGER=bat` highlights
diffs. Quit the tool to return to aur-curator.

**These files are untrusted.** They come from the AUR, and the point of
reviewing them is that they may be hostile. Tools can run code from the
files they open:

- **vim and vimdiff modelines** (a line like `# vim: …`) set options when
  a file is opened, and have been exploited to run commands
  (CVE-2019-12735). Turn them off for the diff tool. The option must be
  given with `--cmd`, which vim applies before reading the files; with
  `-c` it comes too late and the modeline still applies:

  ```sh
  git config --global difftool.vimdiff.cmd 'vimdiff --cmd "set nomodeline" "$LOCAL" "$REMOTE"'
  ```

  For nvim, `nvimdiff` takes the same `--cmd`.
- **Editor plugins** (language servers, linters) may run code or tools
  on a file they open. Use a plain diff tool, or a profile without them.
- **`LESSOPEN`** (lesspipe) runs a program on every file `less` opens.
  aur-curator removes `LESSOPEN` and `LESSCLOSE` from the pager's
  environment: the file `v` writes is plain text and needs no filter.
  Other pagers' own filters are your configuration.

git does not report a diff tool's own exit status unless
`difftool.trustExitCode` is set, so a tool that fails may simply return
you to aur-curator.

### Files aur-curator creates

- `<rootPath>/<package>/`: the package's Git repository. `origin` is your
  Gitea repository, `upstream` is the AUR.
- `<rootPath>/.aur-curator/`: reviews, build records, build logs and the
  logs described below. Do not edit these by hand. A lock file here
  allows only one aur-curator to run per workspace at a time.
- `<repositoryPath>/`: published packages and the repository database.

## Logs and troubleshooting

aur-curator keeps two logs all the time, both in `<rootPath>/.aur-curator/`:

- **`activity.log`**: one line for every import, review decision, merge,
  build, confirm and publish, successful or not. This is your record of
  what was decided and done, similar to `/var/log/pacman.log`. Its
  location can be changed with `activityLogPath`.
- **`errors.log`**: failures only, with details. Look here first when an
  operation fails.

To investigate or report a problem, turn on tracing and reproduce it:

```sh
aur-curator --trace /tmp/aur-curator-trace.log <config-file>
```

The trace records each operation step by step. Lines belonging to one
operation share an ID, so you can follow a single Fetch or Build through
the file. Setting `AUR_CURATOR_TRACE=1` instead writes the trace to
`<rootPath>/.aur-curator/trace.log`.

## Versioning

aur-curator uses [semantic versioning](https://semver.org/). The public
interface is the config file, the CLI and the on-disk state in
`.aur-curator/`:

- **Major**: a config key or CLI command changed incompatibly, or on-disk
  state that needs migrating.
- **Minor**: new features, config keys or commands.
- **Patch**: bug fixes.

`aur-curator --version` prints the version. A build from a commit after a
release looks like `v1.0.0-5-g1a2b3c4`; `-dirty` means it was built from
uncommitted changes.

## For developers

To build from source for development, run the tests, or contribute, start
with [DEVELOPMENT.md](DEVELOPMENT.md).

## License

MIT. See [LICENSE](LICENSE).
