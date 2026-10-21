// Package repodb reads a local pacman repository database: the
// <name>.db.tar.gz archive that repo-add maintains (#378). It is the one
// parser of that database; callers never read the archive themselves.
package repodb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// Entry is one package registered in the repository database, taken from
// its desc file.
type Entry struct {
	Name     string // %NAME%
	Version  string // %VERSION% (pkgver-pkgrel, with epoch if any)
	Filename string // %FILENAME%, relative to the repository directory
	SHA256   string // %SHA256SUM%, lowercase hex
}

// Snapshot is the full content of a repository database at the time it
// was read. Entries are in archive order.
type Snapshot struct {
	Entries []Entry
}

// gzipMagic is the first two bytes of every gzip stream.
var gzipMagic = []byte{0x1f, 0x8b}

// Read reads the repository database at dbPath.
//
// A database that does not exist means nothing has been published yet:
// Read returns an empty Snapshot and no error (repo-add creates the
// database on the first publish).
//
// Any other problem — the file cannot be opened or read, it is not
// gzip-compressed, the archive is truncated or corrupt, or a desc file
// lacks %NAME%, %VERSION%, %FILENAME% or %SHA256SUM% — is returned as an
// error together with an empty Snapshot. Read never returns partial
// results: a caller either sees the whole database or an error.
func Read(dbPath string) (Snapshot, error) {
	f, err := os.Open(dbPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("open repository database: %w", err)
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewReader(f)
	magic, err := br.Peek(len(gzipMagic))
	if err != nil && !errors.Is(err, io.EOF) {
		return Snapshot{}, fmt.Errorf("read repository database %s: %w", dbPath, err)
	}
	if !bytes.Equal(magic, gzipMagic) {
		return Snapshot{}, fmt.Errorf("repository database %s is not gzip-compressed; only .db.tar.gz databases are supported", dbPath)
	}

	gr, err := gzip.NewReader(br)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decompress repository database %s: %w", dbPath, err)
	}
	defer func() { _ = gr.Close() }()

	entries, err := readEntries(tar.NewReader(gr))
	if err != nil {
		return Snapshot{}, fmt.Errorf("repository database %s: %w", dbPath, err)
	}
	return Snapshot{Entries: entries}, nil
}

// readEntries walks the archive and decodes every desc file. Entries other
// than desc files (directories, any other file) carry no package metadata
// and are skipped.
func readEntries(tr *tar.Reader) ([]Entry, error) {
	var entries []Entry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if !hdr.FileInfo().Mode().IsRegular() || path.Base(hdr.Name) != "desc" {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		e, err := parseDesc(content)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", hdr.Name, err)
		}
		entries = append(entries, e)
	}
}

// parseDesc decodes a desc file. The format is a sequence of blocks: a
// %KEY% line, zero or more value lines, and a blank line. Only the first
// value of the four keys Entry needs is used; every other key is ignored.
// A missing or empty required key is an error.
func parseDesc(content []byte) (Entry, error) {
	values := make(map[string]string)
	key := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			key = ""
		case len(line) > 2 && strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%"):
			key = line
		case key != "":
			if _, seen := values[key]; !seen {
				values[key] = line
			}
		}
	}

	e := Entry{
		Name:     values["%NAME%"],
		Version:  values["%VERSION%"],
		Filename: values["%FILENAME%"],
		SHA256:   strings.ToLower(values["%SHA256SUM%"]),
	}
	var missing []string
	for _, f := range []struct{ key, val string }{
		{"%NAME%", e.Name},
		{"%VERSION%", e.Version},
		{"%FILENAME%", e.Filename},
		{"%SHA256SUM%", e.SHA256},
	} {
		if f.val == "" {
			missing = append(missing, f.key)
		}
	}
	if len(missing) > 0 {
		return Entry{}, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return e, nil
}
