package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
)

// Exit status of outdated (workflow.md § Outdated, #403).
const (
	exitOutdatedOK     = 0 // completed, whether or not anything was listed
	exitOutdatedFailed = 1 // a check failed, or outdated could not start or scan
)

// aurRequestReason shortens a failed AUR request's error for stderr. The
// request URL names every curated package, so only the cause is kept:
// "timed out", the network error, or the HTTP status. --trace has the
// full request.
func aurRequestReason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return "timed out"
		}
		return ue.Err.Error()
	}
	return strings.TrimPrefix(err.Error(), aurinfo.ErrAURClientUnreachable.Error()+": ")
}

// outdatedMarker is what a stale package's line adds when a Fetch would
// discard something (workflow.md § Outdated, § Offer).
func outdatedMarker(loss fetchLoss) string {
	switch loss {
	case lossPendingReview:
		return " (older review pending)"
	case lossUnmergedApproval:
		return " (approved, not merged)"
	default:
		return ""
	}
}

// printOutdated writes the outdated list for a scan (workflow.md
// § Outdated, #403) and returns the exit status.
//
// One line per stale package goes to w, in the order of packages (the
// catalog's): "<name> <reviewed> -> <aur>", with a marker when a Fetch
// would discard something. Each failed check goes to errW with its reason
// and makes the exit 1; the stale lines are still written. Up-to-date and
// not-checked packages print nothing.
func printOutdated(w, errW io.Writer, packages []domain.PackageName, p updatePlan) int {
	lines := make(map[domain.PackageName]string, len(p.individual)+len(p.batch))
	for _, c := range p.individual {
		lines[c.pkg] = string(c.pkg) + " " + c.describe() + outdatedMarker(c.loss)
	}
	for _, c := range p.batch {
		lines[c.pkg] = string(c.pkg) + " " + c.describe()
	}
	for _, pkg := range packages {
		if line, ok := lines[pkg]; ok {
			_, _ = fmt.Fprintln(w, line)
		}
	}

	for _, f := range p.failed {
		_, _ = fmt.Fprintf(errW, "%s: %s\n", f.pkg, f.reason)
	}
	if len(p.failed) > 0 {
		return exitOutdatedFailed
	}
	return exitOutdatedOK
}
