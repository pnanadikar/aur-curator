package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/widgets"
)

// TestAURLines covers each variant of the Findings tab's AUR lines
// (tui-review.md § AUR Lines, #495).
func TestAURLines(t *testing.T) {
	read := time.Date(2026, 11, 9, 6, 55, 0, 0, time.UTC)
	prior := &domain.AURRecord{Maintainer: "valandil", ReadAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}
	info := &domain.AURInfo{FirstSubmitted: time.Date(2025, 2, 1, 2, 19, 28, 0, time.UTC), NumVotes: 1, Popularity: 0.00011}
	changed := []domain.Finding{{Category: analyzer.CategoryMaintainerChanged}}

	tests := []struct {
		name   string
		review domain.Review
		want   []string
		ok     bool
	}{
		{"before #495: nothing", domain.Review{BaseCommit: "b"}, nil, true},
		{
			"first review (Import)",
			domain.Review{AUR: &domain.AURRecord{Maintainer: "valandil", ReadAt: read}, AURInfo: info},
			[]string{
				"AUR: maintainer valandil",
				"AUR: first submitted 2025-02-01 (646 days old); 1 vote; popularity 0.00011",
				"AUR: maintainer not compared: no earlier record",
			},
			true,
		},
		{
			"later review, unchanged; no standing line",
			domain.Review{BaseCommit: "b", AUR: &domain.AURRecord{Maintainer: "valandil", ReadAt: read}, AURInfo: info, PriorAUR: prior},
			[]string{"AUR: maintainer valandil", "AUR: maintainer unchanged since 2026-10-30"},
			true,
		},
		{
			"changed: the finding says it, no comparison line",
			domain.Review{BaseCommit: "b", AUR: &domain.AURRecord{Maintainer: "mallory", CoMaintainers: []string{"eve"}, ReadAt: read}, PriorAUR: prior, Findings: changed},
			[]string{"AUR: maintainer mallory; co-maintainers eve"},
			true,
		},
		{
			"orphaned, carried record lost",
			domain.Review{BaseCommit: "b", AUR: &domain.AURRecord{Orphaned: true, ReadAt: read}},
			[]string{"AUR: orphaned", "AUR: maintainer not compared: no earlier record"},
			true,
		},
		{
			"read failed",
			domain.Review{AURError: "aur: client unreachable: HTTP 503", PriorAUR: prior},
			[]string{"AUR record not read: aur: client unreachable: HTTP 503. Maintainer not compared."},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := aurLines(&tt.review)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") || ok != tt.ok {
				t.Errorf("aurLines =\n%s\n(ok %v)\nwant\n%s\n(ok %v)",
					strings.Join(got, "\n"), ok, strings.Join(tt.want, "\n"), tt.ok)
			}
		})
	}
}

// TestFindingsTabShowsAURLines: the lines sit between the scope note and
// the findings header.
func TestFindingsTabShowsAURLines(t *testing.T) {
	model := loadFindings(t, readySized(t), []domain.Finding{{Analyzer: "checksum", Category: "version_bump", Summary: "x"}})
	model.review.AUR = &domain.AURRecord{Maintainer: "valandil", ReadAt: time.Now()}
	model.review.PriorAUR = &domain.AURRecord{Maintainer: "valandil", ReadAt: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}
	model.review.BaseCommit = "b"

	out := model.renderFindingsContent()
	note := strings.Index(out, findingsScopeNote)
	aur := strings.Index(out, "AUR: maintainer valandil")
	header := strings.Index(out, "Findings: 1")
	if note < 0 || aur < 0 || header < 0 || note >= aur || aur >= header {
		t.Errorf("want scope note, AUR lines, header in that order; got:\n%s", out)
	}
}

// TestNotAnalysedLinesFitTheModal: the modal does not wrap, so each line
// must fit an 80-column terminal's modal (#495).
func TestNotAnalysedLinesFitTheModal(t *testing.T) {
	for _, l := range notAnalysedLines {
		if len(l) > 64 {
			t.Errorf("%d columns, want at most 64: %q", len(l), l)
		}
	}
	m := widgets.NewConfirmTextModal(notAnalysedLines, "Reviewer notes (optional):")
	m.SetWidth(80 - 8)
	for _, l := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("modal line is %d columns on an 80-column terminal: %q", w, l)
		}
	}
}
