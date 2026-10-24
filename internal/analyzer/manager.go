package analyzer

import (
	"context"
	"errors"
	"fmt"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// Analyzer examines a GitDiff and produces Findings.
type Analyzer interface {
	Name() string
	Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error)
}

// Manager coordinates execution of all analyzers.
type Manager interface {
	// Run runs every analyzer, even after one fails, and returns the
	// findings of those that succeeded. If any analyzer failed, the
	// error names each one and the findings are incomplete: the caller
	// must not record them as a complete run (#410).
	Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error)
}

type manager struct {
	analyzers []Analyzer
}

// New returns the standard analyzer manager.
func New() Manager {
	return &manager{
		analyzers: []Analyzer{
			&urlAnalyzer{},
			&checksumAnalyzer{},
			&shellAnalyzer{},
		},
	}
}

func newManager(analyzers ...Analyzer) *manager {
	return &manager{
		analyzers: analyzers,
	}
}

func (m *manager) Run(
	ctx context.Context,
	diff *git.GitDiff,
) ([]domain.Finding, error) {

	var findings []domain.Finding
	var errs []error

	for _, analyzer := range m.analyzers {
		f, err := analyzer.Run(ctx, diff)
		if err != nil {
			// One failing analyzer does not stop the others, so a
			// single error report covers every analyzer that failed.
			errs = append(errs, fmt.Errorf("analyzer %q failed: %w", analyzer.Name(), err))
			continue
		}

		findings = append(findings, f...)
	}

	return findings, errors.Join(errs...)
}
