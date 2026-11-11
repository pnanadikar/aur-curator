package build

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real bubblewrap: the sandbox is the only way
// MakepkgBuilder builds (#496), so without bwrap they fail, never skip.

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

// fixture is a workspace laid out as the engine lays it out, with the
// paths the sandbox hides around it.
type fixture struct {
	base, root, ws, logPath, repo, home, config, activity string
	builder                                               MakepkgBuilder
}

// newFixture builds the layout under a short directory in /tmp (a Unix
// socket path must stay under 108 bytes), and a fake makepkg in the
// workspace, first on PATH: the only place a build can reach it.
//
// The fake makepkg prints .srcinfo-fixture for --printsrcinfo;
// otherwise it dumps its environment to env.dump, says whether a keybox
// is bound, prints one line and writes an empty package archive.
func newFixture(t *testing.T) fixture {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "sb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	f := fixture{
		base:     base,
		root:     filepath.Join(base, "root"),
		repo:     filepath.Join(base, "repo"),
		home:     filepath.Join(base, "home"),
		config:   filepath.Join(base, "config.jsonc"),
		activity: filepath.Join(base, "root", ".aur-curator", "activity.log"),
	}
	f.ws = filepath.Join(f.root, ".aur-curator", "builds", "b1", "work", "src")
	f.logPath = filepath.Join(f.root, ".aur-curator", "builds", "b1", "build.log")
	for _, d := range []string{f.ws, f.repo, f.home, filepath.Join(f.ws, "bin")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		f.config:                                `{"giteaToken": "secret-token-496"}`,
		f.activity:                              "secret activity\n",
		filepath.Join(f.home, "secret"):         "secret in home\n",
		filepath.Join(f.repo, "custom.db"):      "repository\n",
		filepath.Join(f.ws, ".srcinfo-fixture"): "pkgbase = x\n\tpkgver = 1\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		// --printsrcinfo must get a read-only workspace, and the network
		// (top-level PKGBUILD code may fetch): it reports a write, and the
		// interfaces it sees, on stderr.
		// Like the real one, it refuses to start unless $BUILDDIR is
		// writable, --printsrcinfo included.
		"[ -w \"$BUILDDIR\" ] || { echo \"ERROR: You do not have write permission for the directory BUILDDIR ($BUILDDIR).\" >&2; exit 11; }\n" +
		"if [ \"$1\" = --printsrcinfo ]; then\n" +
		"  touch ./printsrcinfo-wrote 2>/dev/null && echo PRINTSRCINFO-WROTE >&2\n" +
		"  echo \"PRINTSRCINFO-IFACES=$(grep -c : /proc/net/dev)\" >&2\n" +
		"  cat ./.srcinfo-fixture; exit\n" +
		"fi\n" +
		"env > ./env.dump\n" +
		"if [ -f \"$HOME/.gnupg/pubring.kbx\" ]; then echo keybox-present; fi\n" +
		"cat \"$HOME/secret\" " + f.config + " 2>/dev/null && echo SECRET-READ\n" +
		"echo 'fake makepkg ran'\n" +
		": > x-1-1-any.pkg.tar.zst\n"
	if err := os.WriteFile(filepath.Join(f.ws, "bin", "makepkg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if helper, err = filepath.EvalSymlinks(helper); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	isolateEnv(t)
	t.Setenv("PATH", filepath.Join(f.ws, "bin")+string(os.PathListSeparator)+path)

	f.builder = NewMakepkgBuilder(SandboxPaths{
		RootPath:       f.root,
		RepositoryPath: f.repo,
		ConfigFile:     f.config,
		ActivityLog:    f.activity,
		Helper:         helper,
		Home:           f.home,
	})
	return f
}

func (f fixture) srcinfo(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.ws, ".srcinfo-fixture"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) build(t *testing.T) (BuildResult, string, error) {
	t.Helper()
	result, err := f.builder.Build(context.Background(), f.ws, f.logPath)
	log, readErr := os.ReadFile(f.logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	return result, string(log), err
}

func (f fixture) dump(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.ws, "env.dump"))
	if err != nil {
		t.Fatalf("makepkg did not run: %v", err)
	}
	return string(b)
}

func (f fixture) makepkgRan() bool {
	_, err := os.Stat(filepath.Join(f.ws, "env.dump"))
	return err == nil
}

func TestMakepkgBuilderEnvironment(t *testing.T) {
	f := newFixture(t)
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "tok-value-496")
	t.Setenv("SSH_AUTH_SOCK", "/nonexistent/agent.sock")
	t.Setenv("FOO_TOKEN", "foo-value-496")
	t.Setenv("KEEP_ME", "not-allowlisted")
	t.Setenv("LANG", "C.UTF-8")

	result, log, err := f.build(t)
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, log)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(f.dump(t)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	// Exactly the allowlist, the pinned values and bwrap's PWD; sh adds
	// nothing that survives env(1) here but its own PWD, which is the same.
	want := map[string]string{
		"PATH":       os.Getenv("PATH"),
		"LANG":       "C.UTF-8",
		"HOME":       f.home,
		"PKGDEST":    f.ws,
		"SRCDEST":    f.ws,
		"SRCPKGDEST": f.ws,
		"LOGDEST":    f.ws,
		"BUILDDIR":   f.ws,
		"PWD":        f.ws,

		"FAKEROOTDONTTRYCHOWN": "1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok && k != "SHLVL" && k != "_" {
			t.Errorf("%s reached makepkg", k)
		}
	}

	lines := strings.Split(log, "\n")
	if want := "aur-curator: build environment: PATH, LANG, HOME, PKGDEST, SRCDEST, SRCPKGDEST, LOGDEST, BUILDDIR, FAKEROOTDONTTRYCHOWN"; lines[0] != want {
		t.Errorf("log line 1 = %q, want %q", lines[0], want)
	}
	for _, s := range []string{"tok-value-496", "foo-value-496", "not-allowlisted"} {
		if strings.Contains(log, s) {
			t.Errorf("%q in the build log", s)
		}
	}
	if !strings.Contains(log, "fake makepkg ran") {
		t.Error("makepkg's output is not in the log")
	}
	if strings.Contains(log, "SECRET-READ") {
		t.Error("the build read the home directory or the config file")
	}
	if strings.Contains(log, "keybox-present") {
		t.Error("a keybox was bound for a PKGBUILD with no validpgpkeys")
	}
	if strings.Contains(log, "PRINTSRCINFO-WROTE") {
		t.Error("makepkg --printsrcinfo could write the workspace")
	}
	// The same interfaces as the host: the network is shared.
	hostNet, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("PRINTSRCINFO-IFACES=%d", strings.Count(string(hostNet), ":")); !strings.Contains(log, want) {
		t.Errorf("makepkg --printsrcinfo has no network (want %s):\n%s", want, log)
	}
	if len(result.ArtifactPaths) != 1 {
		t.Errorf("ArtifactPaths = %q, want one archive", result.ArtifactPaths)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.logPath), "keyring")); !os.IsNotExist(err) {
		t.Error("a keyring directory was created with no validpgpkeys")
	}
}

// A self-test failure stops the build before makepkg runs, and the log
// and the error name the check.
func TestMakepkgBuilderSelfTestFailure(t *testing.T) {
	f := newFixture(t)
	l, err := net.Listen("unix", filepath.Join(f.ws, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	_, log, err := f.build(t)
	if err == nil || !strings.Contains(err.Error(), "sockets") {
		t.Fatalf("err = %v, want the sockets check named\n%s", err, log)
	}
	if !strings.Contains(log, "aur-curator: sandbox self-test failed: sockets: accepts a connection: "+filepath.Join(f.ws, "s.sock")) {
		t.Errorf("log does not name the check:\n%s", log)
	}
	if f.makepkgRan() {
		t.Error("makepkg ran after a failed self-test")
	}
}

// With validpgpkeys, only those keys are exported and bound, read-only,
// where makepkg's gpg looks.
func TestMakepkgBuilderKeybox(t *testing.T) {
	f := newFixture(t)
	gnupgHome(t, true)
	fpr := genKey(t, "Upstream <up@example.org>")
	f.srcinfo(t, "pkgbase = x\n\tvalidpgpkeys = "+fpr+"\n")

	_, log, err := f.build(t)
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, log)
	}
	if !strings.Contains(log, "aur-curator: validpgpkeys: "+fpr) {
		t.Errorf("log does not name the key:\n%s", log)
	}
	if !strings.Contains(log, "keybox-present") {
		t.Errorf("no keybox inside the sandbox:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.logPath), "keyring", "pubring.kbx")); err != nil {
		t.Errorf("no keybox beside the log: %v", err)
	}
}

func TestMakepkgBuilderKeyProblems(t *testing.T) {
	for _, tc := range []struct {
		name, srcinfo, want string
	}{
		{"missing key", "\tvalidpgpkeys = " + fprA + "\n", "not in your GnuPG keyring: " + fprA},
		{"not a fingerprint", "\tvalidpgpkeys = --output=/x\n", "not a full fingerprint: --output=/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			gnupgHome(t, false)
			f.srcinfo(t, tc.srcinfo)
			_, log, err := f.build(t)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if !strings.Contains(log, tc.want) {
				t.Errorf("log does not say why:\n%s", log)
			}
			if f.makepkgRan() {
				t.Error("makepkg built anyway")
			}
		})
	}
}

// No bwrap: the build fails, saying the sandbox did not start. There is
// no fallback.
func TestMakepkgBuilderNoBwrap(t *testing.T) {
	f := newFixture(t)
	t.Setenv("PATH", filepath.Join(f.ws, "bin"))
	_, log, err := f.build(t)
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(log, "sandbox self-test failed: start") {
		t.Errorf("log:\n%s", log)
	}
	if f.makepkgRan() {
		t.Error("makepkg ran without the sandbox")
	}
}

func TestMakepkgBuilderMissingPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*SandboxPaths)
		want error
	}{
		{"no helper", func(p *SandboxPaths) { p.Helper = "" }, errNoHelper},
		{"no home", func(p *SandboxPaths) { p.Home = "" }, errNoHome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.edit(&f.builder.Paths)
			_, log, err := f.build(t)
			if !errors.Is(err, tc.want) || !strings.Contains(log, tc.want.Error()) {
				t.Fatalf("err = %v, log %q", err, log)
			}
		})
	}
}

func TestUserMakepkgConf(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	write := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("PACKAGER=x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if src, _ := userMakepkgConf("", home); src != "" {
		t.Fatalf("none: %s", src)
	}
	write(filepath.Join(home, ".makepkg.conf"))
	if src, dest := userMakepkgConf("", home); src != filepath.Join(home, ".makepkg.conf") || dest != src {
		t.Fatalf("legacy: %s → %s", src, dest)
	}
	write(filepath.Join(xdg, "pacman", "makepkg.conf"))
	src, dest := userMakepkgConf(xdg, home)
	if src != filepath.Join(xdg, "pacman", "makepkg.conf") || dest != filepath.Join(home, ".config", "pacman", "makepkg.conf") {
		t.Fatalf("xdg: %s → %s", src, dest)
	}
}

func TestResolvConfInRun(t *testing.T) {
	d := t.TempDir()
	plain := filepath.Join(d, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolvConfInRun(plain); got != "" {
		t.Errorf("regular file: %q", got)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}
	if got := resolvConfInRun(link); got != "" {
		t.Errorf("symlink outside /run: %q", got)
	}
	if got := resolvConfInRun(filepath.Join(d, "missing")); got != "" {
		t.Errorf("missing: %q", got)
	}
}

// Check 4: the same self-test, in a throwaway workspace under dir that
// is gone afterwards.
func TestCheckSandbox(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, ".aur-curator")

	rep, err := CheckSandbox(context.Background(), f.builder.Paths, dir)
	if err != nil {
		t.Fatalf("CheckSandbox: %v (%+v)", err, rep)
	}
	if len(rep.Results) != 7 || len(rep.Failed()) != 0 {
		t.Fatalf("report %+v", rep)
	}
	left, err := filepath.Glob(filepath.Join(dir, "sandbox-check-*"))
	if err != nil || len(left) != 0 {
		t.Fatalf("self-test workspace left behind: %q %v", left, err)
	}

	t.Setenv("PATH", filepath.Join(f.ws, "bin"))
	if _, err := CheckSandbox(context.Background(), f.builder.Paths, dir); err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("no bwrap: %v", err)
	}

	paths := f.builder.Paths
	paths.Helper = ""
	if _, err := CheckSandbox(context.Background(), paths, dir); !errors.Is(err, errNoHelper) {
		t.Fatalf("no helper: %v", err)
	}
}
