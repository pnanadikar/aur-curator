package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// urlAnalyzer flags download URLs in the new PKGBUILD's source arrays
// that no source array of the old PKGBUILD has, any arch. Both files are
// read whole (parsePKGBUILDFile), so an entry is seen wherever it is and
// however it is quoted (#484). Produces findings with category
// "url_new". See docs/domain/analyzer-specs.md § URL Analyzer.
type urlAnalyzer struct{}

func (a *urlAnalyzer) Name() string { return "url" }

var urlTokenRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"')]+`)

func (a *urlAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	pkgbuild, err := loadedPKGBUILD(diff)
	if err != nil || pkgbuild == nil {
		return nil, err
	}

	known := map[string]bool{}
	for _, arr := range parsePKGBUILDFile(pkgbuild.Old).arraysNamed("source") {
		for _, e := range arr.values() {
			for _, url := range urlTokenRe.FindAllString(e.Value, -1) {
				known[url] = true
			}
		}
	}

	// A new URL is reported once, at the first line that has it: moving
	// a URL between arch arrays or repeating it is not a second source.
	firstLine := map[string]int{}
	var urls []string
	for _, arr := range parsePKGBUILDFile(pkgbuild.New).arraysNamed("source") {
		for _, e := range arr.values() {
			for _, url := range urlTokenRe.FindAllString(e.Value, -1) {
				if known[url] {
					continue
				}
				line, seen := firstLine[url]
				if !seen {
					urls = append(urls, url)
				}
				if !seen || e.Line < line {
					firstLine[url] = e.Line
				}
			}
		}
	}
	sort.SliceStable(urls, func(i, j int) bool { return firstLine[urls[i]] < firstLine[urls[j]] })

	findings := make([]domain.Finding, 0, len(urls))
	for _, url := range urls {
		findings = append(findings, domain.Finding{
			Analyzer: a.Name(),
			Category: CategoryURLNew,
			Summary:  fmt.Sprintf("New source URL: %s", url),
			Location: location(pkgbuild.Path, firstLine[url]),
		})
	}
	return findings, nil
}
