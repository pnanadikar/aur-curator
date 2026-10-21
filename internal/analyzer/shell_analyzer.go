package analyzer

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// shellAnalyzer flags new shell invocations in PKGBUILD build()/
// package()/prepare() bodies and anywhere in .install, plus network
// access during build. Produces findings with categories "shell_exec",
// "network_download". A single line may produce both. See
// docs/domain/analyzer-specs.md § Shell Analyzer.
type shellAnalyzer struct{}

func (a *shellAnalyzer) Name() string { return "shell" }

type shellRule struct {
	category string
	label    string
	re       *regexp.Regexp
}

var shellRules = []shellRule{
	{
		category: "network_download",
		label:    "Network download",
		re:       regexp.MustCompile(`\bcurl\b`),
	},
	{
		category: "network_download",
		label:    "Network download",
		re:       regexp.MustCompile(`\bwget\b`),
	},
	{
		category: "network_download",
		label:    "Network download",
		re:       regexp.MustCompile(`\bgit\s+clone\b`),
	},
	{
		category: "network_download",
		label:    "Network download",
		re:       regexp.MustCompile(`\bnc\s+-`),
	},
	{
		category: "network_download",
		label:    "Network download",
		re:       regexp.MustCompile(`/dev/tcp/`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\beval\b`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\bbash\s+-c\b`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\bsh\s+-c\b`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`^\s*source\s+\S+`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`^\s*\.\s+\S+`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile("`[^`]+`"),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\$\([^)]*\)`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\|\s*(bash|sh)\b`),
	},
	{
		category: "shell_exec",
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`base64\s+-d.*\|\s*(bash|sh)`),
	},
}

func (a *shellAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	var findings []domain.Finding

	var pkgbuild, install *git.FileDiff
	for i := range diff.Files {
		switch diff.Files[i].Kind {
		case git.FileKindPKGBUILD:
			pkgbuild = &diff.Files[i]
		case git.FileKindInstall:
			install = &diff.Files[i]
		}
	}

	if pkgbuild != nil {
		parsed := parsePKGBUILDDiff(pkgbuild.Patch)
		for _, fn := range parsed.Functions {
			for _, entry := range fn.AddedLines {
				findings = append(findings, scanLine(a.Name(), entry.Value, pkgbuild.Path, entry.Line, fn.Name)...)
			}
		}
	}

	if install != nil {
		for _, l := range splitPatchLines(install.Patch) {
			if l.kind != lineAdded {
				continue
			}
			findings = append(findings, scanLine(a.Name(), l.text, install.Path, l.newLineNo, "")...)
		}
	}

	return findings, nil
}

// scanLine checks one added line against both pattern sets. inFunc, if
// non-empty, is a PKGBUILD function name for the Summary; empty means
// the line came from .install.
func scanLine(analyzerName, line, path string, lineNo int, inFunc string) []domain.Finding {
	var findings []domain.Finding

	seen := map[string]bool{}

	for _, rule := range shellRules {
		if seen[rule.category] {
			continue
		}
		if !rule.re.MatchString(line) {
			continue
		}

		findings = append(findings, domain.Finding{
			Analyzer: analyzerName,
			Category: rule.category,
			Summary:  summaryFor(rule.label, inFunc, line),
			Location: location(path, lineNo),
		})

		seen[rule.category] = true
	}

	return findings
}

func summaryFor(label, inFunc, line string) string {
	if inFunc != "" {
		return fmt.Sprintf("%s in %s(): %s", label, inFunc, line)
	}
	return fmt.Sprintf("%s: %s", label, line)
}
