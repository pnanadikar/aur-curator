package repodb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realDesc is duplicati-bin's desc as repo-add wrote it on 2026-10-16,
// including an empty key (%REPLACES%).
const realDesc = `%FILENAME%
duplicati-bin-2.4.0.1-2-x86_64.pkg.tar.zst

%NAME%
duplicati-bin

%BASE%
duplicati-bin

%VERSION%
2.4.0.1-2

%DESC%
A free backup client that securely stores encrypted, incremental, compressed backups on cloud storage services and remote file servers

%CSIZE%
85672361

%ISIZE%
222554968

%SHA256SUM%
269e38419027acea7cdc07e53fb70cf8a7aace201705a5719427d52db4aca01e

%URL%
http://duplicati.com

%LICENSE%
MIT

%ARCH%
x86_64

%BUILDDATE%
1792158807

%PACKAGER%
Unknown Packager

%REPLACES%

`

// debugDesc is a second entry, as repo-add writes for a -debug split, with
// a multi-value key (%LICENSE%).
const debugDesc = `%FILENAME%
duplicati-bin-debug-2.4.0.1-2-x86_64.pkg.tar.zst

%NAME%
duplicati-bin-debug

%VERSION%
2.4.0.1-2

%LICENSE%
MIT
Apache

%SHA256SUM%
ABCDEF0000000000000000000000000000000000000000000000000000000000

`

// tarEntry is one file or directory in a test archive.
type tarEntry struct {
	name string
	body string
	dir  bool
}

// makeDB returns a gzip-compressed tar archive holding entries, laid out
// the way repo-add does: a directory per package with a desc file in it.
func makeDB(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.dir {
			hdr = &tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// writeDB writes data to a database file in a fresh directory.
func writeDB(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "custom.db.tar.gz")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	return p
}

// twoPackages is the real repository's layout: main package plus -debug.
func twoPackages() []tarEntry {
	return []tarEntry{
		{name: "duplicati-bin-2.4.0.1-2/", dir: true},
		{name: "duplicati-bin-2.4.0.1-2/desc", body: realDesc},
		{name: "duplicati-bin-debug-2.4.0.1-2/", dir: true},
		{name: "duplicati-bin-debug-2.4.0.1-2/desc", body: debugDesc},
	}
}

func TestReadRealLayout(t *testing.T) {
	t.Parallel()
	snap, err := Read(writeDB(t, makeDB(t, twoPackages())))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []Entry{
		{
			Name:     "duplicati-bin",
			Version:  "2.4.0.1-2",
			Filename: "duplicati-bin-2.4.0.1-2-x86_64.pkg.tar.zst",
			SHA256:   "269e38419027acea7cdc07e53fb70cf8a7aace201705a5719427d52db4aca01e",
		},
		{
			Name:     "duplicati-bin-debug",
			Version:  "2.4.0.1-2",
			Filename: "duplicati-bin-debug-2.4.0.1-2-x86_64.pkg.tar.zst",
			SHA256:   "abcdef0000000000000000000000000000000000000000000000000000000000",
		},
	}
	if len(snap.Entries) != len(want) {
		t.Fatalf("got %d entries %+v, want %d", len(snap.Entries), snap.Entries, len(want))
	}
	for i := range want {
		if snap.Entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, snap.Entries[i], want[i])
		}
	}
}

func TestReadMissingDatabaseIsEmpty(t *testing.T) {
	t.Parallel()
	snap, err := Read(filepath.Join(t.TempDir(), "custom.db.tar.gz"))
	if err != nil {
		t.Fatalf("Read: %v, want no error for a missing database", err)
	}
	if len(snap.Entries) != 0 {
		t.Fatalf("entries = %+v, want none", snap.Entries)
	}
}

func TestReadEmptyArchive(t *testing.T) {
	t.Parallel()
	snap, err := Read(writeDB(t, makeDB(t, nil)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(snap.Entries) != 0 {
		t.Fatalf("entries = %+v, want none", snap.Entries)
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()
	good := makeDB(t, twoPackages())
	noSHA := strings.Replace(realDesc, "%SHA256SUM%\n269e38419027acea7cdc07e53fb70cf8a7aace201705a5719427d52db4aca01e\n", "", 1)
	emptyName := strings.Replace(realDesc, "%NAME%\nduplicati-bin\n", "%NAME%\n\n", 1)

	cases := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"not gzip", []byte("this is not a database"), "not gzip-compressed"},
		{"empty file", []byte{}, "not gzip-compressed"},
		{"truncated", good[:len(good)/2], "custom.db.tar.gz"},
		{"desc without SHA256SUM", makeDB(t, []tarEntry{{name: "p-1-1/desc", body: noSHA}}), "%SHA256SUM%"},
		{"desc with empty NAME", makeDB(t, []tarEntry{{name: "p-1-1/desc", body: emptyName}}), "%NAME%"},
		// One good entry followed by a bad one: no partial result.
		{"bad entry after good", makeDB(t, append(twoPackages(), tarEntry{name: "q-1-1/desc", body: noSHA})), "q-1-1/desc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap, err := Read(writeDB(t, tc.data))
			if err == nil {
				t.Fatalf("Read = %+v with no error, want an error containing %q", snap, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
			if len(snap.Entries) != 0 {
				t.Errorf("entries = %+v on error, want none", snap.Entries)
			}
		})
	}
}

func TestReadUnreadablePathIsError(t *testing.T) {
	t.Parallel()
	// A directory where the database should be: not "missing", so an error.
	dir := filepath.Join(t.TempDir(), "custom.db.tar.gz")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("Read of a directory returned no error")
	}
}

func TestParseDesc(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		desc    string
		want    Entry
		wantErr string
	}{
		{
			name: "a required key with several values takes the first",
			desc: "%NAME%\nfoo\n\n%VERSION%\n1-1\n\n%FILENAME%\nfoo-1-1-any.pkg.tar.zst\n\n%SHA256SUM%\naa\nbb\n\n",
			want: Entry{Name: "foo", Version: "1-1", Filename: "foo-1-1-any.pkg.tar.zst", SHA256: "aa"},
		},
		{
			name: "CRLF line endings",
			desc: "%NAME%\r\nfoo\r\n\r\n%VERSION%\r\n1-1\r\n\r\n%FILENAME%\r\nf\r\n\r\n%SHA256SUM%\r\nAA\r\n",
			want: Entry{Name: "foo", Version: "1-1", Filename: "f", SHA256: "aa"},
		},
		{
			name:    "all required keys missing",
			desc:    "%DESC%\nnothing useful\n",
			wantErr: "missing %NAME%, %VERSION%, %FILENAME%, %SHA256SUM%",
		},
		{
			name:    "a value line with no key before it is ignored",
			desc:    "stray\n\n%NAME%\nfoo\n",
			wantErr: "missing %VERSION%, %FILENAME%, %SHA256SUM%",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDesc([]byte(tc.desc))
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("parseDesc error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDesc: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseDesc = %+v, want %+v", got, tc.want)
			}
		})
	}
}
