package git

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// reviewDiff484 runs ReviewDiff over samplePatch (PKGBUILD, .SRCINFO,
// yay.install) with the given runner outputs and errors after the diff
// call, and returns the diff, the runner and the error.
func reviewDiff484(t *testing.T, base string, after []string, afterErrs []error) (*GitDiff, *fakeRunner, error) {
	t.Helper()
	outputs := []string{"upfull\n"}
	if base != "" {
		outputs = append(outputs, "basefull\n")
	}
	outputs = append(outputs, "u1\x1fAlice\x1f2026-07-22T10:00:00Z\x1fBump\n", samplePatch)
	errs := make([]error, len(outputs))
	outputs = append(outputs, after...)
	errs = append(errs, afterErrs...)
	r := &fakeRunner{outputs: outputs, errs: errs}
	p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}
	diff, err := p.ReviewDiff(context.Background(), "yay", base, "up")
	return diff, r, err
}

func pkgbuild484(t *testing.T, d *GitDiff) FileDiff {
	t.Helper()
	for _, f := range d.Files {
		if f.Kind == FileKindPKGBUILD {
			return f
		}
	}
	t.Fatal("no PKGBUILD in Files")
	return FileDiff{}
}

// TestReviewDiffContents484: ReviewDiff gives the PKGBUILD its whole text
// at both commits, read from the verified commits, and leaves Patch and
// other files as they were.
func TestReviewDiffContents484(t *testing.T) {
	d, r, err := reviewDiff484(t, "base", []string{
		"100644 blob h1\tPKGBUILD\n", "pkgver=1.0\n",
		"100644 blob h2\tPKGBUILD\n", "pkgver=1.1\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := pkgbuild484(t, d)
	if !f.ContentLoaded || f.Old != "pkgver=1.0\n" || f.New != "pkgver=1.1\n" {
		t.Fatalf("PKGBUILD contents = loaded %v, old %q, new %q", f.ContentLoaded, f.Old, f.New)
	}
	if !strings.Contains(f.Patch, "+pkgver=1.1") {
		t.Fatalf("Patch changed: %q", f.Patch)
	}
	for _, other := range d.Files {
		if other.Kind != FileKindPKGBUILD && (other.ContentLoaded || other.Old != "" || other.New != "") {
			t.Fatalf("%s got contents; only the PKGBUILD should", other.Path)
		}
	}
	want := []string{
		"ls-tree basefull -- PKGBUILD", "cat-file blob h1",
		"ls-tree upfull -- PKGBUILD", "cat-file blob h2",
	}
	for i, w := range want {
		if got := strings.Join(lastArgs387(t, r, 4+i), " "); got != w {
			t.Fatalf("call %d = %q, want %q", 4+i, got, w)
		}
	}
	if len(r.calls) != 8 {
		t.Fatalf("runner calls = %d, want 8", len(r.calls))
	}
}

// TestReviewDiffContentsImport484: on Import there is no base: Old is
// empty and nothing is looked up for it.
func TestReviewDiffContentsImport484(t *testing.T) {
	d, r, err := reviewDiff484(t, "", []string{"100644 blob h2\tPKGBUILD\n", "pkgver=1.1\n"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := pkgbuild484(t, d)
	if !f.ContentLoaded || f.Old != "" || f.New != "pkgver=1.1\n" {
		t.Fatalf("PKGBUILD contents = loaded %v, old %q, new %q", f.ContentLoaded, f.Old, f.New)
	}
	if got := strings.Join(lastArgs387(t, r, 3), " "); got != "ls-tree upfull -- PKGBUILD" {
		t.Fatalf("first lookup = %q, want the upstream commit only", got)
	}
	if len(r.calls) != 5 {
		t.Fatalf("runner calls = %d, want 5", len(r.calls))
	}
}

// TestReviewDiffContentsAbsent484: a PKGBUILD missing at one end (added
// or deleted in the range) is "" there, with no read attempted.
func TestReviewDiffContentsAbsent484(t *testing.T) {
	for _, tc := range []struct {
		name     string
		after    []string
		old, new string
		calls    int
	}{
		{"added", []string{"", "100644 blob h2\tPKGBUILD\n", "pkgver=1.1\n"}, "", "pkgver=1.1\n", 7},
		{"deleted", []string{"100644 blob h1\tPKGBUILD\n", "pkgver=1.0\n", ""}, "pkgver=1.0\n", "", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, r, err := reviewDiff484(t, "base", tc.after, nil)
			if err != nil {
				t.Fatal(err)
			}
			f := pkgbuild484(t, d)
			if !f.ContentLoaded || f.Old != tc.old || f.New != tc.new {
				t.Fatalf("PKGBUILD contents = loaded %v, old %q, new %q", f.ContentLoaded, f.Old, f.New)
			}
			if len(r.calls) != tc.calls {
				t.Fatalf("runner calls = %d, want %d", len(r.calls), tc.calls)
			}
		})
	}
}

// TestReviewDiffContentsErrors484: a failed lookup or read, or a path
// that is not a file, fails ReviewDiff; nothing is returned half-loaded.
func TestReviewDiffContentsErrors484(t *testing.T) {
	boom := errors.New("exit status 128")
	for _, tc := range []struct {
		name  string
		after []string
		errs  []error
		want  string
	}{
		{"ls-tree fails", []string{""}, []error{boom}, "look up PKGBUILD at basefull"},
		{"cat-file fails", []string{"100644 blob h1\tPKGBUILD\n", ""}, []error{nil, boom}, "read PKGBUILD at basefull"},
		{"upstream ls-tree fails", []string{"", ""}, []error{nil, boom}, "look up PKGBUILD at upfull"},
		{"not a file", []string{"040000 tree t1\tPKGBUILD\n"}, nil, "not a file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, err := reviewDiff484(t, "base", tc.after, tc.errs)
			if err == nil {
				t.Fatalf("expected an error, got %+v", d)
			}
			if d != nil {
				t.Fatal("expected no diff with the error")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "yay") {
				t.Fatalf("error %q does not say %q for package yay", err, tc.want)
			}
		})
	}
}
