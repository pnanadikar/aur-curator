package build

import (
	"strings"
)

// The build environment is an allowlist (#496, domain.md § Build →
// Build Environment); it replaced P1's denylist (#473). A variable
// reaches makepkg only if it is named here or pinned by buildEnv.
var (
	// passedNames are passed through when set on the host.
	passedNames = []string{"PATH", "USER", "LOGNAME", "LANG", "TZ", "TERM", "MAKEFLAGS", "PACKAGER"}
	// passedPrefixes are passed through when a set name starts with one.
	passedPrefixes = []string{"LC_"}
	// pinnedDests are set to the workspace: makepkg keeps environment
	// values over its configuration files, so nothing it produces or
	// downloads goes outside the one writable path.
	pinnedDests = []string{"PKGDEST", "SRCDEST", "SRCPKGDEST", "LOGDEST", "BUILDDIR"}
	// pinnedFlags are set as they are. FAKEROOTDONTTRYCHOWN: in the user
	// namespace the real chown to an unmapped uid 0 fails with EINVAL,
	// which fakeroot does not ignore (only EPERM), so a package() that
	// chowns to root failed the build; with it, fakeroot only records the
	// ownership (step 7's TestSandboxFixtureChownRoot, #496).
	pinnedFlags = []string{"FAKEROOTDONTTRYCHOWN=1"}
)

// buildEnv returns the environment of a build in workspace with home
// as $HOME: the allowlisted entries of environ, in environ's order,
// then HOME and the pinned destinations. names lists every variable
// set, in the same order, never their values.
func buildEnv(environ []string, home, workspace string) (env []string, names []string) {
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !passed(name) || containsName(names, name) {
			continue
		}
		env = append(env, entry)
		names = append(names, name)
	}
	env = append(env, "HOME="+home)
	names = append(names, "HOME")
	for _, d := range pinnedDests {
		env = append(env, d+"="+workspace)
		names = append(names, d)
	}
	for _, f := range pinnedFlags {
		name, _, _ := strings.Cut(f, "=")
		env = append(env, f)
		names = append(names, name)
	}
	return env, names
}

// envLogLine is the build log's first line: the names of the variables
// the build gets.
func envLogLine(names []string) string {
	return "aur-curator: build environment: " + strings.Join(names, ", ")
}

// passed reports whether a host variable is passed through.
func passed(name string) bool {
	for _, n := range passedNames {
		if name == n {
			return true
		}
	}
	for _, p := range passedPrefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return true
		}
	}
	return false
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
