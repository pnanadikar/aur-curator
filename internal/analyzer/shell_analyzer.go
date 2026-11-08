package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// shellAnalyzer flags new shell invocations and network access on every
// Added line of the PKGBUILD and .install: top-level PKGBUILD code runs
// when makepkg sources the file, and every function runs at build time
// (#474). Produces findings with categories "shell_exec",
// "network_download" (a single line may produce both), and
// "install_changed" for a .install file added or changed (#476). See
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
		category: CategoryNetworkDownload,
		label:    "Network download",
		re:       regexp.MustCompile(`\bcurl\b`),
	},
	{
		category: CategoryNetworkDownload,
		label:    "Network download",
		re:       regexp.MustCompile(`\bwget\b`),
	},
	{
		category: CategoryNetworkDownload,
		label:    "Network download",
		re:       regexp.MustCompile(`\bgit\s+clone\b`),
	},
	{
		category: CategoryNetworkDownload,
		label:    "Network download",
		re:       regexp.MustCompile(`\bnc\s+-`),
	},
	{
		category: CategoryNetworkDownload,
		label:    "Network download",
		re:       regexp.MustCompile(`/dev/tcp/`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\beval\b`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\bbash\s+-c\b`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\bsh\s+-c\b`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`^\s*source\s+\S+`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`^\s*\.\s+\S+`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile("`[^`]+`"),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\$\([^)]*\)`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`\|\s*(bash|sh)\b`),
	},
	{
		category: CategoryShellExec,
		label:    "Dynamic shell execution",
		re:       regexp.MustCompile(`base64\s+-d.*\|\s*(bash|sh)`),
	},
}

func (a *shellAnalyzer) Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error) {
	var findings []domain.Finding

	// Every PKGBUILD and .install file in the diff: a split package can
	// have several .install files (before #476 only the last was scanned).
	for i := range diff.Files {
		f := &diff.Files[i]
		if f.Kind != git.FileKindPKGBUILD && f.Kind != git.FileKindInstall {
			continue
		}
		firstAdded := 0
		for _, l := range splitPatchLines(f.Patch) {
			if l.kind != lineAdded {
				continue
			}
			if firstAdded == 0 {
				firstAdded = l.newLineNo
			}
			findings = append(findings, scanLine(a.Name(), l.text, f.Path, l.newLineNo)...)
		}
		if f.Kind == git.FileKindInstall {
			if finding, ok := installChanged(a.Name(), f, firstAdded); ok {
				findings = append(findings, finding)
			}
		}
	}

	return findings, nil
}

// scanLine checks one added line against both pattern sets.
func scanLine(analyzerName, line, path string, lineNo int) []domain.Finding {
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
			Summary:  fmt.Sprintf("%s: %s", rule.label, line),
			Location: location(path, lineNo),
		})

		seen[rule.category] = true
	}

	return findings
}

// installChanged reports a .install file that was added or changed; a
// deleted one runs nothing and is not reported (#476). Added or deleted
// is read from git's header lines, before the first hunk.
func installChanged(analyzerName string, f *git.FileDiff, firstAdded int) (domain.Finding, bool) {
	added, deleted := false, false
	for _, line := range strings.Split(f.Patch, "\n") {
		if strings.HasPrefix(line, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(line, "new file mode "), line == "--- /dev/null":
			added = true
		case strings.HasPrefix(line, "deleted file mode "), line == "+++ /dev/null":
			deleted = true
		}
	}
	if deleted {
		return domain.Finding{}, false
	}
	verb := "changed"
	if added {
		verb = "added"
	}
	return domain.Finding{
		Analyzer: analyzerName,
		Category: CategoryInstallChanged,
		Summary:  fmt.Sprintf(".install %s: %s", verb, f.Path),
		Location: location(f.Path, firstAdded),
	}, true
}
