package build

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pramod/aur-curator/internal/sanitize"
)

// fingerprintRE is a full OpenPGP v4 fingerprint, the only form makepkg
// accepts in validpgpkeys.
var fingerprintRE = regexp.MustCompile(`^[0-9A-F]{40}$`)

// validPGPKeys reads validpgpkeys from makepkg --printsrcinfo output:
// "key = value" lines, never shell. Each value must be a full
// fingerprint; anything else is an error naming it, so nothing a
// PKGBUILD writes reaches gpg's command line unchecked (#496).
// Duplicates are dropped; the order is the file's.
func validPGPKeys(srcinfo string) ([]string, error) {
	var keys, bad []string
	s := bufio.NewScanner(strings.NewReader(srcinfo))
	for s.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(s.Text()), " = ")
		if !ok || k != "validpgpkeys" {
			continue
		}
		fpr := strings.ToUpper(strings.TrimSpace(v))
		switch {
		case !fingerprintRE.MatchString(fpr):
			bad = append(bad, sanitize.ForMessage(v))
		case !slices.Contains(keys, fpr):
			keys = append(keys, fpr)
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("read .SRCINFO: %w", err)
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("validpgpkeys: not a full fingerprint: %s", strings.Join(bad, ", "))
	}
	return keys, nil
}

// keyboxName is the classic keybox gpg writes into a fresh home
// directory, and the name it reads from $HOME/.gnupg inside the
// sandbox.
const keyboxName = "pubring.kbx"

// exportKeys copies the keys named by fprs from the user's GnuPG (on
// the host, whatever it uses: a classic keybox or keyboxd) into a new
// home directory dir, and returns the path of the keybox written
// there. No keys means no keybox (""). A key the user's GnuPG does not
// have fails the export, naming it: a miss is loud, never a build that
// fails later at signature checking with a vaguer message.
//
// dir must not exist yet; it is created 0700, as gpg wants.
func exportKeys(ctx context.Context, fprs []string, dir string) (string, error) {
	if len(fprs) == 0 {
		return "", nil
	}

	var missing []string
	for _, f := range fprs {
		cmd := exec.CommandContext(ctx, "gpg", "--batch", "--with-colons", "--list-keys", "--", f) //nolint:gosec // gpg itself; f is a checked fingerprint
		if err := cmd.Run(); err != nil {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("validpgpkeys: not in your GnuPG keyring: %s (import them with gpg --recv-keys, after checking them)", strings.Join(missing, ", "))
	}

	var exported, stderr bytes.Buffer
	exp := exec.CommandContext(ctx, "gpg", append([]string{"--batch", "--export", "--"}, fprs...)...) //nolint:gosec // gpg itself; checked fingerprints
	exp.Stdout, exp.Stderr = &exported, &stderr
	if err := exp.Run(); err != nil {
		return "", fmt.Errorf("gpg --export: %w: %s", err, sanitize.ForMessage(stderr.String()))
	}
	if exported.Len() == 0 {
		return "", fmt.Errorf("gpg --export wrote nothing for %s", strings.Join(fprs, ", "))
	}

	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("create keyring directory: %w", err)
	}
	stderr.Reset()
	imp := exec.CommandContext(ctx, "gpg", "--batch", "--homedir", dir, "--import") //nolint:gosec // gpg itself; dir is the build's own directory
	imp.Stdin, imp.Stderr = &exported, &stderr
	importErr := imp.Run()
	// The import may start an agent for the new home; stop it either way.
	_ = exec.CommandContext(ctx, "gpgconf", "--homedir", dir, "--kill", "all").Run() //nolint:gosec // gpgconf itself
	if importErr != nil {
		return "", fmt.Errorf("gpg --import into %s: %w: %s", dir, importErr, sanitize.ForMessage(stderr.String()))
	}

	kbx := filepath.Join(dir, keyboxName)
	if _, err := os.Stat(kbx); err != nil {
		return "", fmt.Errorf("no keybox after import: %w", err)
	}
	return kbx, nil
}
