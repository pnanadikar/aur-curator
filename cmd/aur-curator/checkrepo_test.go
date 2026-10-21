package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// Tests for #378: check 3's repository integrity rows (workflow.md
// § Check (Consistency)). Helpers are prefixed rc to stay clear of other
// test helpers in package main.

type rcLastSuccess struct {
	builds map[domain.PackageName]*domain.Build
	errs   map[domain.PackageName]error
}

func (f rcLastSuccess) GetLastSuccess(pkg domain.PackageName) (*domain.Build, error) {
	if err := f.errs[pkg]; err != nil {
		return nil, err
	}
	if b, ok := f.builds[pkg]; ok {
		return b, nil
	}
	return nil, domain.ErrNotFound
}

type rcArtifacts struct {
	arts map[domain.ArtifactID]*domain.Artifact
	errs map[domain.ArtifactID]error
}

func (f rcArtifacts) Get(id domain.ArtifactID) (*domain.Artifact, error) {
	if err := f.errs[id]; err != nil {
		return nil, err
	}
	if a, ok := f.arts[id]; ok {
		return a, nil
	}
	return nil, domain.ErrNotFound
}

// rcFile is one package file: its name and content.
type rcFile struct{ name, content string }

func rcSum(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// rcDBEntry describes one database entry: the file it names and the SHA
// it records (normally rcSum of the file's content).
type rcDBEntry struct{ pkg, filename, sha string }

// rcWriteDB writes a repo-add style gzip database to repoDir.
func rcWriteDB(t *testing.T, repoDir string, entries []rcDBEntry) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		desc := fmt.Sprintf("%%FILENAME%%\n%s\n\n%%NAME%%\n%s\n\n%%VERSION%%\n1-1\n\n%%SHA256SUM%%\n%s\n\n", e.filename, e.pkg, e.sha)
		if err := tw.WriteHeader(&tar.Header{Name: e.pkg + "-1-1/desc", Mode: 0o644, Size: int64(len(desc)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("header: %v", err)
		}
		if _, err := tw.Write([]byte(desc)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "custom.db.tar.gz"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
}

const (
	rcMain  = "duplicati-bin-1-1-x86_64.pkg.tar.zst"
	rcDebug = "duplicati-bin-debug-1-1-x86_64.pkg.tar.zst"
)

func TestCheckRepository(t *testing.T) {
	t.Parallel()
	mainSum, debugSum := rcSum("main"), rcSum("debug")

	// published is duplicati-bin's last success with both artifacts.
	published := func(flag bool, mainChecksum string) (rcLastSuccess, rcArtifacts) {
		return rcLastSuccess{builds: map[domain.PackageName]*domain.Build{
				"duplicati-bin": {ID: "b1", ArtifactIDs: []domain.ArtifactID{"a1", "a2"}, Confirmed: true, Published: flag},
			}},
			rcArtifacts{arts: map[domain.ArtifactID]*domain.Artifact{
				"a1": {ID: "a1", Filename: rcMain, Checksum: mainChecksum},
				"a2": {ID: "a2", Filename: rcDebug, Checksum: debugSum},
			}}
	}
	bothInDB := []rcDBEntry{{"duplicati-bin", rcMain, mainSum}, {"duplicati-bin-debug", rcDebug, debugSum}}
	bothFiles := []rcFile{{rcMain, "main"}, {rcDebug, "debug"}}

	type want struct {
		fileProblems, notMarked, notInRepo int
		findings                           []string // each must appear in some finding, in order
	}
	cases := []struct {
		name  string
		db    []rcDBEntry // nil: no database file
		rawDB []byte      // if set, written as the database instead
		files []rcFile
		bs    func() (rcLastSuccess, rcArtifacts)
		want  want
	}{
		{
			name:  "consistent: published and in the repository",
			db:    bothInDB,
			files: bothFiles,
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
		},
		{
			name: "no database and nothing published",
			bs:   func() (rcLastSuccess, rcArtifacts) { return published(false, mainSum) },
		},
		{
			name: "no database but marked published (case A)",
			bs:   func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
			want: want{notInRepo: 1, findings: []string{`"duplicati-bin": marked Published, but 2 of 2`}},
		},
		{
			name:  "unreadable database skips everything",
			rawDB: []byte("garbage"),
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
			want:  want{findings: []string{"repository check skipped:"}},
		},
		{
			name:  "repository file missing (case C)",
			db:    bothInDB,
			files: []rcFile{{rcMain, "main"}},
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
			want:  want{fileProblems: 1, findings: []string{rcDebug + `" is missing`}},
		},
		{
			name:  "repository file overwritten, database not updated (case C)",
			db:    bothInDB,
			files: []rcFile{{rcMain, "rebuilt main"}, {rcDebug, "debug"}},
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
			want:  want{fileProblems: 1, findings: []string{rcMain + `" does not match`}},
		},
		{
			name: "database filename is not a plain name (case C)",
			db:   []rcDBEntry{{"evil", "../evil.pkg.tar.zst", rcSum("x")}},
			bs:   func() (rcLastSuccess, rcArtifacts) { return rcLastSuccess{}, rcArtifacts{} },
			want: want{fileProblems: 1, findings: []string{"is not a plain file name"}},
		},
		{
			name:  "partly registered, not marked (case B)",
			db:    []rcDBEntry{{"duplicati-bin", rcMain, mainSum}},
			files: []rcFile{{rcMain, "main"}},
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(false, mainSum) },
			want:  want{notMarked: 1, findings: []string{"1 of 2 artifact(s) are in the repository database"}},
		},
		{
			name:  "marked published, one artifact missing from the database (case A)",
			db:    []rcDBEntry{{"duplicati-bin", rcMain, mainSum}},
			files: []rcFile{{rcMain, "main"}},
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, mainSum) },
			want:  want{notInRepo: 1, findings: []string{"1 of 2 artifact(s) are not in"}},
		},
		{
			name:  "empty artifact checksum never matches",
			db:    bothInDB,
			files: bothFiles,
			bs:    func() (rcLastSuccess, rcArtifacts) { return published(true, "") },
			want:  want{notInRepo: 1, findings: []string{"1 of 2 artifact(s) are not in"}},
		},
		{
			name:  "built, not published, not in the repository (pfetch)",
			db:    bothInDB,
			files: bothFiles,
			bs: func() (rcLastSuccess, rcArtifacts) {
				ls, as := published(true, mainSum)
				ls.builds["pfetch"] = &domain.Build{ID: "b2", ArtifactIDs: []domain.ArtifactID{"p1"}}
				as.arts["p1"] = &domain.Artifact{ID: "p1", Filename: "pfetch-1-1-any.pkg.tar.zst", Checksum: rcSum("pfetch")}
				return ls, as
			},
		},
		{
			name:  "unreadable artifact skips that package",
			db:    bothInDB,
			files: bothFiles,
			bs: func() (rcLastSuccess, rcArtifacts) {
				ls, as := published(false, mainSum)
				as.errs = map[domain.ArtifactID]error{"a2": errors.New("decode: bad json")}
				return ls, as
			},
			want: want{findings: []string{`artifact "a2" unreadable, publication check skipped`}},
		},
		{
			name:  "unreadable last success skips that package",
			db:    bothInDB,
			files: bothFiles,
			bs: func() (rcLastSuccess, rcArtifacts) {
				ls, as := published(false, mainSum)
				ls.errs = map[domain.PackageName]error{"duplicati-bin": errors.New("decode: bad json")}
				return ls, as
			},
			want: want{findings: []string{"last-success record unreadable, publication check skipped"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			switch {
			case tc.rawDB != nil:
				if err := os.WriteFile(filepath.Join(repo, "custom.db.tar.gz"), tc.rawDB, 0o644); err != nil {
					t.Fatalf("write db: %v", err)
				}
			case tc.db != nil:
				rcWriteDB(t, repo, tc.db)
			}
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(repo, f.name), []byte(f.content), 0o644); err != nil {
					t.Fatalf("write %s: %v", f.name, err)
				}
			}
			bs, as := tc.bs()
			pkgs := []domain.PackageName{"duplicati-bin", "pfetch", "never-built"}
			logPath := filepath.Join(t.TempDir(), "activity.log")

			got := checkRepository(context.Background(), logPath, repo, "custom.db.tar.gz", bs, as, pkgs)

			if got.fileProblems != tc.want.fileProblems || got.notMarked != tc.want.notMarked || got.notInRepo != tc.want.notInRepo {
				t.Errorf("counts file=%d notMarked=%d notInRepo=%d, want %d/%d/%d; findings: %q",
					got.fileProblems, got.notMarked, got.notInRepo,
					tc.want.fileProblems, tc.want.notMarked, tc.want.notInRepo, got.findings)
			}
			if len(got.findings) != len(tc.want.findings) {
				t.Fatalf("findings = %q, want %d matching %q", got.findings, len(tc.want.findings), tc.want.findings)
			}
			for i, w := range tc.want.findings {
				if !strings.Contains(got.findings[i], w) {
					t.Errorf("finding %d = %q, want it to contain %q", i, got.findings[i], w)
				}
			}
		})
	}
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := fileSHA256(p)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	if want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Fatalf("fileSHA256 = %q, want %q", got, want)
	}
	if _, err := fileSHA256(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v, want os.ErrNotExist", err)
	}
}
