package build

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	fprA = "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	fprB = "0123456789ABCDEF0123456789ABCDEF01234567"
)

func TestValidPGPKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		srcinfo string
		want    []string
		err     string
	}{
		{"none", "pkgbase = x\n\tpkgver = 1\n", nil, ""},
		{"two, tab-indented as makepkg writes", "pkgbase = x\n\tvalidpgpkeys = " + fprA + "\n\tvalidpgpkeys = " + fprB + "\n", []string{fprA, fprB}, ""},
		{"lower case normalised, duplicate dropped", "\tvalidpgpkeys = " + strings.ToLower(fprA) + "\n\tvalidpgpkeys = " + fprA + "\n", []string{fprA}, ""},
		{"other keys ignored", "\tsource = validpgpkeys = " + fprA + "\n\tpgpvalidkeys = " + fprB + "\n", nil, ""},
		{"short key id refused", "\tvalidpgpkeys = 89ABCDEF\n", nil, "not a full fingerprint: 89ABCDEF"},
		{"option injection refused", "\tvalidpgpkeys = --output=/tmp/x\n", nil, "not a full fingerprint: --output=/tmp/x"},
		{"bad among good refused", "\tvalidpgpkeys = " + fprA + "\n\tvalidpgpkeys = nope\n", nil, "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validPGPKeys(tc.srcinfo)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// gnupgHome makes a throwaway GnuPG home under /tmp (agent socket paths
// must stay short), sets GNUPGHOME to it, and stops its agents at the
// end. keyboxd makes it use keyboxd, as Dan's does.
func gnupgHome(t *testing.T, keyboxd bool) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "g")
	if err != nil {
		t.Fatal(err)
	}
	if keyboxd {
		if err := os.WriteFile(filepath.Join(d, "common.conf"), []byte("use-keyboxd\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GNUPGHOME", d)
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", d, "--kill", "all").Run()
		_ = os.RemoveAll(d)
	})
	return d
}

// genKey creates a key with no passphrase in the current GNUPGHOME and
// returns its fingerprint.
func genKey(t *testing.T, uid string) string {
	t.Helper()
	if out, err := exec.Command("gpg", "--batch", "--passphrase", "", "--quick-gen-key", uid, "ed25519", "sign", "never").CombinedOutput(); err != nil {
		t.Fatalf("gen key: %v: %s", err, out)
	}
	out, err := exec.Command("gpg", "--batch", "--with-colons", "--list-keys", uid).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Split(line, ":"); len(f) > 9 && f[0] == "fpr" {
			return f[9]
		}
	}
	t.Fatalf("no fingerprint for %s", uid)
	return ""
}

// listIn lists the primary fingerprints in a home directory.
func listIn(t *testing.T, home string) []string {
	t.Helper()
	out, err := exec.Command("gpg", "--batch", "--homedir", home, "--with-colons", "--list-keys").Output()
	if err != nil {
		t.Fatalf("list %s: %v", home, err)
	}
	var fprs []string
	prev := ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 9 && f[0] == "fpr" && prev == "pub" {
			fprs = append(fprs, f[9])
		}
		if len(f) > 0 {
			prev = f[0]
		}
	}
	return fprs
}

func TestExportKeys(t *testing.T) {
	for _, keyboxd := range []bool{false, true} {
		name := "classic"
		if keyboxd {
			name = "keyboxd"
		}
		t.Run(name, func(t *testing.T) {
			src := gnupgHome(t, keyboxd)
			want := genKey(t, "Wanted <wanted@example.org>")
			other := genKey(t, "Other <other@example.org>")
			if keyboxd {
				// The source really is keyboxd: no classic keybox to copy.
				if _, err := os.Stat(filepath.Join(src, "pubring.kbx")); !os.IsNotExist(err) {
					t.Fatal("the keyboxd home has a pubring.kbx; keyboxd was not used")
				}
				if _, err := os.Stat(filepath.Join(src, "public-keys.d")); err != nil {
					t.Fatalf("no public-keys.d in the keyboxd home: %v", err)
				}
			}

			dest := filepath.Join(t.TempDir(), "keyring")
			kbx, err := exportKeys(context.Background(), []string{want}, dest)
			if err != nil {
				t.Fatal(err)
			}
			if kbx != filepath.Join(dest, "pubring.kbx") {
				t.Fatalf("keybox %s", kbx)
			}
			if _, err := os.Stat(filepath.Join(dest, "common.conf")); !os.IsNotExist(err) {
				t.Fatal("the new home has a common.conf: it could ask for keyboxd")
			}
			// Only the named key: the build sees none of the user's others.
			if got := listIn(t, dest); !slices.Equal(got, []string{want}) {
				t.Fatalf("keybox holds %q, want only %s (not %s)", got, want, other)
			}
		})
	}
}

func TestExportKeysNone(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "keyring")
	kbx, err := exportKeys(context.Background(), nil, dest)
	if err != nil || kbx != "" {
		t.Fatalf("%q, %v", kbx, err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("a keyring directory was created for no keys")
	}
}

func TestExportKeysMissing(t *testing.T) {
	gnupgHome(t, false)
	have := genKey(t, "Have <have@example.org>")
	dest := filepath.Join(t.TempDir(), "keyring")
	_, err := exportKeys(context.Background(), []string{have, fprA}, dest)
	if err == nil || !strings.Contains(err.Error(), fprA) || strings.Contains(err.Error(), have) {
		t.Fatalf("err = %v; want it to name only %s", err, fprA)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("keyring directory created though a key was missing")
	}
}
