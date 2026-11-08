package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// checksumAnalyzer flags removed PGP/checksum verification, new PGP keys,
// checksum-only refreshes, and pkgver/pkgrel bumps. Produces findings with
// categories "pgp_removed", "pgp_key_changed", "checksum_removed",
// "checksum_refresh", "version_bump". All compare the whole old and new
// PKGBUILD (#484, #485).
// See docs/domain/analyzer-specs.md § Checksum Analyzer.
type checksumAnalyzer struct{}

func (a *checksumAnalyzer) Name() string { return "checksum" }

var (
	hashRe = regexp.MustCompile(`^[0-9a-fA-F]{32,128}$`)
	keyRe  = regexp.MustCompile(`^[0-9A-Fa-f]{8,}$`)
	sumsRe = regexp.MustCompile(`^(md5|sha1|sha224|sha256|sha384|sha512|b2|ck)sums$`)
)

func (a *checksumAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	pkgbuild, err := loadedPKGBUILD(diff)
	if err != nil || pkgbuild == nil {
		return nil, err
	}

	oldFile := parsePKGBUILDFile(pkgbuild.Old)
	newFile := parsePKGBUILDFile(pkgbuild.New)
	findings := pgpFindings(a.Name(), pkgbuild.Path, oldFile, newFile)
	findings = append(findings, checksumFindings(a.Name(), pkgbuild.Path, oldFile, newFile)...)
	if f, ok := versionBump(a.Name(), pkgbuild.Path, oldFile, newFile); ok {
		findings = append(findings, f)
	}
	return findings, nil
}

// versionBump reports pkgver and pkgrel changes as one finding, even if
// both changed: each compared when both files assign it a value, at
// their last assignment (§ version_bump, #485). Location prefers
// pkgver's line, the more consequential of the two.
func versionBump(analyzerName, path string, oldFile, newFile pkgbuildFile) (domain.Finding, bool) {
	var parts []string
	line := 0
	for _, name := range []string{"pkgver", "pkgrel"} {
		before, inOld := oldFile.Scalars[name]
		after, inNew := newFile.Scalars[name]
		if !inOld || !inNew || before.Value == "" || after.Value == "" || before.Value == after.Value {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s → %s", name, before.Value, after.Value))
		if line == 0 {
			line = after.Line
		}
	}
	if len(parts) == 0 {
		return domain.Finding{}, false
	}
	return domain.Finding{
		Analyzer: analyzerName,
		Category: CategoryVersionBump,
		Summary:  strings.Join(parts, ", "),
		Location: location(path, line),
	}, true
}

// checksumFindings compares each checksum label (name and arch) that the
// old and the new PKGBUILD both have, position by position, each position
// holding its entry and any indexed values (fileArray.at): checksum_removed
// when the new array has, where the old one had a hash, a value that is
// not a hash and that the old one did not have there; else
// checksum_refresh when any position's values differ (#485). A label only
// in the new file (Import, a new arch) or only in the old one (deleted)
// gives nothing: see analyzer-specs.md § checksum_removed rule 5.
func checksumFindings(analyzerName, path string, oldFile, newFile pkgbuildFile) []domain.Finding {
	var findings []domain.Finding
	for _, after := range newFile.Arrays {
		if !sumsRe.MatchString(after.Name) {
			continue
		}
		before, inOld := oldFile.array(after.Name, after.Arch)
		if !inOld {
			continue
		}

		removedAt := 0
		for i := 0; i < after.size() && removedAt == 0; i++ {
			removedAt = weakenedAt(before.at(i), after.at(i))
		}
		// An unreadable assignment of the label may still hold the hash:
		// array_unreadable reports it, not a removal.
		if removedAt > 0 && !unreadable(newFile, after.Name, after.Arch) {
			findings = append(findings, domain.Finding{
				Analyzer: analyzerName,
				Category: CategoryChecksumRemoved,
				Summary:  fmt.Sprintf("Checksum verification removed (%s)", after.label()),
				Location: location(path, removedAt),
			})
			continue
		}

		if line, changed := positionsChanged(before, after); changed {
			if line == 0 {
				line = after.Line // no new value to point at: an entry went away
			}
			findings = append(findings, domain.Finding{
				Analyzer: analyzerName,
				Category: CategoryChecksumRefresh,
				Summary:  fmt.Sprintf("Checksum values updated (%s)", after.label()),
				Location: location(path, line),
			})
		}
	}
	return findings
}

// weakenedAt returns the line of a value of after that is not a hash and
// that before does not have, when before has a hash; 0 otherwise. So an
// override already there (spotify's sha512sums[4]='SKIP' in both files)
// is not a removal, and a new one is.
func weakenedAt(before, after []fileEntry) int {
	hadHash := false
	had := map[string]bool{}
	for _, e := range before {
		had[e.Value] = true
		hadHash = hadHash || hashRe.MatchString(e.Value)
	}
	if !hadHash {
		return 0
	}
	for _, e := range after {
		if !hashRe.MatchString(e.Value) && !had[e.Value] {
			return e.Line
		}
	}
	return 0
}

// positionsChanged reports whether before and after differ at any
// position (the values there, in order) or in their number of positions,
// and the line of the first differing value of after; 0 when there is
// none to point at (only the count changed, or a value went away).
func positionsChanged(before, after fileArray) (line int, changed bool) {
	for i := 0; i < after.size(); i++ {
		b, a := before.at(i), after.at(i)
		for j, e := range a {
			if j >= len(b) || b[j].Value != e.Value {
				return e.Line, true
			}
		}
		if len(b) != len(a) {
			changed = true
		}
	}
	return 0, changed || before.size() != after.size()
}

// pgpFindings compares each validpgpkeys label (name and arch) of the
// whole old and new PKGBUILD: pgp_removed when the old array had a key
// and the new one has none, else pgp_key_changed for keys the new one
// gained (#476, #484). Labels are taken in new-file order, then labels
// only the old file has.
func pgpFindings(analyzerName, path string, oldFile, newFile pkgbuildFile) []domain.Finding {
	arches := []string{}
	seen := map[string]bool{}
	for _, f := range []pkgbuildFile{newFile, oldFile} {
		for _, arr := range f.arraysNamed("validpgpkeys") {
			if !seen[arr.Arch] {
				seen[arr.Arch] = true
				arches = append(arches, arr.Arch)
			}
		}
	}

	var findings []domain.Finding
	for _, arch := range arches {
		before, _ := oldFile.array("validpgpkeys", arch)
		after, inNew := newFile.array("validpgpkeys", arch)
		if hasKey(before) && !hasKey(after) {
			if unreadable(newFile, "validpgpkeys", arch) {
				// Not known to be removed: array_unreadable reports it.
				continue
			}
			// No single entry is "the" location for a removal: the new
			// array's opening line, or none when the array is gone.
			line := 0
			if inNew {
				line = after.Line
			}
			findings = append(findings, domain.Finding{
				Analyzer: analyzerName,
				Category: CategoryPGPRemoved,
				Summary:  "PGP key verification removed (validpgpkeys)",
				Location: location(path, line),
			})
			continue
		}
		if f, ok := pgpKeyChanged(analyzerName, path, before, after); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// hasKey reports whether arr has a key-looking entry.
func hasKey(arr fileArray) bool {
	for _, e := range arr.values() {
		if keyRe.MatchString(e.Value) {
			return true
		}
	}
	return false
}

// unreadable reports whether f has an unreadable assignment to the array
// name with this arch suffix.
func unreadable(f pkgbuildFile, name, arch string) bool {
	for _, u := range f.Unreadable {
		if u.Name == name && u.Arch == arch {
			return true
		}
	}
	return false
}

// pgpKeyChanged reports the keys the array gained: key-looking entries of
// after that before (the same label in the old file) does not have.
// "replaced" when one of before's keys is gone, "added" otherwise (#476).
func pgpKeyChanged(analyzerName, path string, before, after fileArray) (domain.Finding, bool) {
	removed := map[string]bool{}
	for _, e := range before.values() {
		if keyRe.MatchString(e.Value) {
			removed[e.Value] = true
		}
	}
	kept := map[string]bool{}
	var added []string
	line := 0
	for _, e := range after.values() {
		if !keyRe.MatchString(e.Value) {
			continue
		}
		if removed[e.Value] {
			kept[e.Value] = true
			continue
		}
		if slices.Contains(added, e.Value) {
			continue
		}
		added = append(added, e.Value)
		if line == 0 {
			line = e.Line
		}
	}
	if len(added) == 0 {
		return domain.Finding{}, false
	}
	verb := "added"
	if len(kept) < len(removed) {
		verb = "replaced"
	}
	return domain.Finding{
		Analyzer: analyzerName,
		Category: CategoryPGPKeyChanged,
		Summary:  fmt.Sprintf("PGP key %s: %s", verb, strings.Join(added, ", ")),
		Location: location(path, line),
	}, true
}
