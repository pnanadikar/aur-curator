// Package inspect reads what a built package installs with special power
// from its archive's listing (#525, P7; domain.md § Build → Confirmed).
// It does no I/O: the caller runs bsdtar and passes its mtree output.
package inspect

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Entry is one archive entry, as bsdtar's mtree listing gives it.
type Entry struct {
	// Path is the entry's name with the leading "./" removed,
	// unescaped.
	Path string
	// Type is mtree's type: file, dir, link, char, block, fifo, socket.
	Type string
	// Mode holds the permission and setuid/setgid/sticky bits.
	Mode uint32
	// UID and GID are the numeric owner.
	UID, GID int
	// Uname and Gname are the owner's names, when the archive has them.
	Uname, Gname string
	// Link is a symlink's target, unescaped.
	Link string
}

// MtreeArgs are bsdtar's arguments that list an archive's own entries
// as mtree, before the "@<file>" argument: every field ParseMtree needs,
// labelled, names escaped.
var MtreeArgs = []string{"-cf", "-", "--format=mtree", "--options=!all,type,mode,uid,gid,uname,gname,link"}

// ParseMtree reads bsdtar's mtree output. "/set" and "/unset" lines set
// and clear defaults for the lines after them; comments and blank lines
// are skipped. An entry without type, mode, uid or gid is an error: the
// classification needs all four.
func ParseMtree(r io.Reader) ([]Entry, error) {
	var out []Entry
	defaults := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "/set":
			for _, kv := range fields[1:] {
				k, v, _ := strings.Cut(kv, "=")
				defaults[k] = v
			}
			continue
		case "/unset":
			for _, k := range fields[1:] {
				if k == "all" {
					defaults = map[string]string{}
				}
				delete(defaults, k)
			}
			continue
		}
		kv := make(map[string]string, len(defaults)+len(fields))
		for k, v := range defaults {
			kv[k] = v
		}
		for _, f := range fields[1:] {
			k, v, _ := strings.Cut(f, "=")
			kv[k] = v
		}
		e, err := entryFrom(fields[0], kv)
		if err != nil {
			return nil, fmt.Errorf("mtree line %d: %w", n, err)
		}
		if e.Path == "" {
			continue // "." itself
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read mtree: %w", err)
	}
	return out, nil
}

func entryFrom(name string, kv map[string]string) (Entry, error) {
	path, err := unescape(name)
	if err != nil {
		return Entry{}, err
	}
	switch {
	case path == ".":
		path = ""
	case strings.HasPrefix(path, "./"):
		path = path[2:]
	}
	e := Entry{Path: path, Type: kv["type"], Uname: kv["uname"], Gname: kv["gname"]}
	if e.Type == "" {
		return Entry{}, fmt.Errorf("%q: no type", path)
	}
	mode, ok := kv["mode"]
	if !ok {
		return Entry{}, fmt.Errorf("%q: no mode", path)
	}
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return Entry{}, fmt.Errorf("%q: mode %q: %w", path, mode, err)
	}
	e.Mode = uint32(m)
	if e.UID, err = intField(kv, "uid"); err != nil {
		return Entry{}, fmt.Errorf("%q: %w", path, err)
	}
	if e.GID, err = intField(kv, "gid"); err != nil {
		return Entry{}, fmt.Errorf("%q: %w", path, err)
	}
	if l, ok := kv["link"]; ok {
		if e.Link, err = unescape(l); err != nil {
			return Entry{}, err
		}
	}
	for _, f := range []*string{&e.Uname, &e.Gname} {
		if *f, err = unescape(*f); err != nil {
			return Entry{}, err
		}
	}
	return e, nil
}

func intField(kv map[string]string, k string) (int, error) {
	v, ok := kv[k]
	if !ok {
		return 0, fmt.Errorf("no %s", k)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", k, v, err)
	}
	return n, nil
}

// unescape decodes mtree's escapes: a backslash and three octal digits
// is that byte (bsdtar writes every space, newline, backslash and
// non-printing byte this way). A backslash followed by anything else is
// an error, so a listing aur-curator cannot read is reported, not
// guessed at.
func unescape(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) {
			return "", fmt.Errorf("bad escape in %q", s)
		}
		v, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			return "", fmt.Errorf("bad escape in %q", s)
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String(), nil
}
