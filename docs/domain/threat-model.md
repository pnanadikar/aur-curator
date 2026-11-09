# Threat Model

> Last updated: 2026-11-09
> Status: Active (#470)
> Companion docs: risk-analysis.md, analyzer-specs.md, workflow.md

aur-curator exists to defend against malicious AUR packages. This
document says which attacks it defends against today, which ones are
planned, and which risks are accepted. It is the input that
orders the security work that follows v1 (§ Plan): small, separately
shippable steps, containment first.

Risk is highlighted, never blocking (`risk-analysis.md`). Every defense
below either blocks something mechanically (a checksum mismatch, a
fast-forward that cannot happen) or puts evidence in front of the person
who decides. None of them makes the trust decision.

## Scope

**Who attacks.** Someone who controls what an AUR package contains: its
maintainer, someone who adopted an orphaned package or took over the
maintainer's account, or someone who controls a source the PKGBUILD
downloads (the upstream project, its release host, a mirror).

**What is trusted.** The user; the machine aur-curator runs on;
aur-curator itself and its Go dependencies; the Arch and EndeavourOS
repositories and their signing keys; the Gitea instance. An attacker who
controls any of these is out of scope. The network between aur-curator
and Gitea is not trusted: the Gitea token travels only over `https://`,
or over `http://` to the same machine (#505). Gitea is not on the build path:
Fetch reads from the AUR, Merge pushes to Gitea, and Build clones the
local repository.

**Setting.** One user, one machine, one local pacman repository. Several
machines sharing the repository changes some answers (§ Install-time,
repository signing).

**The sandbox is a mitigation.** The host kernel is trusted: an attack
that escapes the build sandbox or exploits the kernel is out of scope.
The sandbox takes away what a hostile build can reach; it does not make
a hostile build harmless.

**Build dependencies** are installed on the host by the user, as today;
nothing is installed by aur-curator (#351).

**Confirm's purpose** is to show what the package will do when it is
installed. Stronger provenance (reproducible builds, build attestations)
is not a goal.

**Status values.**

| Status | Meaning |
| --- | --- |
| v1 | aur-curator defends against this today |
| partial | a v1 defense covers some cases; the gap is named |
| planned | in § Plan; `P1`–`P7` are its steps |
| accepted | not defended, and deliberately so; the reason is given |

## Three Models

A package can attack at three points, and each needs its own defense:

1. **Supply-chain:** what the package is made of: the PKGBUILD, the
   sources it downloads, who maintains it.
2. **Build-execution:** what happens while it is built. A PKGBUILD is a
   shell script, and `makepkg` runs it. A reviewed, honest-looking
   PKGBUILD can still run hostile code at build time, and so can the
   upstream build system it calls (`make`, `cargo`, `npm`).
3. **Install-time:** what happens when pacman installs the result:
   `.install` scriptlets and the files the package places, both as root.

The review gate defends against the first only. A v1 build runs with
everything the user has, so the second is open today.

### `-bin` packages

A `-bin` package repackages a binary built by someone else, usually the
vendor. Its build is trivial, so a build sandbox protects almost nothing:
the payload is the binary, and it runs when the user runs it. **For a
`-bin` package only the supply-chain defenses apply.** What the review can
establish is that the binary comes from the vendor's own domain and is
pinned by a checksum; whether the vendor's binary is trustworthy is
outside aur-curator's reach.

## Supply-Chain

| # | Threat | Defense | Status |
| --- | --- | --- | --- |
| S1 | A malicious change in an update to the PKGBUILD or `.install` | Review gate: every update is reviewed as a full diff before it can be merged. Analyzers flag new shell execution and network access (`shell_exec`, `network_download`) | partial — § Analyzer gaps; P3 (#475) and P4 (#476) done |
| S2 | A source moves to an attacker's host | `url_new` (High) for every new URL in `source=()`; the reviewer checks the host | v1; automating the domain check is later (§ Later) |
| S3 | The content at an unchanged source URL is replaced | `makepkg` verifies checksums and PGP signatures at build time; `checksum_removed` and `pgp_removed` (High) flag a weakened check | v1 |
| S4 | A VCS package (`-git` and similar) builds whatever upstream's branch holds at build time | Review covers the PKGBUILD only; the source is unpinned (`SKIP`) and unreviewed by construction | accepted — choosing a VCS package trusts upstream's branch |
| S5 | The package changes hands: orphan adoption, maintainer account takeover | Manual: the reviewer checks committer continuity. Git's author and committer fields are set by whoever commits, so they are weak evidence; the AUR's own maintainer record is authoritative | partial; P4 flags an added or replaced PGP key (`pgp_key_changed`, #476); a change of AUR maintainer or co-maintainers since the last recorded review is `maintainer_changed`, Medium (#495). An account takeover keeps the name and is not detected |
| S6 | A package is malicious from its first commit (new package, typosquat) | Import reviews the whole package as added lines, so every URL and shell pattern is flagged; the Findings tab shows the package's first-submitted date, votes and popularity (#495), so a days-old package with no votes reads slowly | partial |
| S7 | A dependency is installed without review | `makepkg` runs without `-s`, so a build with missing dependencies fails and the user installs them. An AUR dependency installed through an AUR helper is unreviewed | partial; P4 flags added dependencies (`depends_added`, #476); a check that AUR dependencies are curated is later (§ Later) |
| S8 | Something other than the reviewed commit gets merged | Merge fast-forwards to `Review.UpstreamCommit`, never to the moving upstream ref; commits on local `master` that are not upstream block Merge and are shown before Build (`workflow.md` § Merge, § Local Commits Not on Upstream). A build clones the merged commit and records it; only a build of `master`'s HEAD counts (`domain.md` § Build) | v1 |
| S9 | An AUR helper (yay) installs from the AUR without review | None inside aur-curator | accepted while packages are being migrated; removing the helper afterwards closes it |
| S10 | The reviewer approves a hostile change anyway: a large diff, fatigue, a plausible-looking command | Findings are ranked by severity so the riskiest lines come first; the decision stays human | accepted — inherent in "never automate trust" |
| S11 | The review screen shows something other than the file. Text the maintainer writes (diff lines, finding summaries, commit authors and subjects, the version) can hold terminal control sequences (`ESC[1A ESC[2K` erases the line above), a carriage return that prints harmless text over a command, colour that hides text, or bidi and zero-width characters that reorder or hide it (Trojan Source, CVE-2021-42574). This changes nothing makepkg runs; it defeats the review | Every control, bidi and zero-width character is shown as a visible escape in the error style (`\x1b`, `\r`, `\u202e`), never interpreted and never silently removed (`tui-review.md` § Escaped Characters). A version holding one is not readable, so it is never reported as up to date (#491) | v1.3 |
| S12 | aur-curator itself runs with a known vulnerability: in its own Go code, a module it imports, or the Go standard library it is built with. It talks to the AUR and Gitea over HTTPS (TLS, certificate checks, HTTP/2) and parses what they return | `make lint` runs gosec on every change; every exclusion names its reason. `make vuln` (govulncheck, pinned) reports known vulnerabilities in code aur-curator calls, and `make release` runs it before tagging, so a release stops on one like on a failing test. The minimum Go is set in `go.mod` to a release with the known fixes (`DEVELOPMENT.md` § Releases, #493) | v1.3 |

### Current review practice

During daily use (from 31 Oct 2026) a review covers, for every update:

- **Committer continuity:** the same committer across the recent
  commits (S5; weak evidence, see above).
- **Source host:** each download comes from the project's or vendor's
  own domain (S2; for `-bin` packages the whole defense).
- **The full diff and the findings**, not the findings alone (S1).

Automating the first two, with the AUR's maintainer record instead of
Git's committer field, is later work (§ Later).

### Analyzer gaps

The analyzers read text; nothing is executed. The Shell analyzer
matches patterns in added lines; the array checks compare the old and
new PKGBUILD as written (`analyzer-specs.md` § PKGBUILD Parsing, § Known
Limitations). Gaps that matter here:

- **Function coverage** (closed by P2, #474). The Shell analyzer
  scanned `build()`, `package()` and `prepare()` only, and only when the
  function's header was in the hunk; code in `pkgver()`, `check()`,
  `package_foo()`, at top level or deep in a long function had no
  finding pointing at it. It now scans every added PKGBUILD line, and
  the Findings tab says that the diff is the review.
- **Added lines only.** Pre-existing code is not flagged again. That is
  intended for updates; on Import every line counts as added.
- **Sourced files.** A file sourced from the PKGBUILD shows up in the
  diff as another file, but its contents are not scanned.
- **Text, not execution.** Arrays are compared as written: variables
  and brace expansion are not expanded, so a change made through a
  variable (`url=` used in `source`) gets no finding of its own. An
  array the parser cannot read is a High finding (`array_unreadable`),
  never silence (#484).
- **Checksum algorithm downgrade.** Deleting one checksum array while a
  weaker one remains (`sha256sums` deleted, `md5sums` kept) is not a
  finding; makepkg still verifies, with the weaker algorithm (#485).

None of these weakens the gate itself, since the full diff is always
shown. They weaken the pointers into it. P2 removed the first; the
second is by design; the others stay.
Analyzing what a change to existing
code means (rather than what it adds) would need a shell parser and is
not pursued (§ Not Pursued).

## Build-Execution

A v1 build runs `makepkg --noconfirm --nocolor` as the user, with the
user's environment minus a denylist (P1, #473), home directory and
network (`internal/build/makepkg.go`). It runs in a fresh clone of the
package's local repository at the merged commit, under
`<rootPath>/.aur-curator/builds/<id>/work/src`
(`internal/build/engine.go`). Anything the build code does, it does as
the user.

| # | Threat | What a v1 build can reach | Status |
| --- | --- | --- | --- |
| B1 | Credential theft | `~/.ssh`, browser profiles, password-store files, the aur-curator config (`giteaToken`), and the inherited environment, including `AUR_CURATOR_GITEA_TOKEN` when set (the config example recommends it). Also the SSH and GPG agent sockets | partial: P1 removes secrets held in the environment (`AUR_CURATOR_*`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`) and the SSH agent's address; files in `$HOME` (the config's `giteaToken` among them) and the agent sockets at their default paths stay reachable. Planned: P5 (`$HOME` hidden, no session sockets) |
| B2 | Persistence on the host | shell startup files, `systemd --user` units, autostart entries, `crontab` | planned: P5 |
| B3 | Poisoning other curated packages | the whole `rootPath`, including other packages' clones and the `.aur-curator/` stores (review and build records), and `repositoryPath`, the repository pacman installs from. A build that writes a package into `repositoryPath` and updates its database gets it installed as root on the next `pacman -Syu`. Commits added to another package's clone are caught (S8); changes to the stores and to the repository are not | planned: P5 (only the build directory writable) |
| B4 | Exfiltration | the network, for the whole build | planned: P6 (network for fetching sources only) |
| B5 | Other processes of the user | signals and `ptrace` on same-user processes; the session D-Bus (including a secret service); the display server | partial: P1 removes the D-Bus and display addresses; the sockets stay reachable at their default paths. Planned: P5 (separate namespaces, no session sockets) |
| B6 | Privilege escalation | `sudo -n` succeeds if a sudo timestamp is still valid for the terminal aur-curator runs in; setuid binaries on the host | planned: P5 (`no_new_privs`, `nosuid`). Until then: do not run `sudo` in that terminal before building |
| B7 | Resource exhaustion | CPU, memory, disk | partial: a build can be cancelled (X), which ends its whole process group; no limits (§ Later) |
| B8 | Artifacts that differ from what the source says | The build decides what goes into the package | partial: Confirm is a human gate with no verification; planned: P7 |

B3 is the most serious: it turns one hostile build into a root
installation of something nobody reviewed, and it bypasses the review
gate for every other package. B1 is next: the Gitea token gives write
access to every curated repository.

### The network

A build needs the network to download its sources; most do not need it
after that. The exceptions fetch language dependencies in `prepare()`
(cargo, go, npm). P6 splits the build in two:

1. **Fetch,** with network: `makepkg --nobuild --noprepare` downloads
   and verifies the sources.
2. **Build,** without network: `prepare()`, `build()` and `package()`.

A package that needs the network in `prepare()` gets it only through a
per-package setting that is off by default and shown in Review. How many
curated packages need it is measured before P6 is designed, by building
each one with `--unshare-net` on a test workspace.

Network access in the build includes the local network, Gitea among it.
After P1 the build has no Gitea token in its environment, but a service
on the local network that trusts local clients is reachable from a
networked `prepare()`.

## Install-Time

| # | Threat | Defense | Status |
| --- | --- | --- | --- |
| I1 | `.install` scriptlets run as root | The Shell analyzer scans every added line of `.install`, not only function bodies; review | v1; P4 flags a `.install` added or changed (`install_changed`, #476) |
| I2 | Files placed with special power: setuid binaries, pacman hooks, systemd units, files under `/etc`, `sudoers.d`, sysusers, udev rules | Confirm, a human gate with no verification (`domain.md` § Package) | planned: P7 (list them at Confirm; informational, never blocking) |
| I3 | The installed program itself, run by the user | Out of aur-curator's reach: trust in the upstream project or vendor, established at review (S2, S5) | accepted |
| I4 | Something other than a published build is in the local repository | Only Publish writes `repositoryPath`, by design; nothing stops another writer (B3). The repository is not signed, and its `pacman.conf` entry uses `SigLevel = Optional TrustAll`, so pacman installs an unsigned package from it without a prompt | planned: P5 closes the build route; repository signing matters once more than one machine uses the repository (§ Later) |

## Plan

Small steps, each one issue and shippable on its own. Containment comes
first; analyzers are a second layer. None of these changes the on-disk
format, so each can ship as a v1.x minor release.

| Step | What | Size | Closes |
| --- | --- | --- | --- |
| P1 | **Clean build environment.** `makepkg` gets the user's environment minus `AUR_CURATOR_*`, credential variables (`*_TOKEN`, `*_SECRET`, `*_PASSWORD`), askpass helpers, and the SSH agent, D-Bus session and display addresses (a denylist; P5 replaces it with an allowlist). Removing an address does not hide its socket; the GPG agent has no address variable at all (#473) | small | B1 and B5, the environment part |
| P2 | **Analyzer scope.** The Shell analyzer scans every added PKGBUILD line, not three function bodies: top-level code runs when `makepkg` sources the file, and `pkgver()`, `check()` and `package_*()` run at build time. The Findings tab says that analyzers scan added lines and that the diff is the review (#474) | small | § Analyzer gaps, the first |
| P3 | **No silent Low.** A test that every category an analyzer emits is in the severity table; an unmapped category resolves to High, is written to the error log and is reported to the user as an internal inconsistency (#475) | small | `risk-analysis.md` § Unmapped categories |
| P4 | **Signals from the existing PKGBUILD parser,** each Medium: `.install` added or changed; a key added to `validpgpkeys`, with or without one removed; an entry added to `depends`, `makedepends`, `checkdepends` or `optdepends`. Medium becomes reachable (#23) (#476) | small–medium | S1, S5, S7, I1, in part |
| P5 | **Sandboxed build** (bwrap): `/` read-only; only the build directory writable; `$HOME` on a tmpfs with the GPG public keyring read-only; no session sockets (`/run/user/<uid>`: the SSH and GPG agents, the session bus); an allowlisted environment; separate namespaces; `no_new_privs`. If the sandbox cannot be set up, the build fails; there is no unsandboxed fallback. Adversarial fixture builds that try to read a dummy secret, write `rootPath`, write `repositoryPath`, read the token and run `sudo -n`, each expected to fail | medium–high | B1, B2, B3, B5, B6 |
| P6 | **Network for fetching only** (§ The network) | medium | B4 |
| P7 | **Confirm inspector.** Read the built archive without extracting it; list setuid and setgid files, `.INSTALL`, pacman hooks, systemd units, files under `/etc`, `sudoers.d`, sysusers and udev rules, and the archive's sha256. Informational, never blocking. Changes the domain rule "Confirm performs no verification": an ADR first | medium | I2, B8 |

P1–P4 are independent of each other. P5 needs an ADR ("sandboxed builds
via bwrap") and a plan doc; P6 builds on P5.

**Inputs for the P5 and P6 plans.** From RUA, read only: its bwrap
argument list; how it exposes the GPG keyring for `validpgpkeys`; how it
separates fetching from an offline build. From the test workspace: which
curated packages fail with no network in `prepare()`.

## Later

Not in the plan above, and not rejected: each waits for a reason.

- **Resource limits** (B7): cancel already ends the whole process
  group; limits come after P5, if a build ever needs them.
- **Dependency check** (S7): that a package's AUR dependencies are
  curated and installed, stopping with a message naming the missing ones
  (aur-curator never installs them).
- **Provenance signals** (S2): a `-bin` source off the vendor's domain.
  The AUR's maintainer record, package age and popularity are done
  (#495, S5, S6).
- **Repository signing** (I4): once more than one machine uses the
  repository. The signing key must stay out of the build's reach.

## Not Pursued

- **Semantic analysis of changed code:** detecting what a change to an
  existing variable or condition activates would need a shell parser.
  The full diff is always reviewed instead.
- **A shell parser or shellcheck** in place of the line heuristics.
- **Network policy modes** (strict, dependency, compatibility): one
  policy with a per-package exception is enough (§ The network).
- **Reproducible builds and build attestations** (§ Scope, Confirm's
  purpose).
- **Defending against sandbox or kernel escapes** (§ Scope).
