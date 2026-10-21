package analyzer

import (
	"context"
	"errors"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

type fakeAnalyzer struct {
	name     string
	findings []domain.Finding
	err      error
}

func (f fakeAnalyzer) Name() string {
	return f.name
}

func (f fakeAnalyzer) Run(
	context.Context,
	*git.GitDiff,
) ([]domain.Finding, error) {

	if f.err != nil {
		return nil, f.err
	}

	return f.findings, nil
}

func TestManagerRunEmpty(t *testing.T) {
	m := newManager(discardLogger{})

	got := m.Run(context.Background(), &git.GitDiff{})

	if len(got) != 0 {
		t.Fatalf("expected no findings, got %d", len(got))
	}
}

func TestManagerCollectsFindings(t *testing.T) {
	m := newManager(discardLogger{},
		fakeAnalyzer{
			name: "a",
			findings: []domain.Finding{
				{Category: "url_new"},
			},
		},
		fakeAnalyzer{
			name: "b",
			findings: []domain.Finding{
				{Category: "shell_exec"},
			},
		},
	)

	got := m.Run(context.Background(), &git.GitDiff{})

	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}

	if got[0].Category != "url_new" {
		t.Fatal("unexpected first finding")
	}

	if got[1].Category != "shell_exec" {
		t.Fatal("unexpected second finding")
	}
}

func TestManagerContinuesAfterAnalyzerFailure(t *testing.T) {
	m := newManager(discardLogger{},
		fakeAnalyzer{
			name: "broken",
			err:  errors.New("boom"),
		},
		fakeAnalyzer{
			name: "working",
			findings: []domain.Finding{
				{Category: "checksum_refresh"},
			},
		},
	)

	got := m.Run(context.Background(), &git.GitDiff{})

	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}

	if got[0].Category != "checksum_refresh" {
		t.Fatal("unexpected finding")
	}
}

func TestManagerPreservesAnalyzerOrder(t *testing.T) {
	m := newManager(discardLogger{},
		fakeAnalyzer{
			findings: []domain.Finding{
				{Category: "version_bump"},
			},
		},
		fakeAnalyzer{
			findings: []domain.Finding{
				{Category: "url_new"},
			},
		},
	)

	got := m.Run(context.Background(), &git.GitDiff{})

	if got[0].Category != "version_bump" {
		t.Fatal("order changed")
	}

	if got[1].Category != "url_new" {
		t.Fatal("order changed")
	}
}
