package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// runChecksumFiles runs the Checksum analyzer on whole old and new PKGBUILDs
// (#484, #485).
func runChecksumFiles(t *testing.T, old, new string) []domain.Finding {
	t.Helper()
	findings, err := (&checksumAnalyzer{}).Run(context.Background(), loaded484(old, new))
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

// #476: pgp_key_changed fires when validpgpkeys gains a key and still
// has one; full removal stays pgp_removed only. #484: the old and new
// PKGBUILD are compared whole.
func TestPGPKeyChanged(t *testing.T) {
	const (
		keyA = "AAAAAAAAAAAAAAAA"
		keyB = "BBBBBBBBBBBBBBBB"
		keyC = "CCCCCCCCCCCCCCCC"
	)
	tests := []struct {
		name        string
		old, new    string
		wantChanged int
		wantRemoved int
		wantSummary string // pgp_key_changed
		wantLoc     string // the one PGP finding's Location, if set
	}{
		{
			name: "rotation: one key replaced by another",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=('" + keyB + "')",
			wantChanged: 1, wantSummary: "PGP key replaced: " + keyB, wantLoc: "PKGBUILD:1",
		},
		{
			name: "key added, old one kept",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=('" + keyA + "' '" + keyB + "')",
			wantChanged: 1, wantSummary: "PGP key added: " + keyB,
		},
		{
			name:        "multi-line: one of two replaced, other key unchanged",
			old:         "validpgpkeys=(\n  '" + keyA + "'\n  '" + keyB + "'\n)",
			new:         "validpgpkeys=(\n  '" + keyA + "'\n  '" + keyC + "'\n)",
			wantChanged: 1, wantSummary: "PGP key replaced: " + keyC, wantLoc: "PKGBUILD:3",
		},
		{
			name: "full removal is pgp_removed only",
			old:  "validpgpkeys=('" + keyA + "' '" + keyB + "')", new: "validpgpkeys=()",
			wantRemoved: 1, wantLoc: "PKGBUILD:1",
		},
		{
			name: "array deleted: pgp_removed with no line",
			old:  "pkgname=x\nvalidpgpkeys=('" + keyA + "')", new: "pkgname=x",
			wantRemoved: 1, wantLoc: "PKGBUILD",
		},
		{
			name: "one of two keys dropped, nothing new",
			old:  "validpgpkeys=('" + keyA + "' '" + keyB + "')", new: "validpgpkeys=('" + keyA + "')",
		},
		{
			name: "unchanged list beside other changes",
			old:  "validpgpkeys=('" + keyA + "')\npkgver=1", new: "validpgpkeys=('" + keyA + "')\npkgrel=2\npkgver=1",
		},
		{
			name: "Import: every key is added",
			old:  "", new: "pkgname=foo\nvalidpgpkeys=('" + keyA + "' '" + keyB + "')",
			wantChanged: 1, wantSummary: "PGP key added: " + keyA + ", " + keyB, wantLoc: "PKGBUILD:2",
		},
		{
			name: "unchanged key beside a changed line: nothing",
			old:  "source=('https://a.example/1.tar.gz')\nvalidpgpkeys=('" + keyA + "')",
			new:  "source=('https://a.example/2.tar.gz')\nvalidpgpkeys=('" + keyA + "')",
		},
		{
			name:        "key replaced beside a changed line",
			old:         "source=('https://a.example/1.tar.gz')\nvalidpgpkeys=('" + keyA + "')",
			new:         "source=('https://a.example/2.tar.gz')\nvalidpgpkeys=('" + keyB + "')",
			wantChanged: 1, wantSummary: "PGP key replaced: " + keyB, wantLoc: "PKGBUILD:2",
		},
		{
			name: "non-key entry added is not a key",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=('" + keyA + "' 'not-a-key')",
		},
		{
			name: "unquoted keys, reformatted: nothing",
			old:  "validpgpkeys=(" + keyA + " " + keyB + ")", new: "validpgpkeys=(\n  '" + keyB + "'  # Bob\n  " + keyA + "\n)",
		},
		{
			name:        "key added deep in a long list, unquoted",
			old:         "validpgpkeys=(\n" + strings.Repeat("  "+keyA+"\n", 8) + ")",
			new:         "validpgpkeys=(\n" + strings.Repeat("  "+keyA+"\n", 6) + "  " + keyC + "\n" + strings.Repeat("  "+keyA+"\n", 2) + ")",
			wantChanged: 1, wantSummary: "PGP key added: " + keyC, wantLoc: "PKGBUILD:8",
		},
		{
			name: "keys replaced by a command substitution: pgp_removed",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=(\"$(cat keys.txt)\")",
			wantRemoved: 1,
		},
		{
			name: "new list unreadable: no pgp finding (array_unreadable reports it)",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=('" + keyA + "' ; )",
		},
		{
			name: "old list unreadable: its keys count as absent",
			old:  "validpgpkeys=('" + keyA + "' ; )", new: "validpgpkeys=('" + keyA + "')",
			wantChanged: 1, wantSummary: "PGP key added: " + keyA,
		},
		{
			name: "arch-suffixed list is its own label",
			old:  "validpgpkeys=('" + keyA + "')", new: "validpgpkeys=('" + keyA + "')\nvalidpgpkeys_x86_64=('" + keyB + "')",
			wantChanged: 1, wantSummary: "PGP key added: " + keyB, wantLoc: "PKGBUILD:2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runChecksumFiles(t, tt.old, tt.new)
			if n := countCategory(findings, CategoryPGPKeyChanged); n != tt.wantChanged {
				t.Fatalf("pgp_key_changed = %d, want %d: %v", n, tt.wantChanged, findings)
			}
			if n := countCategory(findings, CategoryPGPRemoved); n != tt.wantRemoved {
				t.Fatalf("pgp_removed = %d, want %d: %v", n, tt.wantRemoved, findings)
			}
			for _, f := range findings {
				if f.Category != CategoryPGPKeyChanged && f.Category != CategoryPGPRemoved {
					continue
				}
				if f.Category == CategoryPGPKeyChanged && f.Summary != tt.wantSummary {
					t.Errorf("Summary = %q, want %q", f.Summary, tt.wantSummary)
				}
				if tt.wantLoc != "" && f.Location != tt.wantLoc {
					t.Errorf("Location = %q, want %q", f.Location, tt.wantLoc)
				}
			}
		})
	}
}

// TestChecksumAnalyzerNotLoaded484: a PKGBUILD without its whole text is
// an error, never an empty result.
func TestChecksumAnalyzerNotLoaded484(t *testing.T) {
	diff := &git.GitDiff{Files: []git.FileDiff{{Path: "PKGBUILD", Kind: git.FileKindPKGBUILD,
		Patch: "@@ -1 +1 @@\n-validpgpkeys=('AAAAAAAAAAAAAAAA')\n+validpgpkeys=()\n"}}}
	if _, err := (&checksumAnalyzer{}).Run(context.Background(), diff); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("err = %v, want a not-loaded error", err)
	}
}
