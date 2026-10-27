package main

import (
	"errors"
	"fmt"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
)

// fetchLoss is what fetching a newer upstream commit would discard: the
// new commit replaces the stored review with a new Pending one
// (workflow.md § Transition Matrix, note 2).
type fetchLoss int

const (
	// lossNone: the review is decided and nothing is waiting on it — it
	// was merged, or rejected.
	lossNone fetchLoss = iota
	// lossPendingReview: an undecided review, with its notes and findings.
	lossPendingReview
	// lossUnmergedApproval: an approval whose commit was never merged.
	lossUnmergedApproval
)

// fetchLossFor returns what a fetch would discard for a review with the
// given status; merged says whether its commit is on local master and
// matters only for an approved review. ok is false for a status update
// does not offer (anything but Pending, Approved, Rejected).
func fetchLossFor(status domain.ReviewStatus, merged bool) (loss fetchLoss, ok bool) {
	switch status {
	case domain.ReviewPending:
		return lossPendingReview, true
	case domain.ReviewApproved:
		if merged {
			return lossNone, true
		}
		return lossUnmergedApproval, true
	case domain.ReviewRejected:
		return lossNone, true
	}
	return 0, false
}

// individualCandidate is a stale package whose fetch would discard
// something, so update asks about it on its own, with a warning.
type individualCandidate struct {
	updateCandidate
	loss fetchLoss
}

// pkgReason names a package and why it is reported (#402).
type pkgReason struct {
	pkg    domain.PackageName
	reason string
}

// Reasons a package is not checked: a property of the package, true on
// every run, so it does not affect the exit status (workflow.md § Update).
const reasonNotOnAUR = "not on the AUR"

// updatePlan is what update offers, in the order it offers it, and what
// the scan found about every other package (workflow.md § Update).
type updatePlan struct {
	pendingUpToDate []domain.PackageName  // pending, AUR not newer: one informational line each
	individual      []individualCandidate // asked one by one, with a warning
	batch           []updateCandidate     // the y/n/a/q list (#424)

	upToDate   int         // the AUR has not moved past the local version
	notChecked []pkgReason // the version cannot be compared
	failed     []pkgReason // the check itself failed
	// pending: packages whose review was Pending when scanned. With the
	// reviews update starts, they make the exit status 10.
	pending []domain.PackageName
}

// planUpdate sorts the AUR check's pairs into an updatePlan. A pair's
// local version is the version at the reviewed commit (workflow.md
// § Local Version, #426), so AURStale means a fetch brings something new.
// review returns a package's stored review; merged reports whether an
// approved review's commit is on local master, and is called only for
// approved reviews.
//
// Every pair lands somewhere (#402): up to date, offered, not checked
// (aurinfo.ErrVersionNotReadable, or not on the AUR) or failed (any other
// resolver error, a review read error, a review status update does not
// know). A failed merge check is named as failed, and the package is
// treated as not merged (fail-closed): if stale, it is still offered, on
// its own.
func planUpdate(
	pairs []aurinfo.VersionPair,
	review func(domain.PackageName) (*domain.Review, error),
	merged func(pkg domain.PackageName, commit string) (bool, error),
) updatePlan {
	var p updatePlan
	for _, pair := range pairs {
		if pair.LocalErr != nil && !errors.Is(pair.LocalErr, aurinfo.ErrVersionNotReadable) {
			p.failed = append(p.failed, pkgReason{pair.Pkg, pair.LocalErr.Error()})
			continue
		}
		r, err := review(pair.Pkg)
		if err != nil {
			p.failed = append(p.failed, pkgReason{pair.Pkg, "read review: " + err.Error()})
			continue
		}
		if r.Status == domain.ReviewPending {
			p.pending = append(p.pending, pair.Pkg)
		}
		isMerged := false
		if r.Status == domain.ReviewApproved {
			var err error
			isMerged, err = merged(pair.Pkg, r.UpstreamCommit)
			if err != nil {
				isMerged = false
				p.failed = append(p.failed, pkgReason{pair.Pkg, "check whether the approved commit is merged: " + err.Error()})
			}
		}
		loss, ok := fetchLossFor(r.Status, isMerged)
		if !ok {
			p.failed = append(p.failed, pkgReason{pair.Pkg, fmt.Sprintf("unknown review status %q", r.Status)})
			continue
		}

		switch {
		case pair.AURUnknown:
			p.notChecked = append(p.notChecked, pkgReason{pair.Pkg, reasonNotOnAUR})
			continue
		case pair.LocalErr != nil:
			p.notChecked = append(p.notChecked, pkgReason{pair.Pkg, pair.LocalErr.Error()})
			continue
		case !pair.AURStale:
			p.upToDate++
			if loss == lossPendingReview {
				p.pendingUpToDate = append(p.pendingUpToDate, pair.Pkg)
			}
			continue
		}

		c := updateCandidate{
			pkg:          pair.Pkg,
			aurVer:       pair.AURVersion,
			localVer:     pair.LocalVersion,
			localUnknown: pair.LocalUnknown,
		}
		if loss == lossNone {
			p.batch = append(p.batch, c)
		} else {
			p.individual = append(p.individual, individualCandidate{updateCandidate: c, loss: loss})
		}
	}
	return p
}

// individualPrompt is the question for a package update asks about on
// its own: it says what the fetch would discard.
func individualPrompt(c individualCandidate) string {
	if c.loss == lossUnmergedApproval {
		return fmt.Sprintf("%s: approved review (%s) is not merged yet — AUR has moved to %s. Fetching replaces the approval with a new pending review. Proceed? [y/n/q] ", c.pkg, c.localVer, c.aurVer)
	}
	return fmt.Sprintf("%s: pending review is stale — AUR has moved to %s since this review was created at %s. Confirming will discard the current review's notes/findings. Proceed? [y/n/q] ", c.pkg, c.aurVer, c.localVer)
}
