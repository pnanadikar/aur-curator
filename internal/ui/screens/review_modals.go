package screens

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
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
