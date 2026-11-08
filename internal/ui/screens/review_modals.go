package screens

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/widgets"
)

// modalFlow tracks which confirmation flow is currently open, so a
// single m.modal field can drive both confirmation shapes from
// tui-review.md § Confirmation Flows (Approve-without-analysis and the
// two-step Reject) without a separate bool per flow.
type modalFlow int

const (
	modalNone modalFlow = iota
	modalApprove
	modalRejectConfirm
	modalRejectReason
	modalInconsistency
)

func (m *ReviewScreen) openApproveModal() {
	p := m.engine.Policy()

	var lines []string
	if m.review != nil {
		if m.review.FindingsStatus == domain.FindingsNotRun {
			lines = append(lines,
				"No analyzers have been run for this review. Approve anyway?",
			)
		} else if len(m.review.Findings) == 0 {
			lines = append(lines, "There are 0 findings, approve?")
		} else {
			lines = append(lines, findingsSummary(p, m.review))
			lines = append(lines, "\nApprove?")
		}
	}
	lines = append(lines, "Reviewer notes (optional):")

	modal := widgets.NewConfirmTextModal(lines, "")
	modal.SetWidth(max(m.width-8, 48))
	m.modal = &modal
	m.modalFlow = modalApprove
}

func (m *ReviewScreen) openRejectConfirmModal() {
	modal := widgets.NewChoiceModal(
		"Reject this review?",
		"No further action will be taken \xe2\x80\x94 this update will not be",
		"installed.",
	)
	modal.SetWidth(max(m.width-8, 48))
	m.modal = &modal
	m.modalFlow = modalRejectConfirm
}

func (m *ReviewScreen) openRejectReasonModal() tea.Cmd {
	modal := widgets.NewTextModal("Reason for rejection (optional):")
	modal.SetWidth(max(m.width-8, 48))
	m.modal = &modal
	m.modalFlow = modalRejectReason
	return m.modal.Focus()
}

func (m *ReviewScreen) handleModalConfirmed(value string) tea.Cmd {
	switch m.modalFlow {
	case modalApprove:
		trace.Log(m.traceCtx, "handoff", "to", "RecordDecision", "from", "approve_confirm", "pkg", m.pkgName)
		decision := &domain.Review{
			Status:        domain.ReviewApproved,
			ReviewerNotes: value,
		}
		m.closeModal()
		return m.saveReviewCmd(decision)
	case modalRejectConfirm:
		trace.Log(m.traceCtx, "handoff", "to", "reject_reason_modal", "pkg", m.pkgName)
		return m.openRejectReasonModal()
	case modalInconsistency:
		m.closeModal()
		return nil
	default:
		m.closeModal()
		return nil
	}
}

func (m *ReviewScreen) handleModalSubmitted(value string) tea.Cmd {
	switch m.modalFlow {
	case modalRejectReason:
		trace.Log(m.traceCtx, "handoff", "to", "RecordDecision", "from", "reject_reason_modal", "pkg", m.pkgName)
		decision := &domain.Review{
			Status:        domain.ReviewRejected,
			ReviewerNotes: "",
			Rationale:     value,
		}
		m.closeModal()
		return m.saveReviewCmd(decision)
	default:
		m.closeModal()
		return nil
	}
}

func (m *ReviewScreen) handleModalSkipped() tea.Cmd {
	switch m.modalFlow {
	case modalRejectReason:
		trace.Log(m.traceCtx, "handoff", "to", "RecordDecision", "from", "reject_reason_skipped", "pkg", m.pkgName)
		decision := &domain.Review{
			Status:        domain.ReviewRejected,
			ReviewerNotes: "",
			Rationale:     "",
		}
		m.closeModal()
		return m.saveReviewCmd(decision)
	default:
		m.closeModal()
		return nil
	}
}

func (m *ReviewScreen) closeModal() {
	m.modal = nil
	m.modalFlow = modalNone
}

// unmappedCategories returns, sorted and once each, the categories of
// findings that have no severity mapping (#475).
func unmappedCategories(p policy.Engine, findings []domain.Finding) []string {
	seen := map[string]bool{}
	var cats []string
	for _, f := range findings {
		if !seen[f.Category] && !p.Mapped(f.Category) {
			seen[f.Category] = true
			cats = append(cats, f.Category)
		}
	}
	sort.Strings(cats)
	return cats
}

// inconsistencyLines is the user-facing text for unmapped categories,
// shown in the modal and at the top of the Findings tab
// (tui-review.md § Internal Inconsistency).
func inconsistencyLines(cats []string) []string {
	var lines []string
	for _, c := range cats {
		lines = append(lines,
			fmt.Sprintf("Internal inconsistency: finding category %q has no severity (ranked HIGH).", c),
			"Please file a bug report with this line, the package name and aur-curator's version.",
		)
	}
	return lines
}

// maybeOpenInconsistencyModal shows the internal-inconsistency modal for
// unmapped categories not yet shown since the screen was opened, unless
// another modal is open (#475).
func (m *ReviewScreen) maybeOpenInconsistencyModal() {
	if m.modal != nil || m.review == nil {
		return
	}
	if m.inconsistencyShown == nil {
		m.inconsistencyShown = map[string]bool{}
	}
	var fresh []string
	for _, c := range unmappedCategories(m.engine.Policy(), m.review.Findings) {
		if !m.inconsistencyShown[c] {
			m.inconsistencyShown[c] = true
			fresh = append(fresh, c)
		}
	}
	if len(fresh) == 0 {
		return
	}
	modal := widgets.NewNoticeModal(inconsistencyLines(fresh)...)
	modal.SetWidth(max(m.width-8, 48))
	m.modal = &modal
	m.modalFlow = modalInconsistency
}
