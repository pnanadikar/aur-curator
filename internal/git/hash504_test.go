package git

import (
	"context"
	"strings"
	"testing"
)

const (
	sha1Hash   = "0123456789abcdef0123456789abcdef01234567"
	sha256Hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// notHashes are commits IsMerged and FileAtCommit refuse without running
// git (#504).
var notHashes = []string{
	"",
	"-x",
	"--output=/tmp/x",
	"HEAD",
	"master",
	"0123456", // short
	"0123456789ABCDEF0123456789ABCDEF01234567",  // uppercase
	"0123456789abcdef0123456789abcdef0123456g",  // not hex
	"0123456789abcdef0123456789abcdef012345678", // 41
	" 0123456789abcdef0123456789abcdef01234567", // space
}

func TestCheckFullHash(t *testing.T) {
	for _, c := range []string{sha1Hash, sha256Hash} {
		if err := checkFullHash(c); err != nil {
			t.Errorf("checkFullHash(%q) = %v", c, err)
		}
	}
	for _, c := range notHashes {
		if err := checkFullHash(c); err == nil {
			t.Errorf("checkFullHash(%q) = nil, want an error", c)
		}
	}
}

func TestIsMergedRefusesNonHash504(t *testing.T) {
	for _, c := range notHashes {
		r := &fakeRunner{}
		p := &provider{runner: r, catalog: &fakeCatalog{repoPath: "/tmp/repo"}}
		merged, err := p.IsMerged(context.Background(), "yay", c)
		if err == nil || !strings.Contains(err.Error(), "not a full commit hash") {
			t.Errorf("IsMerged(%q) err = %v, want not a full commit hash", c, err)
		}
		if merged {
			t.Errorf("IsMerged(%q) = true", c)
		}
		if len(r.calls) != 0 {
			t.Errorf("IsMerged(%q) ran git: %v", c, r.calls)
		}
	}
}

func TestFileAtCommitRefusesNonHash504(t *testing.T) {
	for _, c := range notHashes {
		r := &fakeRunner{}
		p := &provider{runner: r, catalog: &fakeCatalog{repoPath: "/tmp/repo"}}
		if _, err := p.FileAtCommit(context.Background(), "yay", c, "PKGBUILD"); err == nil || !strings.Contains(err.Error(), "not a full commit hash") {
			t.Errorf("FileAtCommit(%q) err = %v, want not a full commit hash", c, err)
		}
		if len(r.calls) != 0 {
			t.Errorf("FileAtCommit(%q) ran git: %v", c, r.calls)
		}
	}
}

// A full hash, SHA-1 or SHA-256, reaches git unchanged.
func TestFullHashReachesGit504(t *testing.T) {
	for _, c := range []string{sha1Hash, sha256Hash} {
		r := &fakeRunner{output: "x"}
		p := &provider{runner: r, catalog: &fakeCatalog{repoPath: "/tmp/repo"}}
		if _, err := p.IsMerged(context.Background(), "yay", c); err != nil {
			t.Errorf("IsMerged(%q) = %v", c, err)
		}
		if _, err := p.FileAtCommit(context.Background(), "yay", c, "PKGBUILD"); err != nil {
			t.Errorf("FileAtCommit(%q) = %v", c, err)
		}
		want := [][]string{{"merge-base", "--is-ancestor", c, "master"}, {"show", c + ":PKGBUILD"}}
		if len(r.calls) != 2 || strings.Join(r.calls[0].Args, " ") != strings.Join(want[0], " ") || strings.Join(r.calls[1].Args, " ") != strings.Join(want[1], " ") {
			t.Errorf("git calls = %v, want %v", r.calls, want)
		}
	}
}
