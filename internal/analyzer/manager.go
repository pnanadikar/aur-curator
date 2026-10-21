package analyzer

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

type Logger interface {
	Printf(format string, args ...any)
}

// Analyzer examines a GitDiff and produces Findings.
type Analyzer interface {
	Name() string
	Run(ctx context.Context, diff *git.GitDiff) ([]domain.Finding, error)
}

// Manager coordinates execution of all analyzers.
type Manager interface {
	Run(ctx context.Context, diff *git.GitDiff) []domain.Finding
}

type manager struct {
	analyzers []Analyzer
	logger    Logger
}

// New returns the standard analyzer manager.
func New(logger Logger) Manager {
	return &manager{
		logger: logger,
		analyzers: []Analyzer{
			&urlAnalyzer{},
			&checksumAnalyzer{},
			&shellAnalyzer{},
		},
	}
}

func newManager(logger Logger, analyzers ...Analyzer) *manager {
	return &manager{
		logger:    logger,
		analyzers: analyzers,
	}
}

func (m *manager) Run(
	ctx context.Context,
	diff *git.GitDiff,
) []domain.Finding {

	var findings []domain.Finding

	for _, analyzer := range m.analyzers {
		f, err := analyzer.Run(ctx, diff)
		if err != nil {
			if m.logger != nil {
				m.logger.Printf("analyzer %q failed: %v", analyzer.Name(), err)
			}
			// Analyzer failures are intentionally isolated.
			// Sprint 5 requires one failing analyzer not to
			// abort the overall analysis run.
			continue
		}

		findings = append(findings, f...)
	}

	return findings
}
