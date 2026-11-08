package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateEnv clears the test process's environment except PATH, so the
// test controls every variable makepkg could inherit (a desktop session
// sets DISPLAY, SSH_AUTH_SOCK, ...). t.Setenv restores each one at
// cleanup.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if name == "" || name == "PATH" {
			continue
		}
		t.Setenv(name, value)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

// fakeMakepkg puts a makepkg script in bin, first on PATH. It dumps its
// environment to dumpPath, prints one line to stdout and creates an empty
// package archive in its working directory.
func fakeMakepkg(t *testing.T, bin, dumpPath string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"env > '" + dumpPath + "'\n" +
		"echo 'fake makepkg ran'\n" +
		": > x-1-1-any.pkg.tar.zst\n"
	if err := os.WriteFile(filepath.Join(bin, "makepkg"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake makepkg: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// runFakeBuild runs MakepkgBuilder.Build against the fake makepkg and
// returns the environment dump, the log and the result.
func runFakeBuild(t *testing.T, setup func()) (dump, log string, result BuildResult) {
	t.Helper()
	workDir := t.TempDir()
	dumpPath := filepath.Join(t.TempDir(), "env.dump")
	logPath := filepath.Join(t.TempDir(), "logs", "build.log")
	bin := t.TempDir() // before isolateEnv, which clears TMPDIR

	isolateEnv(t)
	fakeMakepkg(t, bin, dumpPath)
	setup()

	result, err := MakepkgBuilder{}.Build(context.Background(), workDir, logPath)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	dumpBytes, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(dumpBytes), string(logBytes), result
}

// hasVar reports whether an env dump has a line for name.
func hasVar(dump, name string) bool {
	for _, line := range strings.Split(dump, "\n") {
		if strings.HasPrefix(line, name+"=") {
			return true
		}
	}
	return false
}

func TestMakepkgBuilderCleanEnvironment(t *testing.T) {
	const (
		tokenValue = "tok-value-473"
		fooValue   = "foo-value-473"
	)
	dump, log, result := runFakeBuild(t, func() {
		t.Setenv("AUR_CURATOR_GITEA_TOKEN", tokenValue)
		t.Setenv("SSH_AUTH_SOCK", "/nonexistent/agent.sock")
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/bus")
		t.Setenv("FOO_TOKEN", fooValue)
		t.Setenv("KEEP_ME", "kept-473")
	})

	for _, name := range []string{"AUR_CURATOR_GITEA_TOKEN", "SSH_AUTH_SOCK", "DBUS_SESSION_BUS_ADDRESS", "FOO_TOKEN"} {
		if hasVar(dump, name) {
			t.Errorf("%s reached makepkg", name)
		}
	}
	if !hasVar(dump, "KEEP_ME") {
		t.Error("KEEP_ME did not reach makepkg")
	}
	for _, value := range []string{tokenValue, fooValue} {
		if strings.Contains(dump, value) {
			t.Errorf("value %q reached makepkg", value)
		}
		if strings.Contains(log, value) {
			t.Errorf("value %q written to the build log", value)
		}
	}

	lines := strings.Split(log, "\n")
	wantFirst := "aur-curator: removed from build environment: AUR_CURATOR_GITEA_TOKEN, DBUS_SESSION_BUS_ADDRESS, FOO_TOKEN, SSH_AUTH_SOCK"
	if lines[0] != wantFirst {
		t.Errorf("log line 1 = %q, want %q", lines[0], wantFirst)
	}
	if len(lines) < 2 || lines[1] != "fake makepkg ran" {
		t.Errorf("log line 2 = %q, want makepkg's output", lines[1:])
	}
	if len(result.ArtifactPaths) != 1 {
		t.Errorf("ArtifactPaths = %q, want one archive", result.ArtifactPaths)
	}
}

func TestMakepkgBuilderNothingRemoved(t *testing.T) {
	dump, log, _ := runFakeBuild(t, func() {
		t.Setenv("KEEP_ME", "kept-473")
	})

	if !hasVar(dump, "KEEP_ME") {
		t.Error("KEEP_ME did not reach makepkg")
	}
	first, _, _ := strings.Cut(log, "\n")
	if want := "aur-curator: removed from build environment: none"; first != want {
		t.Errorf("log line 1 = %q, want %q", first, want)
	}
}
