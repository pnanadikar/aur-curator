package analyzer

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/git"
)

func TestChecksumAnalyzer_NoPKGBUILD(t *testing.T) {
	a := &checksumAnalyzer{}

	findings, err := a.Run(context.Background(), &git.GitDiff{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("got %d findings", len(findings))
	}
}

func TestVersionBumpPkgver(t *testing.T) {
	findings := runChecksumFiles(t, "pkgver=1.0", "pkgver=2.0")

	if countCategory(findings, "version_bump") != 1 {
		t.Fatal(findings)
	}
}

func TestVersionBumpPkgrel(t *testing.T) {
	findings := runChecksumFiles(t, "pkgrel=1", "pkgrel=2")

	if countCategory(findings, "version_bump") != 1 {
		t.Fatal(findings)
	}
}

func TestVersionBumpBothStillOneFinding(t *testing.T) {
	findings := runChecksumFiles(t, "pkgver=1\npkgrel=1", "pkgver=2\npkgrel=2")

	if countCategory(findings, "version_bump") != 1 {
		t.Fatal(findings)
	}
}

// TestPGPRemoved covers full removal — every key-looking entry gone,
// nothing left in validpgpkeys. Going from two keys down to one
// *remaining* key is deliberately NOT covered here: verification still
// succeeds against the surviving key, so that's key rotation/cleanup,
// not "PGP verification removed" — see
// docs/domain/analyzer-specs.md § Checksum Analyzer, § Known
// Limitations ("PGP key rotation... is not flagged by any current
// category — only full removal is").
func TestPGPRemoved(t *testing.T) {
	findings := runChecksumFiles(t, "validpgpkeys=('AAAAAAAAAAAAAAAA' 'BBBBBBBBBBBBBBBB')", "validpgpkeys=()")

	if countCategory(findings, "pgp_removed") != 1 {
		t.Fatal(findings)
	}
}

// TestPGPKeyRotationNotFlagged: one of two keys dropped, one remains —
// verification still succeeds, so this must NOT produce a pgp_removed
// finding. (A key that is replaced or added is pgp_key_changed, #476.)
func TestPGPKeyRotationNotFlagged(t *testing.T) {
	findings := runChecksumFiles(t, "validpgpkeys=('AAAAAAAAAAAAAAAA' 'BBBBBBBBBBBBBBBB')", "validpgpkeys=('AAAAAAAAAAAAAAAA')")

	if countCategory(findings, "pgp_removed") != 0 {
		t.Fatal(findings)
	}
}

func TestChecksumRemovedSkip(t *testing.T) {
	findings := runChecksumFiles(t, "sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", "sha256sums=('SKIP')")

	if countCategory(findings, "checksum_removed") != 1 {
		t.Fatal(findings)
	}
}

func TestChecksumRemovedEmpty(t *testing.T) {
	findings := runChecksumFiles(t, "sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", "sha256sums=('')")

	if countCategory(findings, "checksum_removed") != 1 {
		t.Fatal(findings)
	}
}

func TestChecksumRefresh(t *testing.T) {
	findings := runChecksumFiles(t, "sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", "sha256sums=('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')")

	if countCategory(findings, "checksum_refresh") != 1 {
		t.Fatal(findings)
	}
}

func TestChecksumUnchanged(t *testing.T) {
	findings := runChecksumFiles(t, "sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", "sha256sums=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')")

	if len(findings) != 0 {
		t.Fatal(findings)
	}
}

func TestArchSpecificChecksumArray(t *testing.T) {
	findings := runChecksumFiles(t, "sha256sums_x86_64=('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')", "sha256sums_x86_64=('SKIP')")

	if countCategory(findings, "checksum_removed") != 1 {
		t.Fatal(findings)
	}
}
