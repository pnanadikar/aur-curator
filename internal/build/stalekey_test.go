package build

import (
	"strings"
	"testing"
)

const (
	keyID1 = "316C1FACD62D07D9"
	keyID2 = "0123456789ABCDEF"
	fpr1   = "EF6E286DDA85EA2A4BA7DE684E2C6E8793298290"
	fpr2   = "0123456789ABCDEF0123456789ABCDEF01234567"
)

func TestUnknownKeyWatcher(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{"makepkg's line", []string{"    tor-browser.tar.xz ... FAILED (unknown public key " + keyID1 + ")\n"}, []string{keyID1}},
		{"split across writes", []string{"x ... FAILED (unknown pub", "lic key " + keyID1[:5], keyID1[5:] + ")\n"}, []string{keyID1}},
		{"no final newline", []string{"FAILED (unknown public key " + keyID1 + ")"}, []string{keyID1}},
		{"two files, one key twice", []string{
			"a ... FAILED (unknown public key " + keyID1 + ")\n",
			"b ... FAILED (unknown public key " + keyID2 + ")\n",
			"c ... FAILED (unknown public key " + keyID1 + ")\n",
		}, []string{keyID1, keyID2}},
		{"after a progress meter", []string{"  50%\r 100%\rx ... FAILED (unknown public key " + keyID1 + ")\n"}, []string{keyID1}},
		{"bad signature", []string{"x ... FAILED (bad signature from public key " + keyID1 + ")\n"}, nil},
		{"revoked", []string{"x ... FAILED (public key " + keyID1 + " has been revoked)\n"}, nil},
		{"invalid public key", []string{"x ... FAILED (invalid public key " + fpr1 + ")\n"}, nil},
		{"lower case", []string{"FAILED (unknown public key " + strings.ToLower(keyID1) + ")\n"}, nil},
		{"short id", []string{"FAILED (unknown public key D62D07D9)\n"}, nil},
		{"translated", []string{"FEHLGESCHLAGEN (unbekannter öffentlicher Schlüssel " + keyID1 + ")\n"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w unknownKeyWatcher
			for _, s := range tt.writes {
				if n, err := w.Write([]byte(s)); err != nil || n != len(s) {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			if got := w.IDs(); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

// A line with no newline cannot grow the watcher without limit, and the
// message after it is still seen.
func TestUnknownKeyWatcherLongLine(t *testing.T) {
	var w unknownKeyWatcher
	for range 100 {
		_, _ = w.Write([]byte(strings.Repeat("#", 1000)))
		if len(w.line) > watchMaxLine {
			t.Fatalf("kept %d bytes", len(w.line))
		}
	}
	_, _ = w.Write([]byte("\nFAILED (unknown public key " + keyID1 + ")\n"))
	if got := w.IDs(); len(got) != 1 || got[0] != keyID1 {
		t.Errorf("IDs = %v", got)
	}
}

func TestStaleKeyHint(t *testing.T) {
	want := "unknown public key " + keyID1 + ": either your copy of " + fpr1 + " is out of date (upstream signs with a newer subkey; check the key, then refresh it: gpg --refresh-keys " + fpr1 + ", or the upstream's documented method), or the file is signed by a key not in validpgpkeys (check the source before trusting it)"
	if got := staleKeyHint([]string{keyID1}, []string{fpr1}); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	got := staleKeyHint([]string{keyID1, keyID2}, []string{fpr1, fpr2})
	for _, part := range []string{
		"unknown public key " + keyID1 + ", " + keyID2 + ":",
		"your copy of " + fpr1 + " or " + fpr2 + " is out of date",
		"gpg --refresh-keys " + fpr1 + " " + fpr2 + ",",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("hint lacks %q:\n%s", part, got)
		}
	}
}
