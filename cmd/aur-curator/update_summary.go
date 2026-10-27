package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/review"
)

// Exit status of update (workflow.md § Update, #402).
const (
	exitUpdateNothingToReview = 0
	exitUpdateFailed          = 1
	exitUpdateReviewsPending  = 10
)

// updateSummary collects what one update run did, for the summary line
// that replaces "Update complete." and for the exit status (#402).
type updateSummary struct {
	total      int                  // curated packages
	upToDate   int                  // scan: the AUR has not moved past the local version
	notChecked []pkgReason          // scan: the version cannot be compared
	failed     []pkgReason          // scan, merge check or fetch failed
	offered    int                  // stale packages offered
	attempted  int                  // fetches run, whatever their outcome
	fetched    []domain.PackageName // fetches that completed
	started    []domain.PackageName // ...and started a review
	pending    bool                 // a review was Pending when scanned
	readErr    error                // reading an answer failed (not EOF)
}

// newUpdateSummary starts a summary from the scan's plan.
func newUpdateSummary(total int, p updatePlan) *updateSummary {
	return &updateSummary{
		total:      total,
		upToDate:   p.upToDate,
		notChecked: p.notChecked,
		failed:     append([]pkgReason(nil), p.failed...),
		offered:    len(p.individual) + len(p.batch),
		pending:    len(p.pending) > 0,
	}
}

// recordFetch prints the outcome of one fetch (#405) and records it. A
// fetch that ran but could not read the upstream commit (no review step
// ran) is a failure.
func (s *updateSummary) recordFetch(out, errOut io.Writer, pkg domain.PackageName, res review.FetchResult, err error) {
	s.attempted++
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(errOut, "%s: fetch failed: %v\n", pkg, err)
		s.failed = append(s.failed, pkgReason{pkg, "fetch failed: " + err.Error()})
	case res.EmptyResult:
		const reason = "fetch ran, but the upstream commit could not be read; no review step ran"
		_, _ = fmt.Fprintf(errOut, "%s: %s.\n", pkg, reason)
		s.failed = append(s.failed, pkgReason{pkg, reason})
	case res.ReviewStarted:
		_, _ = fmt.Fprintf(out, "%s: fetch complete — review started for %s.\n", pkg, shortHash(res.ReviewCommit))
		s.fetched = append(s.fetched, pkg)
		s.started = append(s.started, pkg)
	default:
		_, _ = fmt.Fprintf(out, "%s: fetch complete — no new upstream commit.\n", pkg)
		s.fetched = append(s.fetched, pkg)
	}
}

// skipped is the number of offered packages not fetched: answered n, or
// skipped by q or end of input.
func (s *updateSummary) skipped() int {
	return s.offered - s.attempted
}

// line is the last line of update: did anything change, and is there
// anything to review (workflow.md § Summary and Exit Status).
func (s *updateSummary) line() string {
	if len(s.failed) == 0 && len(s.notChecked) == 0 && s.upToDate == s.total {
		return fmt.Sprintf("All %d curated packages match the AUR.", s.total)
	}
	var parts []string
	if n := len(s.fetched); n > 0 {
		p := fmt.Sprintf("Fetched %d", n)
		if len(s.started) > 0 {
			p += " (new reviews pending: " + joinNames(s.started) + ")"
		}
		parts = append(parts, p)
	}
	if n := s.skipped(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", n))
	}
	if n := len(s.failed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", n))
	}
	if n := len(s.notChecked); n > 0 {
		parts = append(parts, fmt.Sprintf("%d not checked", n))
	}
	if s.upToDate > 0 {
		parts = append(parts, fmt.Sprintf("%d up to date", s.upToDate))
	}
	return strings.Join(parts, "; ") + "."
}

// print writes the not-checked packages (out) and the failures (errOut),
// each with its reason, then the summary line (out).
func (s *updateSummary) print(out, errOut io.Writer) {
	for _, r := range s.notChecked {
		_, _ = fmt.Fprintf(out, "Not checked: %s: %s.\n", r.pkg, r.reason)
	}
	for _, r := range s.failed {
		_, _ = fmt.Fprintf(errOut, "Failed: %s: %s.\n", r.pkg, r.reason)
	}
	if s.readErr != nil {
		_, _ = fmt.Fprintf(errOut, "Failed: reading the answer: %v.\n", s.readErr)
	}
	_, _ = fmt.Fprintln(out, s.line())
}

// exitCode: 1 when anything failed, else 10 when a review is pending at
// the end of the run, else 0 (workflow.md § Summary and Exit Status).
func (s *updateSummary) exitCode() int {
	switch {
	case len(s.failed) > 0 || s.readErr != nil:
		return exitUpdateFailed
	case s.pending || len(s.started) > 0:
		return exitUpdateReviewsPending
	}
	return exitUpdateNothingToReview
}

func joinNames(pkgs []domain.PackageName) string {
	names := make([]string, len(pkgs))
	for i, p := range pkgs {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}

// shortHash abbreviates a commit hash for messages.
func shortHash(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
