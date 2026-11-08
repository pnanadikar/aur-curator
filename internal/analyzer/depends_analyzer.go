package analyzer

import (
	"context"
	"fmt"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// dependsAnalyzer flags dependencies added to depends, makedepends,
// checkdepends or optdepends (any arch suffix). It compares each label
// of the whole old and new PKGBUILD (parsePKGBUILDFile), every
// assignment of it together, so a split package's package_*() arrays and
// an entry anywhere in a long or unquoted array count (#484). Produces
// findings with category "depends_added", one per array name and arch.
// See docs/domain/analyzer-specs.md § Depends Analyzer (#476).
type dependsAnalyzer struct{}

func (a *dependsAnalyzer) Name() string { return "depends" }

// dependsArrays are the arrays whose entries are packages the build or
// the installed package needs.
var dependsArrays = []string{"depends", "makedepends", "checkdepends", "optdepends"}

func (a *dependsAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	pkgbuild, err := loadedPKGBUILD(diff)
	if err != nil || pkgbuild == nil {
		return nil, err
	}

	old := parsePKGBUILDFile(pkgbuild.Old)
	var findings []domain.Finding
	for _, name := range dependsArrays {
		for _, arr := range parsePKGBUILDFile(pkgbuild.New).arraysNamed(name) {
			before, _ := old.array(arr.Name, arr.Arch)
			if f, ok := dependsAdded(a.Name(), pkgbuild.Path, before, arr); ok {
				findings = append(findings, f)
			}
		}
	}
	return findings, nil
}

// dependsAdded reports the entries the array gained: those of after
// whose package name is not the name of any entry of before (the same
// label in the old file; empty if it had none). Entries are named as
// written, except optdepends, named by package only: their descriptions
// are long, and the diff has them.
func dependsAdded(analyzerName, path string, before, after fileArray) (domain.Finding, bool) {
	old := map[string]bool{}
	for _, e := range before.values() {
		old[dependencyName(e.Value)] = true
	}
	seen := map[string]bool{}
	var added []string
	line := 0
	for _, e := range after.values() {
		name := dependencyName(e.Value)
		shown := e.Value
		if after.Name == "optdepends" {
			shown = name
		}
		if name == "" || old[name] || seen[shown] {
			continue
		}
		seen[shown] = true
		added = append(added, shown)
		if line == 0 {
			line = e.Line
		}
	}
	if len(added) == 0 {
		return domain.Finding{}, false
	}
	return domain.Finding{
		Analyzer: analyzerName,
		Category: CategoryDependsAdded,
		Summary:  fmt.Sprintf("Dependencies added (%s): %s", after.label(), strings.Join(added, ", ")),
		Location: location(path, line),
	}, true
}

// dependencyName is an entry's package name: the entry up to the first
// ':' (an optdepends description) or version operator ('<', '>', '=').
// So "foo>=1.2" and "foo: for bar" are both "foo".
func dependencyName(entry string) string {
	if i := strings.IndexAny(entry, ":<>="); i >= 0 {
		entry = entry[:i]
	}
	return strings.TrimSpace(entry)
}
