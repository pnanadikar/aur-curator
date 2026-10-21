package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanFixture checks the scanner against testdata/fixture, which holds
// well-formed calls, malformed calls, spread calls, an aliased import,
// activitylog.Log and procgroup.Run fixtures, and a non-matching Log method
// as a decoy.
func TestScanFixture(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	total, bad, _, err := scan(filepath.Join("testdata", "fixture"), &out)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// well-formed + malformed + spread + aliased + activitylog + procgroup;
	// the decoy is not counted.
	if total != 9 {
		t.Errorf("total = %d, want 9", total)
	}
	if bad != 4 {
		t.Errorf("bad = %d, want 4\noutput:\n%s", bad, out.String())
	}

	report := out.String()
	for _, want := range []string{"LABEL_PLAIN", "LABEL_ALIASED", "ACTIVITYLOG", "PROCGROUP"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing malformed call %q\noutput:\n%s", want, report)
		}
	}
	for _, unwanted := range []string{`"pkg", "yay"`, "kv...", `"odd"`} {
		if strings.Contains(report, unwanted) {
			t.Errorf("report flags a call it should not: %q\noutput:\n%s", unwanted, report)
		}
	}
}

// TestScanSkipsTestdata ensures a scan from a parent directory never
// descends into testdata/, so the deliberately malformed fixtures can't
// fail `make tracecheck` on the real repository.
func TestScanSkipsTestdata(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	_, bad, _, err := scan(".", &out)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if bad != 0 {
		t.Errorf("bad = %d, want 0 — testdata/ was scanned\noutput:\n%s", bad, out.String())
	}
}

// TestScanMissingRoot reports an error rather than a clean result.
func TestScanMissingRoot(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("precondition: path unexpectedly exists")
	}
	if _, _, _, err := scan(missing, &bytes.Buffer{}); err == nil {
		t.Error("scan of a missing root returned nil error")
	}
}
