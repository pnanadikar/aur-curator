package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/inspect"
)

// inspectorFixture puts a fake bsdtar in the build directory b1 (visible
// in the sandbox, first on PATH) and an artifact file next to it.
func inspectorFixture(t *testing.T) (fixture, ArchiveInspector, string) {
	t.Helper()
	f := newFixture(t)
	dir := filepath.Join(f.root, ".aur-curator", "builds", "b1")
	script := "#!/bin/sh\n" +
		// It may not write into the build directory, nor see the host's
		// network or the hidden files.
		"touch \"$PWD/inspector-wrote\" 2>/dev/null\n" +
		"cat \"$HOME/secret\" " + f.config + " >/dev/null 2>&1 && echo SECRET-READ >&2\n" +
		"[ -f \"$PWD/fail-bsdtar\" ] && { echo 'bsdtar: Unrecognized archive format' >&2; exit 1; }\n" +
		"case \"$1\" in\n" +
		"-cf) [ \"$3 $4\" = '--format=mtree --options=!all,type,mode,uid,gid,uname,gname,link' ] || { echo \"bad args: $*\" >&2; exit 2; }\n" +
		"     [ \"$5\" = \"@$PWD/x-1-1-any.pkg.tar.zst\" ] || { echo \"bad file: $5\" >&2; exit 2; }\n" +
		"     cat \"$PWD/listing\" ;;\n" +
		"-xOf) [ \"$3\" = .INSTALL ] || exit 2; echo 'post_install() { :; }'; echo \"IFACES=$(grep -c : /proc/net/dev)\" ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(f.ws, "bin", "bsdtar"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"x-1-1-any.pkg.tar.zst": "archive",
		"listing": "#mtree\n./.INSTALL mode=644 uid=0 gid=0 type=file\n" +
			"./usr/bin/s mode=4755 uid=0 gid=0 uname=root gname=root type=file\n" +
			"./usr/bin/plain mode=755 uid=0 gid=0 type=file\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return f, ArchiveInspector{Paths: f.builder.Paths}, dir
}

func TestArchiveInspectorInspect(t *testing.T) {
	_, ins, dir := inspectorFixture(t)
	r, err := ins.Inspect(context.Background(), "b1", "x-1-1-any.pkg.tar.zst")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(r.Groups) != 2 || r.Groups[0].Category != inspect.CatSetID || r.Groups[0].Entries[0].Path != "usr/bin/s" || !r.HasInstall() {
		t.Errorf("report %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "inspector-wrote")); err == nil {
		t.Error("bsdtar wrote into the build directory")
	}
}

func TestArchiveInspectorInstallScript(t *testing.T) {
	_, ins, _ := inspectorFixture(t)
	s, err := ins.InstallScript(context.Background(), "b1", "x-1-1-any.pkg.tar.zst")
	if err != nil {
		t.Fatalf("InstallScript: %v", err)
	}
	if !strings.HasPrefix(s, "post_install() { :; }\n") {
		t.Errorf("script %q", s)
	}
	// No network: lo alone.
	if !strings.Contains(s, "IFACES=1\n") {
		t.Errorf("bsdtar saw the host's network: %q", s)
	}
}

func TestArchiveInspectorErrors(t *testing.T) {
	_, ins, dir := inspectorFixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ id, file, want string }{
		{"b1", "../x-1-1-any.pkg.tar.zst", "is not a file name"},
		{"b1", "", "is not a file name"},
		{"b1", "missing.pkg.tar.zst", "no such file"},
		{"../b1", "x-1-1-any.pkg.tar.zst", "is not a build ID"},
		{"", "x-1-1-any.pkg.tar.zst", "is not a build ID"},
		{"b1", "work", "not a regular file"},
	} {
		if _, err := ins.Inspect(ctx, domainBuildID(tc.id), tc.file); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Inspect(%q, %q) = %v, want %q", tc.id, tc.file, err, tc.want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-bsdtar"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ins.Inspect(ctx, "b1", "x-1-1-any.pkg.tar.zst")
	if err == nil || !strings.HasPrefix(err.Error(), "bsdtar failed: ") || !strings.Contains(err.Error(), "Unrecognized archive format") {
		t.Errorf("failing bsdtar: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "fail-bsdtar")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "listing"), []byte("./a type=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ins.Inspect(ctx, "b1", "x-1-1-any.pkg.tar.zst"); err == nil || !strings.Contains(err.Error(), "no mode") {
		t.Errorf("unreadable listing: %v", err)
	}
}

func TestLimitBuffer(t *testing.T) {
	l := limitBuffer{limit: 4}
	if n, err := l.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatal(n, err)
	}
	if _, err := l.Write([]byte("de")); err == nil || !l.over || l.buf.String() != "abc" {
		t.Errorf("over limit: %v %v %q", err, l.over, l.buf.String())
	}
}

func domainBuildID(s string) domain.BuildID { return domain.BuildID(s) }
