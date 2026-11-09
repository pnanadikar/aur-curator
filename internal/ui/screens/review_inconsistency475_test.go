package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

const retiredCategory = "retired_category_475"

// loadFindings delivers a reviewLoadedMsg whose pending review has run
// analyzers with the given findings.
func loadFindings(t *testing.T, model ReviewScreen, findings []domain.Finding) ReviewScreen {
	t.Helper()
	updated, _ := model.Update(reviewLoadedMsg{
		diff: git.GitDiff{UpstreamCommit: "upstream123", LocalCommit: "local123"},
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings:       findings,
		},
	})
	return updated
}

func unmappedFindings() []domain.Finding {
	return []domain.Finding{
		{Analyzer: "checksum", Category: "version_bump", Summary: "pkgver changed"},
		{Analyzer: "shell", Category: retiredCategory, Summary: "old finding"},
	}
}

func readySized(t *testing.T) ReviewScreen {
	t.Helper()
	model, _ := newTestReviewScreen(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return updated
}

func TestInconsistencyModalOnLoad(t *testing.T) {
	model := loadFindings(t, readySized(t), unmappedFindings())

	if model.modal == nil || model.modalFlow != modalInconsistency {
		t.Fatalf("modal = %v, flow = %v; want the inconsistency modal", model.modal != nil, model.modalFlow)
	}
	view := model.View()
	for _, want := range []string{
		`Internal inconsistency: finding category "` + retiredCategory + `" has no severity (ranked HIGH).`,
		"Please file a bug report with this line, the package name and aur-curator's version.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("modal view missing %q", want)
		}
	}
}

func TestInconsistencyModalNotShownForMappedCategories(t *testing.T) {
	model := loadFindings(t, readySized(t), []domain.Finding{
		{Analyzer: "checksum", Category: "version_bump", Summary: "pkgver changed"},
	})
	if model.modal != nil {
		t.Fatalf("modal open (flow %v) with only mapped categories", model.modalFlow)
	}
	if strings.Contains(model.renderFindingsContent(), "Internal inconsistency") {
		t.Error("Findings tab shows the inconsistency lines with only mapped categories")
	}
}

func TestInconsistencyModalDismiss(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyEsc}} {
		t.Run(k.String(), func(t *testing.T) {
			model := loadFindings(t, readySized(t), unmappedFindings())
			updated, cmd := model.Update(k)
			model = updated
			if model.modal != nil {
				t.Fatalf("%s did not dismiss the modal", k)
			}
			if cmd != nil {
				if _, back := cmd().(msgs.BackMsg); back {
					t.Fatalf("%s left the screen instead of only dismissing the modal", k)
				}
			}
		})
	}
}

func TestInconsistencyModalOncePerOpening(t *testing.T) {
	model := loadFindings(t, readySized(t), unmappedFindings())
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated

	// A reload (e.g. after an operation) does not show it again.
	model = loadFindings(t, model, unmappedFindings())
	if model.modal != nil {
		t.Fatal("modal shown again on a reload within the same opening")
	}

	// Re-entering the screen (Refresh) is a new opening.
	model = model.Refresh()
	model = loadFindings(t, model, unmappedFindings())
	if model.modal == nil || model.modalFlow != modalInconsistency {
		t.Fatal("modal not shown after re-opening the screen")
	}
}

func TestInconsistencyLinesOnFindingsTab(t *testing.T) {
	model := loadFindings(t, readySized(t), unmappedFindings())
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated

	content := model.renderFindingsContent()
	want := `Internal inconsistency: finding category "` + retiredCategory + `" has no severity (ranked HIGH).`
	if !strings.Contains(content, want) {
		t.Errorf("Findings tab missing %q", want)
	}
	if i, j := strings.Index(content, "Internal inconsistency"), strings.Index(content, findingsScopeNote); i < 0 || j < 0 || i > j {
		t.Error("inconsistency lines should come before the scope note")
	}
	if !strings.Contains(content, "Findings: 2 (1 HIGH, 1 LOW)") {
		t.Error("the unmapped finding should count as HIGH")
	}
}
