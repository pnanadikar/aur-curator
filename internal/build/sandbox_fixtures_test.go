//go:build sandboxfixtures

package build

// The adversarial fixtures of P5 (#496, docs/plans/496-sandboxed-builds.md
// step 7): PKGBUILDs whose build() tries one thing a hostile package
// would, built through MakepkgBuilder with the real makepkg inside the
// real bubblewrap. Run them with `make test-sandbox`, as a normal user
// (makepkg refuses root). Without bwrap or makepkg they fail, never skip.

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newRealFixture is newFixture's layout with the real makepkg on PATH
// and the given PKGBUILD in the workspace.
func newRealFixture(t *testing.T, pkgbuild string) fixture {
	t.Helper()
	for _, tool := range []string{"bwrap", "makepkg", "fakeroot"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: %v", tool, err)
		}
	}
	if os.Geteuid() == 0 {
		t.Fatal("run as a normal user: makepkg refuses root")
	}
	f := newFixture(t)
	// The fake makepkg goes: these builds use the real one.
	if err := os.RemoveAll(filepath.Join(f.ws, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.ws, ".srcinfo-fixture")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ws, "PKGBUILD"), []byte(pkgbuild), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// attack is a PKGBUILD whose build() runs script and writes what
// happened to $startdir/result. The package itself always builds, so
// every attempt in script ends in "|| echo ...": makepkg aborts build()
// on any failing command (its ERR trap), a refused attempt included.
func attack(script string) string {
	return `pkgname=attack
pkgver=1
pkgrel=1
arch=(any)
build() {
	exec >"$startdir/result" 2>&1
	set +e # makepkg runs build() with errexit; a refused attack must not end it
` + script + `
	return 0
}
package() { :; }
`
}

func (f fixture) result(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.ws, "result"))
	if err != nil {
		t.Fatalf("build() wrote no result: %v", err)
	}
	return string(b)
}

func mustBuild(t *testing.T, f fixture) string {
	t.Helper()
	_, log, err := f.build(t)
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, log)
	}
	return log
}

func absentOnHost(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		_ = os.Remove(path)
		t.Errorf("%s reached the host", path)
	}
}

func TestSandboxFixtureReadHome(t *testing.T) {
	f := newRealFixture(t, attack(`cat "$HOME/secret" && echo READ || echo "cat: $?"`))
	mustBuild(t, f)
	if r := f.result(t); strings.Contains(r, "secret in home") || strings.Contains(r, "READ") {
		t.Fatalf("read $HOME: %s", r)
	}
}

func TestSandboxFixtureReadConfig(t *testing.T) {
	f := newRealFixture(t, "")
	writePKGBUILD(t, f, attack(fmt.Sprintf(`cat %q && echo READ || echo "cat: $?"`, f.config)))
	mustBuild(t, f)
	if r := f.result(t); strings.Contains(r, "secret-token-496") || strings.Contains(r, "READ") {
		t.Fatalf("read the token: %s", r)
	}
}

func TestSandboxFixtureWriteRootPath(t *testing.T) {
	f := newRealFixture(t, "")
	target := filepath.Join(f.root, "pwned")
	other := filepath.Join(f.root, ".aur-curator", "builds", "other-build")
	writePKGBUILD(t, f, attack(fmt.Sprintf(`echo x > %q || echo "write: $?"; mkdir -p %q && echo x > %q/f || echo "mkdir: $?"`, target, other, other)))
	mustBuild(t, f)
	absentOnHost(t, target)
	absentOnHost(t, other)
}

func TestSandboxFixtureWriteRepository(t *testing.T) {
	f := newRealFixture(t, "")
	target := filepath.Join(f.repo, "evil-1-1-any.pkg.tar.zst")
	db := filepath.Join(f.repo, "custom.db")
	writePKGBUILD(t, f, attack(fmt.Sprintf(`echo x > %q || echo "write: $?"; echo x >> %q || echo "append: $?"; echo "entries: $( (ls -A %q 2>/dev/null || true) | wc -l)"`, target, db, f.repo)))
	mustBuild(t, f)
	absentOnHost(t, target)
	if b, err := os.ReadFile(db); err != nil || string(b) != "repository\n" {
		t.Errorf("repository database changed: %q %v", b, err)
	}
	if r := f.result(t); !strings.Contains(r, "entries: 0") {
		t.Errorf("the repository was listable: %s", r)
	}
}

func TestSandboxFixtureSudo(t *testing.T) {
	f := newRealFixture(t, attack(`sudo -n true && echo ESCALATED || echo "sudo: $?"`))
	mustBuild(t, f)
	if r := f.result(t); strings.Contains(r, "ESCALATED") || strings.Contains(r, "sudo: 0") {
		t.Fatalf("sudo -n succeeded: %s", r)
	}
}

func TestSandboxFixtureUserNamespace(t *testing.T) {
	f := newRealFixture(t, attack(`unshare -U true && echo NESTED || echo "unshare: $?"`))
	mustBuild(t, f)
	if r := f.result(t); strings.Contains(r, "NESTED") || strings.Contains(r, "unshare: 0") {
		t.Fatalf("created a user namespace: %s", r)
	}
}

// A socket in /run/user/<uid> (where the agents live) and one in /tmp,
// each listening on the host: the build must reach neither. curl's
// exit status 7 is "could not connect"; anything else connected.
func TestSandboxFixtureSockets(t *testing.T) {
	f := newRealFixture(t, "")
	var socks []string
	tmp, err := os.MkdirTemp("/tmp", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	socks = append(socks, filepath.Join(tmp, "agent.sock"))
	if run := fmt.Sprintf("/run/user/%d", os.Getuid()); writableDir(run) {
		d, err := os.MkdirTemp(run, "aur-curator-fixture-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(d) })
		socks = append(socks, filepath.Join(d, "s"))
	} else {
		t.Logf("no writable %s: testing /tmp only", run)
	}
	var script strings.Builder
	for _, s := range socks {
		l, err := net.Listen("unix", s)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				_ = c.Close()
			}
		}()
		fmt.Fprintf(&script, "curl -s -m 2 --unix-socket %q http://x/ && echo \"%s: 0\" || echo \"%s: $?\"\n", s, s, s)
	}
	writePKGBUILD(t, f, attack(script.String()))
	mustBuild(t, f)
	r := f.result(t)
	for _, s := range socks {
		if !strings.Contains(r, s+": 7") {
			t.Errorf("%s: connected or untested: %s", s, r)
		}
	}
}

// Positive: a signed local source verifies against a key the PKGBUILD
// names, exported from a keyboxd home; tampered, it fails.
func TestSandboxFixtureSignedSource(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(fmt.Sprintf("tampered=%v", tamper), func(t *testing.T) {
			f := newRealFixture(t, "")
			gnupgHome(t, true)
			fpr := genKey(t, "Upstream <up@example.org>")
			data := filepath.Join(f.ws, "data.txt")
			if err := os.WriteFile(data, []byte("upstream release\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("gpg", "--batch", "--detach-sign", data).CombinedOutput(); err != nil {
				t.Fatalf("sign: %v: %s", err, out)
			}
			if tamper {
				if err := os.WriteFile(data, []byte("replaced\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writePKGBUILD(t, f, `pkgname=signed
pkgver=1
pkgrel=1
arch=(any)
source=(data.txt data.txt.sig)
sha256sums=(SKIP SKIP)
validpgpkeys=(`+fpr+`)
package() { install -Dm644 data.txt "$pkgdir/usr/share/signed/data.txt"; }
`)
			_, log, err := f.build(t)
			if tamper {
				if err == nil {
					t.Fatalf("a tampered source built:\n%s", log)
				}
				// A bad signature keeps makepkg's message (#514).
				if strings.Contains(log, "unknown public key") || strings.Contains(err.Error(), "out of date") {
					t.Errorf("a bad signature got the stale-key hint: %v\n%s", err, log)
				}
				return
			}
			if err != nil {
				t.Fatalf("a correctly signed source failed: %v\n%s", err, log)
			}
			if !strings.Contains(log, "aur-curator: validpgpkeys: "+fpr) {
				t.Errorf("log does not name the key:\n%s", log)
			}
		})
	}
}

// A key whose local copy predates the subkey that signed: makepkg fails
// with "unknown public key <subkey ID>", and aur-curator adds the hint
// naming the key (#514). The keyring has the key, so the build gets past
// exportKeys; the copy lacks the signing subkey, as the user's Tor key
// lacked the subkey created 11 Aug 2026.
func TestSandboxFixtureStaleKey(t *testing.T) {
	f := newRealFixture(t, "")
	gnupgHome(t, true)
	fpr := genKey(t, "Upstream <up@example.org>")
	gpg := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("gpg", append([]string{"--batch"}, args...)...).Output()
		if err != nil {
			t.Fatalf("gpg %v: %v", args, err)
		}
		return string(out)
	}
	gpg("--passphrase", "", "--quick-add-key", fpr, "ed25519", "sign", "never")
	var subID, subFpr string
	inSub := false
	for _, line := range strings.Split(gpg("--with-colons", "--list-keys", fpr), "\n") {
		f := strings.Split(line, ":")
		switch {
		case f[0] == "sub" && len(f) > 4:
			subID, inSub = f[4], true
		case f[0] == "fpr" && inSub && len(f) > 9:
			subFpr, inSub = f[9], false
		}
	}
	if subID == "" || subFpr == "" {
		t.Fatal("no signing subkey")
	}

	data := filepath.Join(f.ws, "data.txt")
	if err := os.WriteFile(data, []byte("upstream release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gpg("--local-user", subID+"!", "--detach-sign", data)

	// The stale copy: the key without its signing subkey, in place of
	// the whole key.
	stale := gpg("--export-filter", "drop-subkey=fpr = "+subFpr, "--export", fpr)
	gpg("--yes", "--delete-secret-and-public-key", fpr)
	imp := exec.Command("gpg", "--batch", "--import")
	imp.Stdin = strings.NewReader(stale)
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("import the stale copy: %v: %s", err, out)
	}
	if strings.Contains(gpg("--with-colons", "--list-keys", fpr), "\nsub:") {
		t.Fatal("the stale copy still has the subkey")
	}

	writePKGBUILD(t, f, `pkgname=signed
pkgver=1
pkgrel=1
arch=(any)
source=(data.txt data.txt.sig)
sha256sums=(SKIP SKIP)
validpgpkeys=(`+fpr+`)
package() { install -Dm644 data.txt "$pkgdir/usr/share/signed/data.txt"; }
`)
	_, log, err := f.build(t)
	if err == nil {
		t.Fatalf("built with a stale key:\n%s", log)
	}
	if !strings.Contains(log, "FAILED (unknown public key "+subID+")") {
		t.Fatalf("makepkg did not report the subkey as unknown:\n%s", log)
	}
	want := "unknown public key " + subID + ": either your copy of " + fpr + " is out of date"
	if !strings.Contains(log, "aur-curator: "+want) {
		t.Errorf("log lacks the hint %q:\n%s", want, log)
	}
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "gpg --refresh-keys "+fpr) {
		t.Errorf("error lacks the hint: %v", err)
	}
}

// A signed source with no validpgpkeys: the sandbox has no keyring, so
// makepkg reports the key as unknown, and there is no stale-key hint:
// there is no copy to be out of date (#514).
func TestSandboxFixtureNoValidPGPKeys(t *testing.T) {
	f := newRealFixture(t, "")
	gnupgHome(t, true)
	genKey(t, "Upstream <up@example.org>")
	data := filepath.Join(f.ws, "data.txt")
	if err := os.WriteFile(data, []byte("upstream release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("gpg", "--batch", "--detach-sign", data).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v: %s", err, out)
	}
	writePKGBUILD(t, f, `pkgname=signed
pkgver=1
pkgrel=1
arch=(any)
source=(data.txt data.txt.sig)
sha256sums=(SKIP SKIP)
package() { install -Dm644 data.txt "$pkgdir/usr/share/signed/data.txt"; }
`)
	_, log, err := f.build(t)
	if err == nil {
		t.Fatalf("built with no key to check against:\n%s", log)
	}
	if !strings.Contains(log, "unknown public key") {
		t.Fatalf("makepkg did not report an unknown key:\n%s", log)
	}
	if strings.Contains(log, "out of date") || strings.Contains(err.Error(), "out of date") {
		t.Errorf("hint with no validpgpkeys: %v\n%s", err, log)
	}
}

// Positive: package() can chown to root under fakeroot in the user
// namespace, and the archive records root:root. Without
// FAKEROOTDONTTRYCHOWN=1 fakeroot tries the real chown first, which
// fails with EINVAL (uid 0 is not mapped) and aborts package(); this
// fixture is what decided it is needed (#496).
func TestSandboxFixtureChownRoot(t *testing.T) {
	f := newRealFixture(t, `pkgname=owned
pkgver=1
pkgrel=1
arch=(any)
package() {
	install -Dm644 /dev/null "$pkgdir/usr/share/owned/file"
	chown root:root "$pkgdir/usr/share/owned/file"
	chown 0:0 "$pkgdir/usr/share/owned"
}
`)
	result, log, err := f.build(t)
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, log)
	}
	if len(result.ArtifactPaths) != 1 {
		t.Fatalf("artifacts %q", result.ArtifactPaths)
	}
	out, err := exec.Command("bsdtar", "-tvf", result.ArtifactPaths[0]).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if fields := strings.Fields(line); len(fields) > 3 && (fields[2] != "root" || fields[3] != "root") {
			t.Errorf("not root-owned: %s", line)
		}
	}
}

// Cancel ends the whole build: nothing it started is left running.
func TestSandboxFixtureCancel(t *testing.T) {
	marker := fmt.Sprintf("4961%d", time.Now().UnixNano()%100000)
	f := newRealFixture(t, attack("sleep "+marker))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.builder.Build(ctx, f.ws, f.logPath)
		done <- err
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !running(marker) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the build's sleep never started")
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	// Without --die-with-parent the sandbox outlives bwrap, keeps the
	// log pipe open, and Build never returns: a deadline, not a hang.
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("Build after cancel: %v", err)
		}
	case <-time.After(20 * time.Second):
		_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
		t.Fatal("Build did not return after cancel: the sandbox outlived it")
	}
	time.Sleep(time.Second)
	if running(marker) {
		_ = exec.Command("pkill", "-f", "sleep "+marker).Run()
		t.Fatal("the build outlived its cancel")
	}
}

func running(marker string) bool {
	return exec.Command("pgrep", "-f", "sleep "+marker).Run() == nil
}

func writePKGBUILD(t *testing.T, f fixture, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.ws, "PKGBUILD"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writableDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.CreateTemp(path, ".probe-")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}
