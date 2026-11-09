package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/trace"
)

// aurAnalyzerName is the Finding.Analyzer of maintainer_changed.
const aurAnalyzerName = "aur"

// aurLocation is the Finding.Location of maintainer_changed: it points at
// the AUR's record, not at a file.
const aurLocation = "AUR record"

// now is time.Now, replaced in tests.
var now = time.Now

// checkAUR reads the package's AUR record into r (AUR, AURInfo, or
// AURError on failure) and returns the maintainer_changed findings from
// comparing it with r.PriorAUR (domain.md § AUR Record, #495).
func (e *Engine) checkAUR(ctx context.Context, r *domain.Review) []domain.Finding {
	rec, err := e.aur.Lookup(ctx, r.PackageName)
	if err != nil {
		r.AUR, r.AURInfo = nil, nil
		r.AURError = aurErrorText(r.PackageName, err)
		trace.Log(ctx, "aur-check", "pkg", string(r.PackageName), "status", "not_read", "error", err.Error())
		return nil
	}

	r.AURError = ""
	r.AUR = &domain.AURRecord{
		Maintainer:    rec.Maintainer,
		Orphaned:      rec.Orphaned,
		CoMaintainers: rec.CoMaintainers,
		ReadAt:        now().UTC(),
		ReviewID:      r.ID,
	}
	r.AURInfo = &domain.AURInfo{
		FirstSubmitted: rec.FirstSubmitted,
		NumVotes:       rec.NumVotes,
		Popularity:     rec.Popularity,
	}
	findings := compareAUR(r.PriorAUR, *r.AUR)
	trace.Log(ctx, "aur-check", "pkg", string(r.PackageName), "status", "read", "findings", len(findings))
	return findings
}

// aurErrorText says why the AUR record could not be read, for the
// Findings tab.
func aurErrorText(pkg domain.PackageName, err error) string {
	if errors.Is(err, aurinfo.ErrAURPackageNotFound) {
		return fmt.Sprintf("the AUR has no package named %s", pkg)
	}
	return err.Error()
}

// compareAUR compares the last recorded maintainer record with the one
// just read. It returns one maintainer_changed finding when the
// maintainer differs (including orphaned and adopted), or, with the same
// maintainer, when the co-maintainers differ; none when they match or
// when there is no prior record (analyzer-specs.md § AUR Record).
func compareAUR(prior *domain.AURRecord, current domain.AURRecord) []domain.Finding {
	if prior == nil {
		return nil
	}
	since := fmt.Sprintf("(since the review of %s)", prior.ReadAt.Format(time.DateOnly))

	var summary string
	switch {
	case prior.Orphaned && current.Orphaned:
		// still orphaned: compare co-maintainers below
	case current.Orphaned:
		summary = fmt.Sprintf("Package orphaned: was %s %s", prior.Maintainer, since)
	case prior.Orphaned:
		summary = fmt.Sprintf("Package adopted by %s %s", current.Maintainer, since)
	case prior.Maintainer != current.Maintainer:
		summary = fmt.Sprintf("AUR maintainer changed: %s → %s %s", prior.Maintainer, current.Maintainer, since)
	}
	if summary == "" {
		added, removed := setDiff(prior.CoMaintainers, current.CoMaintainers)
		if len(added) == 0 && len(removed) == 0 {
			return nil
		}
		var parts []string
		if len(added) > 0 {
			parts = append(parts, "added "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			parts = append(parts, "removed "+strings.Join(removed, ", "))
		}
		summary = fmt.Sprintf("Co-maintainers changed: %s %s", strings.Join(parts, "; "), since)
	}

	details, _ := json.Marshal(struct {
		Prior   domain.AURRecord `json:"prior"`
		Current domain.AURRecord `json:"current"`
	}{*prior, current})

	return []domain.Finding{{
		Analyzer: aurAnalyzerName,
		Category: analyzer.CategoryMaintainerChanged,
		Summary:  summary,
		Details:  details,
		Location: aurLocation,
	}}
}

// setDiff returns the names in b but not a (added) and in a but not b
// (removed), each sorted.
func setDiff(a, b []string) (added, removed []string) {
	for _, n := range b {
		if !slices.Contains(a, n) {
			added = append(added, n)
		}
	}
	for _, n := range a {
		if !slices.Contains(b, n) {
			removed = append(removed, n)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}
