package build

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildEnv(t *testing.T) {
	const home, ws = "/home/dan", "/home/dan/aur/.aur-curator/builds/b1/work/src"
	host := []string{
		"PATH=/usr/bin:/home/dan/.cargo/bin",
		"AUR_CURATOR_GITEA_TOKEN=tok",
		"SSH_AUTH_SOCK=/run/user/1000/ssh",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
		"DISPLAY=:0",
		"GITHUB_TOKEN=gh",
		"GNUPGHOME=/home/dan/.gnupg",
		"MAKEPKG_CONF=/etc/other.conf",
		"PKGDEST=/srv/pkgs", // pinned below, never passed
		"HOME=/root",        // pinned below, never passed
		"LANG=en_IN.UTF-8",
		"LC_ALL=C",
		"LC_=x", // not a locale variable
		"TERM=xterm",
		"MAKEFLAGS=-j8",
		"PACKAGER=Dan <d@example.org>",
		"USER=dan",
		"PATH=/second", // a duplicate: the first wins, as getenv does
		"NOEQUALS",
	}
	env, names := buildEnv(host, home, ws)

	want := []string{
		"PATH=/usr/bin:/home/dan/.cargo/bin",
		"LANG=en_IN.UTF-8",
		"LC_ALL=C",
		"TERM=xterm",
		"MAKEFLAGS=-j8",
		"PACKAGER=Dan <d@example.org>",
		"USER=dan",
		"HOME=" + home,
		"PKGDEST=" + ws, "SRCDEST=" + ws, "SRCPKGDEST=" + ws, "LOGDEST=" + ws, "BUILDDIR=" + ws,
		"FAKEROOTDONTTRYCHOWN=1",
	}
	if !slices.Equal(env, want) {
		t.Fatalf("env\n got %q\nwant %q", env, want)
	}
	wantNames := []string{"PATH", "LANG", "LC_ALL", "TERM", "MAKEFLAGS", "PACKAGER", "USER", "HOME", "PKGDEST", "SRCDEST", "SRCPKGDEST", "LOGDEST", "BUILDDIR", "FAKEROOTDONTTRYCHOWN"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("names %q", names)
	}
}

func TestBuildEnvMinimal(t *testing.T) {
	env, names := buildEnv(nil, "/h", "/w")
	if len(env) != 7 || env[0] != "HOME=/h" || names[0] != "HOME" {
		t.Fatalf("%q %q", env, names)
	}
}

func TestEnvLogLine(t *testing.T) {
	got := envLogLine([]string{"PATH", "HOME"})
	if got != "aur-curator: build environment: PATH, HOME" {
		t.Fatalf("%q", got)
	}
}

// The log line carries names only: nothing after a name can be a value.
func TestEnvLogLineHasNoValues(t *testing.T) {
	_, names := buildEnv([]string{"PATH=/secret/path", "PACKAGER=Dan <d@example.org>"}, "/h", "/w")
	line := envLogLine(names)
	for _, v := range []string{"/secret/path", "Dan", "/h", "/w"} {
		if strings.Contains(line, v) {
			t.Errorf("log line holds a value %q: %s", v, line)
		}
	}
}
