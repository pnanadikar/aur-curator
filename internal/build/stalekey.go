package build

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// unknownKeyRE matches makepkg's English report of a signature whose key
// is not in the keyring: "FAILED (unknown public key <keyid>)", the key
// ID being gpg's 16-digit long ID (libmakepkg verify_signature.sh, from
// gpg's ERRSIG … 9). In another language it does not match, and there is
// no hint (#514).
var unknownKeyRE = regexp.MustCompile(`unknown public key ([0-9A-F]{16})\)`)

// watchMaxLine bounds the unfinished line unknownKeyWatcher keeps: a line
// without a newline (a progress meter, redrawn with \r) cannot grow it
// without limit. The message is far shorter.
const watchMaxLine = 4096

// unknownKeyWatcher is an io.Writer for makepkg's stderr that collects
// the key IDs of "unknown public key" failures, in order, without
// duplicates. It never fails a write.
type unknownKeyWatcher struct {
	line []byte
	ids  []string
}

func (w *unknownKeyWatcher) Write(p []byte) (int, error) {
	w.line = append(w.line, p...)
	for {
		i := bytes.IndexByte(w.line, '\n')
		if i < 0 {
			break
		}
		w.scan(w.line[:i])
		w.line = w.line[i+1:]
	}
	if len(w.line) > watchMaxLine {
		w.line = w.line[len(w.line)-watchMaxLine/2:]
	}
	return len(p), nil
}

func (w *unknownKeyWatcher) scan(line []byte) {
	for _, m := range unknownKeyRE.FindAllSubmatch(line, -1) {
		if id := string(m[1]); !slices.Contains(w.ids, id) {
			w.ids = append(w.ids, id)
		}
	}
}

// IDs returns the key IDs seen, the unfinished last line included.
func (w *unknownKeyWatcher) IDs() []string {
	if len(w.line) > 0 {
		w.scan(w.line)
		w.line = nil
	}
	return w.ids
}

// staleKeyHint explains an "unknown public key" failure when every key
// in validpgpkeys (keys, never empty) is in the keyring, as exportKeys
// makes sure: either the local copy predates the subkey that signed, or
// the signer is not in validpgpkeys. It says how to refresh the key but
// never does: trust stays the user's (#514).
func staleKeyHint(ids, keys []string) string {
	return fmt.Sprintf("unknown public key %s: either your copy of %s is out of date (upstream signs with a newer subkey; check the key, then refresh it: gpg --refresh-keys %s, or the upstream's documented method), or the file is signed by a key not in validpgpkeys (check the source before trusting it)",
		strings.Join(ids, ", "), strings.Join(keys, " or "), strings.Join(keys, " "))
}
