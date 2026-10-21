package analyzer

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// urlAnalyzer flags new/changed source=() URLs not present in the prior
// reviewed commit. Produces findings with category "url_new". See
// docs/domain/analyzer-specs.md § URL Analyzer.
type urlAnalyzer struct{}

func (a *urlAnalyzer) Name() string { return "url" }

var urlTokenRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"')]+`)

func (a *urlAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
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

	removed := map[string]bool{}
	for _, arr := range parsed.arraysNamed("source") {
		for _, entry := range arr.Removed {
			for _, url := range urlTokenRe.FindAllString(entry, -1) {
				removed[url] = true
			}
		}
	}

	var findings []domain.Finding
	seen := map[string]bool{}
	for _, arr := range parsed.arraysNamed("source") {
		for _, entry := range arr.Added {
			for _, url := range urlTokenRe.FindAllString(entry.Value, -1) {
				if removed[url] || seen[url] {
					continue // moved/reformatted, not genuinely new
				}
				seen[url] = true
				findings = append(findings, domain.Finding{
					Analyzer: a.Name(),
					Category: "url_new",
					Summary:  fmt.Sprintf("New source URL: %s", url),
					Location: location(pkgbuild.Path, entry.Line),
				})
			}
		}
	}

	return findings, nil
}
