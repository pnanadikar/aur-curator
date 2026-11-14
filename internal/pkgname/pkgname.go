// Package pkgname checks package names against the AUR's rule (#504).
// Every name that enters aur-curator — a CLI argument, a directory in the
// workspace, a store key — is checked here before it reaches a path, git or
// the network.
package pkgname

import (
	"fmt"
)

// Rule describes the names Check accepts, for error messages and docs.
const Rule = "lowercase letters, digits and . + _ -, starting with a letter or digit"

// Check returns an error naming the rule unless name is a valid AUR
// package name: aurweb's ^[a-z0-9][a-z0-9.+_-]*$. It is stricter than
// makepkg's rule (no @, no leading . _ or +), which is fine: aur-curator
// curates AUR packages only. Every valid name is safe as a single path
// element and as a command-line argument: it has no separator, is not
// "." or "..", and cannot start with "-".
func Check(name string) error {
	if name == "" {
		return fmt.Errorf("invalid package name: empty; the AUR allows %s", Rule)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '.' || r == '+' || r == '_' || r == '-'):
		default:
			return fmt.Errorf("invalid package name %q: the AUR allows %s", name, Rule)
		}
	}
	return nil
}
