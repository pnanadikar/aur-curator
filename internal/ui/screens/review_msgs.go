package screens

import (
	"context"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// reviewLoadedMsg carries the result of the initial Git fetch/diff and
// review-engine EnsureReview call performed in Init. A non-nil err
// leaves diff/review at their zero values — callers must check err
// before touching either field.
type reviewLoadedMsg struct {
	diff   git.GitDiff
	review *domain.Review
	err    error
}

func (m reviewLoadedMsg) GetReview() *domain.Review { return m.review }
func (m reviewLoadedMsg) GetDiff() git.GitDiff      { return m.diff }
func (m reviewLoadedMsg) GetErr() error             { return m.err }

// reviewSavedMsg carries the result of RecordDecision (Approve/Reject).
type reviewSavedMsg struct {
	review   *domain.Review
	err      error
	traceCtx context.Context
}
