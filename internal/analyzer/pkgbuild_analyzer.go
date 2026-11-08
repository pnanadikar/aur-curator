package analyzer

import (
	"context"
	"fmt"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// pkgbuildAnalyzer reports the parser's coverage: one "array_unreadable"
// finding per array assignment of the new PKGBUILD that parsePKGBUILDFile
// could not read, so a part no check covered never reads as a part with
// nothing to report. See docs/domain/analyzer-specs.md § Pkgbuild
// Analyzer (#484).
type pkgbuildAnalyzer struct{}

func (a *pkgbuildAnalyzer) Name() string { return "pkgbuild" }

func (a *pkgbuildAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	pkgbuild, err := loadedPKGBUILD(diff)
	if err != nil || pkgbuild == nil {
		return nil, err
	}

	var findings []domain.Finding
	for _, u := range parsePKGBUILDFile(pkgbuild.New).Unreadable {
		lines := fmt.Sprintf("lines %d–%d", u.First, u.Last)
		if u.First == u.Last {
			lines = fmt.Sprintf("line %d", u.First)
		}
		findings = append(findings, domain.Finding{
			Analyzer: a.Name(),
			Category: CategoryArrayUnreadable,
			Summary:  fmt.Sprintf("%s: could not read %s; findings for it may be missing. Check the diff.", u.label(), lines),
			Location: location(pkgbuild.Path, u.First),
		})
	}
	return findings, nil
}

// loadedPKGBUILD returns the diff's PKGBUILD entry, or nil if the range
// does not change it. Its whole old and new text must have been loaded
// (ReviewDiff does): without them an analyzer would compare nothing and
// report nothing, so a missing load is an error, never an empty result
// (analyzer-specs.md § Input shape, #484).
func loadedPKGBUILD(diff *git.GitDiff) (*git.FileDiff, error) {
	for i := range diff.Files {
		f := &diff.Files[i]
		if f.Kind != git.FileKindPKGBUILD {
			continue
		}
		if !f.ContentLoaded {
			return nil, fmt.Errorf("%s: whole old and new text not loaded", f.Path)
		}
		return f, nil
	}
	return nil, nil
}
