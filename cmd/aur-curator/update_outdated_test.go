package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
)

// #403 — outdated lists the stale packages from update's scan and
// changes nothing (workflow.md § Outdated).

// outdatedFor runs the scan on pairs and reviews, then printOutdated,
// returning stdout, stderr and the exit status. merged reports the
// commits listed in mergedCommits as merged, and fails for "merge-error".
func outdatedFor(t *testing.T, pairs []aurinfo.VersionPair, reviews map[domain.PackageName]*domain.Review, mergedCommits ...string) (string, string, int) {
	t.Helper()
	review := func(pkg domain.PackageName) (*domain.Review, error) {
		r, ok := reviews[pkg]
		if !ok {
			return nil, errors.New("store error")
		}
		return r, nil
	}
	merged := func(pkg domain.PackageName, commit string) (bool, error) {
		if pkg == "merge-error" {
			return false, errors.New("git broke")
		}
		for _, c := range mergedCommits {
			if c == commit {
				return true, nil
			}
		}
		return false, nil
	}
	packages := make([]domain.PackageName, len(pairs))
	for i, p := range pairs {
		packages[i] = p.Pkg
	}
	var out, errOut bytes.Buffer
	code := printOutdated(&out, &errOut, packages, planUpdate(pairs, review, merged))
	return out.String(), errOut.String(), code
}

func stale403(pkg, local, aur string) aurinfo.VersionPair {
	return aurinfo.VersionPair{Pkg: domain.PackageName(pkg), LocalVersion: local, AURVersion: aur, AURStale: true}
}

func current403(pkg, ver string) aurinfo.VersionPair {
	return aurinfo.VersionPair{Pkg: domain.PackageName(pkg), LocalVersion: ver, AURVersion: ver}
}

func TestOutdatedNothingStale(t *testing.T) {
	pairs := []aurinfo.VersionPair{current403("pfetch", "1.11.0-1"), current403("yay", "12.4.2-1")}
	reviews := map[domain.PackageName]*domain.Review{
		"pfetch": {Status: domain.ReviewApproved, UpstreamCommit: "m1"},
		"yay":    {Status: domain.ReviewPending, UpstreamCommit: "p1"}, // pending, covers the AUR's version
	}
	out, errOut, code := outdatedFor(t, pairs, reviews, "m1")
	if out != "" || errOut != "" || code != exitOutdatedOK {
		t.Fatalf("got stdout %q, stderr %q, exit %d; want nothing, exit 0", out, errOut, code)
	}
}

func TestOutdatedListsStaleWithMarkers(t *testing.T) {
	pairs := []aurinfo.VersionPair{
		stale403("pfetch", "1.11.0-1", "1.12.0-1"), // approved and merged
		stale403("yay", "12.4.1-1", "12.4.2-1"),    // pending
		current403("units", "2.24-1"),              // up to date
		stale403("broot", "0.44.1-1", "0.45.0-1"),  // approved, not merged
		stale403("rua", "0.19.9-1", "0.19.10-2"),   // rejected
		{Pkg: "computed", AURVersion: "1.0-1", LocalUnknown: true, LocalErr: aurinfo.ErrVersionNotReadable},
		{Pkg: "gone", LocalVersion: "1.0-1", AURUnknown: true},
	}
	reviews := map[domain.PackageName]*domain.Review{
		"pfetch":   {Status: domain.ReviewApproved, UpstreamCommit: "m1"},
		"yay":      {Status: domain.ReviewPending, UpstreamCommit: "p1"},
		"units":    {Status: domain.ReviewApproved, UpstreamCommit: "m2"},
		"broot":    {Status: domain.ReviewApproved, UpstreamCommit: "a1"},
		"rua":      {Status: domain.ReviewRejected, UpstreamCommit: "r1"},
		"computed": {Status: domain.ReviewApproved, UpstreamCommit: "m3"},
		"gone":     {Status: domain.ReviewApproved, UpstreamCommit: "m4"},
	}
	out, errOut, code := outdatedFor(t, pairs, reviews, "m1", "m2", "m3", "m4")

	// Catalog order, whatever group the scan put each package in; not
	// checked packages are silent.
	want := "pfetch 1.11.0-1 -> 1.12.0-1\n" +
		"yay 12.4.1-1 -> 12.4.2-1 (older review pending)\n" +
		"broot 0.44.1-1 -> 0.45.0-1 (approved, not merged)\n" +
		"rua 0.19.9-1 -> 0.19.10-2\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if errOut != "" || code != exitOutdatedOK {
		t.Errorf("stderr %q, exit %d; want none, exit 0", errOut, code)
	}
}

// A failed check is named on stderr and makes the exit 1; the stale
// packages are still listed.
func TestOutdatedFailedCheck(t *testing.T) {
	pairs := []aurinfo.VersionPair{
		stale403("pfetch", "1.11.0-1", "1.12.0-1"),
		{Pkg: "timeout", AURVersion: "1.1-1", LocalUnknown: true, LocalErr: errors.New("read PKGBUILD at abc1234: timed out")},
		stale403("no-review-record", "1.0-1", "1.1-1"),
		stale403("merge-error", "2.0-1", "2.1-1"),
	}
	reviews := map[domain.PackageName]*domain.Review{
		"pfetch":      {Status: domain.ReviewRejected, UpstreamCommit: "r1"},
		"timeout":     {Status: domain.ReviewApproved, UpstreamCommit: "m1"},
		"merge-error": {Status: domain.ReviewApproved, UpstreamCommit: "a1"},
	}
	out, errOut, code := outdatedFor(t, pairs, reviews)

	// A failed merge check is treated as not merged (fail-closed), so the
	// package is listed with the unmerged marker and also named as failed.
	wantOut := "pfetch 1.11.0-1 -> 1.12.0-1\n" +
		"merge-error 2.0-1 -> 2.1-1 (approved, not merged)\n"
	if out != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, wantOut)
	}
	for _, name := range []string{"timeout: ", "no-review-record: read review: store error", "merge-error: "} {
		if !bytes.Contains([]byte(errOut), []byte(name)) {
			t.Errorf("stderr lacks %q:\n%s", name, errOut)
		}
	}
	if code != exitOutdatedFailed {
		t.Errorf("exit %d, want %d", code, exitOutdatedFailed)
	}
}

// The AUR request error is cut to its cause: the request URL names every
// curated package.
func TestAURRequestReason(t *testing.T) {
	unreachable := aurinfo.ErrAURClientUnreachable
	long := "https://aur.archlinux.org/rpc?v=5&type=info&arg[]=a&arg[]=b"
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"timeout", fmt.Errorf("%w: %w", unreachable, &url.Error{Op: "Get", URL: long, Err: context.DeadlineExceeded}), "timed out"},
		{"network error", fmt.Errorf("%w: %w", unreachable, &url.Error{Op: "Get", URL: long, Err: errors.New("dial tcp: lookup aur.archlinux.org: no such host")}), "dial tcp: lookup aur.archlinux.org: no such host"},
		{"HTTP status", fmt.Errorf("%w: HTTP 503", unreachable), "HTTP 503"},
		{"other", errors.New("something else"), "something else"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aurRequestReason(tc.err); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
