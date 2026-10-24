package screens

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// reviewLoadedMsg carries the result of the initial Git fetch/diff and
// review-engine EnsureReview call performed in Init. A non-nil err
// leaves diff/review at their zero values — callers must check err
// before touching either field. upstreamVersion is filled only for the
// reload after a refusal (#399), and is "" when it cannot be read.
type reviewLoadedMsg struct {
	diff            git.GitDiff
	review          *domain.Review
	err             error
	upstreamVersion string
}

func (m reviewLoadedMsg) GetReview() *domain.Review { return m.review }
func (m reviewLoadedMsg) GetDiff() git.GitDiff      { return m.diff }
func (m reviewLoadedMsg) GetErr() error             { return m.err }

// reviewSavedMsg carries the result of RecordDecision (Approve/Reject).
// event is "approve" or "reject".
type reviewSavedMsg struct {
	review   *domain.Review
	err      error
	event    string
	traceCtx context.Context
}
