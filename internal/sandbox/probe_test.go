package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// listen creates a Unix socket listening at path; it closes with the test.
func listen(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
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
}

// shortDir returns a temporary directory with a path short enough for
// a Unix socket (108 bytes on Linux; t.TempDir's can be longer).
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "sbx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func TestCheckSockets(t *testing.T) {
	root := shortDir(t)
	if f := checkSockets(root, nil, nil); f != "" {
		t.Fatalf("empty tree: %s", f)
	}

	sock := filepath.Join(root, "agent.sock")
	listen(t, sock)
	if f := checkSockets(root, nil, nil); !strings.Contains(f, sock) {
		t.Fatalf("listening socket not reported: %q", f)
	}
	if f := checkSockets(root, nil, []string{sock}); f != "" {
		t.Fatalf("skipped socket reported: %s", f)
	}

	// A socket nobody listens on is not a failure.
	dead := filepath.Join(root, "dead.sock")
	l, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	_ = l.Close()
	if f := checkSockets(root, nil, []string{sock}); f != "" {
		t.Fatalf("dead socket reported: %s", f)
	}

	// An excluded tree is not walked.
	sub := filepath.Join(root, "usr")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	listen(t, filepath.Join(sub, "s"))
	if f := checkSockets(root, []string{sub}, []string{sock}); f != "" {
		t.Fatalf("excluded tree walked: %s", f)
	}
}

func TestCheckNNP(t *testing.T) {
	d := t.TempDir()
	for _, tc := range []struct {
		status string
		ok     bool
	}{
		{"Name:\tx\nNoNewPrivs:\t1\nSeccomp:\t0\n", true},
		{"NoNewPrivs:\t0\n", false},
		{"Name:\tx\n", false},
	} {
		p := filepath.Join(d, "status")
		if err := os.WriteFile(p, []byte(tc.status), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := checkNNP(p) == ""; got != tc.ok {
			t.Errorf("%q: ok=%v, want %v (%s)", tc.status, got, tc.ok, checkNNP(p))
		}
	}
	if checkNNP(filepath.Join(d, "missing")) == "" {
		t.Error("missing status file passed")
	}
}

func TestCheckEnv(t *testing.T) {
	want := []string{"PATH=/usr/bin", "HOME=/h"}
	if f := checkEnv([]string{"HOME=/h", "PATH=/usr/bin"}, want); f != "" {
		t.Fatalf("same set, other order: %s", f)
	}
	f := checkEnv([]string{"HOME=/h", "PATH=/bin", "TOKEN=s3cr3t"}, want)
	if !strings.Contains(f, "TOKEN") || !strings.Contains(f, "PATH") || strings.Contains(f, "s3cr3t") || strings.Contains(f, "/bin") {
		t.Fatalf("detail %q: want the extra and the different name, never a value", f)
	}
}

func TestCheckChain(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, ".aur-curator", "builds", "b1", "work", "src")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	if f := checkChain(root, ws); f != "" {
		t.Fatalf("bare chain: %s", f)
	}
	// Files inside the workspace are fine: it is the build's own.
	if err := os.WriteFile(filepath.Join(ws, "PKGBUILD"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if f := checkChain(root, ws); f != "" {
		t.Fatalf("workspace content: %s", f)
	}
	// Another package beside .aur-curator is visible: fail.
	if err := os.Mkdir(filepath.Join(root, "otherpkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if f := checkChain(root, ws); !strings.Contains(f, "otherpkg") {
		t.Fatalf("sibling not reported: %q", f)
	}
}

func TestCheckChainWorkspaceOutside(t *testing.T) {
	root := t.TempDir()
	if f := checkChain(root, "/elsewhere/ws"); f != "" {
		t.Fatalf("empty root: %s", f)
	}
	if err := os.WriteFile(filepath.Join(root, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if f := checkChain(root, "/elsewhere/ws"); f == "" {
		t.Fatal("non-empty root passed")
	}
}

func TestCheckEmpty(t *testing.T) {
	d := t.TempDir()
	empty := filepath.Join(d, "empty")
	full := filepath.Join(d, "full")
	dir := filepath.Join(d, "dir")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		ok   bool
	}{
		{filepath.Join(d, "absent"), true},
		{empty, true}, // a /dev/null mask
		{"/dev/null", true},
		{full, false},
		{dir, true},
	} {
		if got := checkEmpty(tc.path) == ""; got != tc.ok {
			t.Errorf("%s: ok=%v, want %v", tc.path, got, tc.ok)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if checkEmpty(dir) == "" {
		t.Error("non-empty directory passed")
	}
}

func TestCheckHidden(t *testing.T) {
	fsRoot := t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(fsRoot, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mk("/usr")
	mk("/home/dan/aur/.aur-curator/builds/b1/work/src")
	a := probeArgs{
		top:       []string{"home", "usr"},
		rootPath:  "/home/dan/aur",
		workspace: "/home/dan/aur/.aur-curator/builds/b1/work/src",
		repo:      "/srv/repo",
		hide:      []string{"/etc/aur-curator.jsonc"},
	}
	if f := checkHidden(fsRoot, a); f != "" {
		t.Fatalf("clean tree: %s", f)
	}

	mk("/mnt/btrfs_root")
	mk("/srv/repo/custom.db.tar.gz")
	mk("/etc")
	if err := os.WriteFile(filepath.Join(fsRoot, "/etc/aur-curator.jsonc"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := checkHidden(fsRoot, a)
	for _, want := range []string{"/mnt is visible", "/srv is visible", "/srv/repo lists", "/etc/aur-curator.jsonc is readable"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in %q", want, f)
		}
	}
}

func TestAbstractSockets(t *testing.T) {
	table := "Num       RefCount Protocol Flags    Type St Inode Path\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 1001 @/tmp/.X11-unix/X0\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 1002 /run/user/1000/bus\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 1003 @/tmp/.X11-unix/X0\n" +
		"0000000000000000: 00000002 00000000 00010000 0001 01 1004 @/org/freedesktop/systemd1/notify\n" +
		"0000000000000000: 00000003 00000000 00000000 0001 03 1005\n"
	p := filepath.Join(t.TempDir(), "unix")
	if err := os.WriteFile(p, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	got := abstractSockets(p)
	want := []string{"@/org/freedesktop/systemd1/notify", "@/tmp/.X11-unix/X0"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestParseProbeArgs(t *testing.T) {
	a, err := parseProbeArgs([]string{"-root", "/r", "-workspace", "/r/ws", "-repo", "/repo", "-marker", "m",
		"-top", "usr", "-top", "etc", "-env", "A=1", "-hide", "/c", "-skip-socket", "/run/s"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.top, []string{"usr", "etc"}) || !slices.Equal(a.env, []string{"A=1"}) ||
		!slices.Equal(a.hide, []string{"/c"}) || !slices.Equal(a.skip, []string{"/run/s"}) {
		t.Fatalf("parsed %+v", a)
	}
	if _, err := parseProbeArgs([]string{"-root", "/r"}); err == nil {
		t.Fatal("missing arguments accepted")
	}
	if _, err := parseProbeArgs([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestProbeBadArgs(t *testing.T) {
	var out strings.Builder
	if code := Probe([]string{"-root"}, &out); code != 2 || !strings.HasPrefix(out.String(), "fail probe: ") {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestExecUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"makepkg"}, {"--"}, {"x", "makepkg"}} {
		if err := Exec(args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%q: %v", args, err)
		}
	}
	if err := Exec([]string{"--", "no-such-command-aur-curator"}); err == nil || !strings.Contains(err.Error(), "find") {
		t.Errorf("missing command: %v", err)
	}
}
