package build

import (
	"slices"
	"strings"
)

// The build environment denylist (#473, threat-model.md § Plan P1).
// makepkg gets the user's environment minus these variables. Removing a
// variable removes a secret value (the Gitea token) or the address of a
// socket; it does not hide the socket itself. Containment is P5, which
// replaces this denylist with an allowlist.
var (
	// deniedNames are removed when the name matches exactly.
	deniedNames = []string{
		"SSH_AUTH_SOCK",
		"SSH_AGENT_PID",
		"DBUS_SESSION_BUS_ADDRESS",
		"SSH_ASKPASS",
		"SUDO_ASKPASS",
		"GIT_ASKPASS",
		"DISPLAY",
		"WAYLAND_DISPLAY",
		"XAUTHORITY",
	}
	// deniedPrefixes are removed when the name starts with one of them.
	deniedPrefixes = []string{
		"AUR_CURATOR_",
	}
	// deniedSuffixes are removed when the name ends with one of them:
	// other tools' credentials (GITHUB_TOKEN, GH_TOKEN, ...).
	deniedSuffixes = []string{
		"_TOKEN",
		"_SECRET",
		"_PASSWORD",
	}
)

// cleanEnv returns environ without the denylisted variables, and the
// sorted, de-duplicated names it removed. Kept entries stay in order.
// An entry without "=" is kept unchanged. removed holds names only,
// never values.
func cleanEnv(environ []string) (kept []string, removed []string) {
	kept = make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if ok && denied(name) {
			removed = append(removed, name)
			continue
		}
		kept = append(kept, entry)
	}
	slices.Sort(removed)
	return kept, slices.Compact(removed)
}

// envLogLine is the build log's first line: the names cleanEnv removed,
// or "none".
func envLogLine(removed []string) string {
	names := "none"
	if len(removed) > 0 {
		names = strings.Join(removed, ", ")
	}
	return "aur-curator: removed from build environment: " + names
}

// denied reports whether name is on the build environment denylist.
func denied(name string) bool {
	if slices.Contains(deniedNames, name) {
		return true
	}
	for _, p := range deniedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, s := range deniedSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}
