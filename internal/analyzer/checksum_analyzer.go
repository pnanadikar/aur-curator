package analyzer

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// checksumAnalyzer flags removed PGP/checksum verification, checksum-only
// refreshes, and pkgver/pkgrel bumps. Produces findings with categories
// "pgp_removed", "checksum_removed", "checksum_refresh", "version_bump".
// See docs/domain/analyzer-specs.md § Checksum Analyzer.
type checksumAnalyzer struct{}

func (a *checksumAnalyzer) Name() string { return "checksum" }

var (
	hashRe = regexp.MustCompile(`^[0-9a-fA-F]{32,128}$`)
	keyRe  = regexp.MustCompile(`^[0-9A-Fa-f]{8,}$`)
	sumsRe = regexp.MustCompile(`^(md5|sha1|sha256|sha384|sha512|b2)sums$`)
)

func (a *checksumAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	var pkgbuild *git.FileDiff
	for i := range diff.Files {
		if diff.Files[i].Kind == git.FileKindPKGBUILD {
			pkgbuild = &diff.Files[i]
			break
		}
	}
	if pkgbuild == nil {
		return nil, nil
	}

	parsed := parsePKGBUILDDiff(pkgbuild.Patch)
	var findings []domain.Finding

	// pgp_removed: validpgpkeys had a real key removed and nothing
	// key-like remains in Added. No single entry is "the" location for
	// a removal, so this falls back to the array's own Line.
	for _, arr := range parsed.arraysNamed("validpgpkeys") {
		hadKey := false
		for _, e := range arr.Removed {
			if keyRe.MatchString(e) {
				hadKey = true
				break
			}
		}
		stillHasKey := false
		for _, e := range arr.Added {
			if keyRe.MatchString(e.Value) {
				stillHasKey = true
				break
			}
		}
		if hadKey && !stillHasKey {
			findings = append(findings, domain.Finding{
				Analyzer: a.Name(),
				Category: "pgp_removed",
				Summary:  "PGP key verification removed (validpgpkeys)",
				Location: location(pkgbuild.Path, arr.Line),
			})
		}
	}

	// checksum_removed / checksum_refresh: one Finding per touched
	// *sums array, checksum_removed taking priority over
	// checksum_refresh for the same array.
	for _, arr := range parsed.Arrays {
		if !sumsRe.MatchString(arr.Name) {
			continue
		}

		removedLine := 0
		removedFinding := false
		for i, newEntry := range arr.Added {
			if hashRe.MatchString(newEntry.Value) {
				continue // still a real-looking hash — not a removal
			}
			if i < len(arr.Removed) && hashRe.MatchString(arr.Removed[i]) {
				removedFinding = true
				removedLine = newEntry.Line // the entry's own line, whether it's "SKIP", empty, or garbage
				break
			}
		}
		if removedFinding {
			findings = append(findings, domain.Finding{
				Analyzer: a.Name(),
				Category: "checksum_removed",
				Summary:  fmt.Sprintf("Checksum verification removed (%s)", arr.arrayLabel()),
				Location: location(pkgbuild.Path, removedLine),
			})
			continue
		}

		if changedLine, ok := valuesChanged(arr.Added, arr.Removed); ok {
			loc := changedLine
			if loc == 0 {
				loc = arr.Line // fallback: e.g. entry count itself changed
			}
			findings = append(findings, domain.Finding{
				Analyzer: a.Name(),
				Category: "checksum_refresh",
				Summary:  fmt.Sprintf("Checksum values updated (%s)", arr.arrayLabel()),
				Location: location(pkgbuild.Path, loc),
			})
		}
	}

	// version_bump: one Finding total, even if pkgver and pkgrel both
	// changed in the same review. Location prefers pkgver's line since
	// that's the more consequential of the two when both change together.
	pkgver, verOK := parsed.scalar("pkgver")
	pkgrel, relOK := parsed.scalar("pkgrel")
	var parts []string
	line := 0
	if verOK && pkgver.Old != "" && pkgver.New != "" && pkgver.Old != pkgver.New {
		parts = append(parts, fmt.Sprintf("pkgver: %s → %s", pkgver.Old, pkgver.New))
		line = pkgver.NewLine
	}
	if relOK && pkgrel.Old != "" && pkgrel.New != "" && pkgrel.Old != pkgrel.New {
		parts = append(parts, fmt.Sprintf("pkgrel: %s → %s", pkgrel.Old, pkgrel.New))
		if line == 0 {
			line = pkgrel.NewLine
		}
	}
	if len(parts) > 0 {
		summary := parts[0]
		for _, p := range parts[1:] {
			summary += ", " + p
		}
		findings = append(findings, domain.Finding{
			Analyzer: a.Name(),
			Category: "version_bump",
			Summary:  summary,
			Location: location(pkgbuild.Path, line),
		})
	}

	return findings, nil
}

// valuesChanged reports whether added/removed differ position-by-
// position (different lengths always count as changed), and returns the
// new-file line of the first differing Added entry — 0 if the change is
// a length mismatch rather than a value substitution at a common index.
func valuesChanged(added []pkgbuildEntry, removed []string) (line int, changed bool) {
	if len(added) != len(removed) {
		return 0, true
	}
	for i := range added {
		if added[i].Value != removed[i] {
			return added[i].Line, true
		}
	}
	return 0, false
}

func (arr pkgbuildArray) arrayLabel() string {
	if arr.Arch == "" {
		return arr.Name
	}
	return arr.Name + "_" + arr.Arch
}
