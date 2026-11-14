//go:build sandboxfixtures

package build

// P6's fixtures (#522, docs/plans/522-network-fetch-only.md step 7): the
// network only for the download. "The network" here is the host's
// loopback: a TCP listener on 127.0.0.1 and an abstract Unix socket. A
// sandbox with a network namespace of its own has its own lo and reaches
// neither; one sharing the host's reaches both. No internet is needed.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// hostListeners starts a TCP listener on 127.0.0.1 and an abstract Unix
// socket, each accepting and closing; it returns the TCP port, the
// abstract name (without '@') and the number of connections so far.
func hostListeners(t *testing.T) (port int, abstract string, conns func() int64) {
	t.Helper()
	var n atomic.Int64
	serve := func(l net.Listener) {
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				n.Add(1)
				_ = c.Close()
			}
		}()
	}
	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serve(tl)
	abstract = fmt.Sprintf("aur-curator-fixture-522-%d", os.Getpid())
	ul, err := net.Listen("unix", "@"+abstract)
	if err != nil {
		t.Fatal(err)
	}
	serve(ul)
	return tl.Addr().(*net.TCPAddr).Port, abstract, n.Load
}

// connectScript tries both host listeners from build(); curl's exit
// status 7 is "could not connect".
func connectScript(port int, abstract string) string {
	return fmt.Sprintf(`(exec 3<>/dev/tcp/127.0.0.1/%d) 2>/dev/null && echo "tcp: connected" || echo "tcp: refused"
	curl -s -m 2 --abstract-unix-socket %q http://x/ && echo "abstract: 0" || echo "abstract: $?"
`, port, abstract)
}

// Without buildNetwork, build() reaches neither the host's loopback nor
// its abstract sockets (B4, B5).
func TestSandboxFixtureNetworkOffline(t *testing.T) {
	port, abstract, conns := hostListeners(t)
	f := newRealFixture(t, attack(connectScript(port, abstract)))
	mustBuild(t, f)
	r := f.result(t)
	if !strings.Contains(r, "tcp: refused") || !strings.Contains(r, "abstract: 7") {
		t.Errorf("build() reached the host's network: %s", r)
	}
	if n := conns(); n != 0 {
		t.Errorf("%d connection(s) reached the host", n)
	}
}

// With buildNetwork, build() shares the host's network, as in v1.4.
func TestSandboxFixtureNetworkBuildNetwork(t *testing.T) {
	port, abstract, conns := hostListeners(t)
	f := newRealFixture(t, attack(connectScript(port, abstract)))
	if log, err := f.buildWith(t, BuildOptions{Package: "attack", Network: true}); err != nil {
		t.Fatalf("build failed: %v\n%s", err, log)
	}
	r := f.result(t)
	if !strings.Contains(r, "tcp: connected") || strings.Contains(r, "abstract: 7") {
		t.Errorf("build() did not reach the host's network: %s", r)
	}
	if n := conns(); n < 2 {
		t.Errorf("%d connection(s) reached the host, want 2", n)
	}
}

// prepare() that needs the network: the build fails without it, saying
// so; with buildNetwork it builds.
func TestSandboxFixturePrepareFetch(t *testing.T) {
	port, _, _ := hostListeners(t)
	pkgbuild := fmt.Sprintf(`pkgname=fetcher
pkgver=1
pkgrel=1
arch=(any)
prepare() { exec 3<>/dev/tcp/127.0.0.1/%d; }
package() { :; }
`, port)

	f := newRealFixture(t, pkgbuild)
	log, err := f.buildWith(t, BuildOptions{Package: "fetcher"})
	if err == nil {
		t.Fatalf("prepare() reached the network:\n%s", log)
	}
	if !strings.Contains(log, "Starting prepare()") {
		t.Errorf("did not fail in prepare():\n%s", log)
	}
	hint := "this build had no network (#522); if the package needs it, check why in the PKGBUILD, then add fetcher to buildNetwork in the config"
	if !strings.Contains(log, "aur-curator: "+hint) || !strings.HasSuffix(err.Error(), "; "+hint) {
		t.Errorf("no hint: %v\n%s", err, log)
	}

	f = newRealFixture(t, pkgbuild)
	if log, err := f.buildWith(t, BuildOptions{Package: "fetcher", Network: true}); err != nil {
		t.Fatalf("with buildNetwork: %v\n%s", err, log)
	}
}

// Top-level code that fetches (tor-browser-bin reads its checksum list
// with curl): the download has the network, so its checks pass; the
// build has none, so makepkg finds no checksums and stops. With
// buildNetwork it builds.
func TestSandboxFixtureTopLevelFetch(t *testing.T) {
	port, _, _ := hostListeners(t)
	pkgbuild := fmt.Sprintf(`pkgname=toplevel
pkgver=1
pkgrel=1
arch=(any)
source=(data.txt)
sha256sums=($( (exec 3<>/dev/tcp/127.0.0.1/%d) 2>/dev/null && echo SKIP))
package() { install -Dm644 data.txt "$pkgdir/usr/share/toplevel/data.txt"; }
`, port)
	setup := func() fixture {
		f := newRealFixture(t, pkgbuild)
		if err := os.WriteFile(filepath.Join(f.ws, "data.txt"), []byte("data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}

	f := setup()
	log, err := f.buildWith(t, BuildOptions{Package: "toplevel"})
	if err == nil {
		t.Fatalf("built without its checksums:\n%s", log)
	}
	if !strings.Contains(log, "aur-curator: sources downloaded; building without network") {
		t.Errorf("the download did not pass:\n%s", log)
	}
	if !strings.Contains(log, "Integrity checks are missing") || !strings.Contains(err.Error(), "add toplevel to buildNetwork") {
		t.Errorf("not the offline failure, or no hint: %v\n%s", err, log)
	}

	f = setup()
	if log, err := f.buildWith(t, BuildOptions{Package: "toplevel", Network: true}); err != nil {
		t.Fatalf("with buildNetwork: %v\n%s", err, log)
	}
}

// A git source builds without the network: the download clones it, the
// build (--holdver) does not fetch, and pkgver() reads the clone.
func TestSandboxFixtureGitSource(t *testing.T) {
	f := newRealFixture(t, "")
	up := filepath.Join(f.ws, "upstream")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", up, "-c", "user.name=u", "-c", "user.email=u@example.org", "-c", "init.defaultBranch=main"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(up, 0o750); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(up, "f"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "f")
	git("commit", "-q", "-m", "one")
	git("commit", "-q", "--allow-empty", "-m", "two")
	writePKGBUILD(t, f, `pkgname=gitsrc
pkgver=0
pkgrel=1
arch=(any)
source=("up::git+file://$startdir/upstream")
sha256sums=(SKIP)
pkgver() { cd up; printf 'r%s' "$(git rev-list --count HEAD)"; }
package() { install -Dm644 up/f "$pkgdir/usr/share/gitsrc/f"; }
`)
	result, log, err := f.build(t)
	if err != nil {
		t.Fatalf("git source failed without the network: %v\n%s", err, log)
	}
	if len(result.ArtifactPaths) != 1 || !strings.Contains(filepath.Base(result.ArtifactPaths[0]), "gitsrc-r2-1-") {
		t.Errorf("artifacts %q, want gitsrc-r2-1", result.ArtifactPaths)
	}
	if strings.Contains(log, "Updating up git repo") {
		t.Errorf("the build fetched the git source again:\n%s", log)
	}
}

// tor-browser-bin's shape: the fetch is quoted, so offline it leaves one
// empty checksum. makepkg 7's lint rejects it while reading the PKGBUILD
// (--printsrcinfo), before any build, as Dan's tor-browser-bin build
// showed; makepkg 6 reads it and fails the build's checksum check. The
// hint comes either way; with buildNetwork it builds.
func TestSandboxFixtureTopLevelFetchQuoted(t *testing.T) {
	port, _, _ := hostListeners(t)
	pkgbuild := fmt.Sprintf(`pkgname=toplevel
pkgver=1
pkgrel=1
arch=(any)
source=(data.txt)
sha256sums=("$( (exec 3<>/dev/tcp/127.0.0.1/%d) 2>/dev/null && echo SKIP)")
package() { install -Dm644 data.txt "$pkgdir/usr/share/toplevel/data.txt"; }
`, port)
	setup := func() fixture {
		f := newRealFixture(t, pkgbuild)
		if err := os.WriteFile(filepath.Join(f.ws, "data.txt"), []byte("data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return f
	}

	f := setup()
	log, err := f.buildWith(t, BuildOptions{Package: "toplevel"})
	if err == nil {
		t.Fatalf("built with an empty checksum:\n%s", log)
	}
	switch {
	case strings.HasPrefix(err.Error(), "makepkg --printsrcinfo failed: ") && strings.Contains(log, "does not allow empty values"):
		t.Log("failed reading the PKGBUILD (makepkg 7)")
	case strings.HasPrefix(err.Error(), "makepkg failed: ") && strings.Contains(log, "did not pass the validity check"):
		t.Log("failed the build's checksum check (makepkg 6)")
	default:
		t.Errorf("neither the --printsrcinfo nor the checksum failure: %v\n%s", err, log)
	}
	if !strings.Contains(log, "aur-curator: this build had no network (#522)") || !strings.HasSuffix(err.Error(), "add toplevel to buildNetwork in the config") {
		t.Errorf("no hint: %v\n%s", err, log)
	}

	f = setup()
	if log, err := f.buildWith(t, BuildOptions{Package: "toplevel", Network: true}); err != nil {
		t.Fatalf("with buildNetwork: %v\n%s", err, log)
	}
}
