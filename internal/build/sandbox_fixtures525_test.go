//go:build sandboxfixtures

package build

// P7's fixture (#525): a package built by the real makepkg, inspected by
// the real bsdtar in the sandbox. makepkg refuses paths with a newline
// ("Package contains paths with newlines"), so that escape is only in
// internal/inspect's tests.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/inspect"
)

func TestSandboxFixtureInspect(t *testing.T) {
	f := newRealFixture(t, `pkgname=p7demo
pkgver=1
pkgrel=1
arch=(any)
install=p7demo.install
package() {
  install -Dm4755 /dev/null "$pkgdir/usr/bin/suidtool"
  install -Dm644 /dev/null "$pkgdir/usr/share/libalpm/hooks/p7.hook"
  install -Dm644 /dev/null "$pkgdir/usr/lib/systemd/system/p7.service"
  install -Dm440 /dev/null "$pkgdir/etc/sudoers.d/p7"
  install -Dm644 /dev/null "$pkgdir/usr/lib/tmpfiles.d/p7.conf"
  install -Dm644 /dev/null "$pkgdir/usr/share/doc/p7/README"
  mkdir -p "$pkgdir/usr/share/odd"
  touch "$pkgdir/usr/share/odd/f -> x"
  install -Dm644 /dev/null "$pkgdir/etc/odd -> name"
}
`)
	if err := os.WriteFile(filepath.Join(f.ws, "p7demo.install"), []byte("post_install() {\n  echo p7\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, log, err := f.build(t)
	if err != nil || len(result.ArtifactPaths) != 1 {
		t.Fatalf("build: %v %q\n%s", err, result.ArtifactPaths, log)
	}
	// The Engine keeps artifacts in builds/<id>/.
	name := filepath.Base(result.ArtifactPaths[0])
	data, err := os.ReadFile(result.ArtifactPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".aur-curator", "builds", "b1", name), data, 0o600); err != nil {
		t.Fatal(err)
	}

	ins := ArchiveInspector{Paths: f.builder.Paths}
	r, err := ins.Inspect(context.Background(), "b1", name)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	got := map[inspect.Category][]string{}
	for _, g := range r.Groups {
		for _, e := range g.Entries {
			got[g.Category] = append(got[g.Category], e.Path)
		}
	}
	want := map[inspect.Category][]string{
		inspect.CatSetID:    {"usr/bin/suidtool"},
		inspect.CatInstall:  {".INSTALL"},
		inspect.CatHook:     {"usr/share/libalpm/hooks/p7.hook"},
		inspect.CatUnit:     {"usr/lib/systemd/system/p7.service"},
		inspect.CatSudoers:  {"etc/sudoers.d/p7"},
		inspect.CatSysusers: {"usr/lib/tmpfiles.d/p7.conf"},
		inspect.CatEtc:      {"etc/odd -> name"},
	}
	for c, w := range want {
		if strings.Join(got[c], "|") != strings.Join(w, "|") {
			t.Errorf("%s = %q, want %q", c, got[c], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("categories %v", got)
	}
	if s := r.Groups[0].Entries[0]; s.Mode != 0o4755 || s.UID != 0 || s.Uname != "root" {
		t.Errorf("setuid entry %+v", s)
	}

	script, err := ins.InstallScript(context.Background(), "b1", name)
	if err != nil || script != "post_install() {\n  echo p7\n}\n" {
		t.Errorf("InstallScript = %q, %v", script, err)
	}

	// Not an archive: an error naming bsdtar, not a guess.
	if err := os.WriteFile(filepath.Join(f.root, ".aur-curator", "builds", "b1", "bad.pkg.tar.zst"), []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ins.Inspect(context.Background(), "b1", "bad.pkg.tar.zst"); err == nil {
		t.Error("a file that is not an archive inspected without error")
	} else {
		t.Logf("not an archive: %v", err)
	}
}
