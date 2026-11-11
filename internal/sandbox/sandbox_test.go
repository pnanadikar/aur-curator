package sandbox

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func basePolicy() Policy {
	return Policy{
		Workspace:         "/home/dan/aur/.aur-curator/builds/b1/work/src",
		WorkspaceWritable: true,
		Home:              "/home/dan",
		Masks: []Mask{
			{Path: "/home/dan/.config/aur-curator/config.jsonc"},
			{Path: "/srv/repo", Dir: true},
		},
		ReadOnly: []Bind{
			{Src: "/home/dan/.makepkg.conf", Dest: "/home/dan/.makepkg.conf"},
		},
		Helper:  "/home/dan/.local/bin/aur-curator",
		Env:     []string{"PATH=/usr/bin", "HOME=/home/dan", "LANG=en_US.UTF-8"},
		Network: true,
	}
}

// The whole argument list for a typical build. A change to the sandbox
// shows up here as a diff to review, never silently.
func TestArgvGolden(t *testing.T) {
	got := Argv(basePolicy(), "makepkg", "--noconfirm", "--nocolor")
	want := []string{
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc", "/etc",
		"--ro-bind", "/opt", "/opt",
		"--ro-bind", "/sys", "/sys",
		"--ro-bind", "/var/lib/pacman", "/var/lib/pacman",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib", "/lib64",
		"--symlink", "usr/bin", "/sbin",
		"--dev", "/dev", "--proc", "/proc",
		"--tmpfs", "/tmp", "--tmpfs", "/var/tmp", "--tmpfs", "/run",
		"--tmpfs", "/home/dan",
		"--ro-bind", "/home/dan/.makepkg.conf", "/home/dan/.makepkg.conf",
		"--ro-bind", "/home/dan/.local/bin/aur-curator", "/home/dan/.local/bin/aur-curator",
		"--bind", "/home/dan/aur/.aur-curator/builds/b1/work/src", "/home/dan/aur/.aur-curator/builds/b1/work/src",
		"--chdir", "/home/dan/aur/.aur-curator/builds/b1/work/src",
		"--unshare-all", "--unshare-user", "--disable-userns",
		"--share-net",
		"--new-session", "--die-with-parent",
		"--clearenv",
		"--setenv", "PATH", "/usr/bin",
		"--setenv", "HOME", "/home/dan",
		"--setenv", "LANG", "en_US.UTF-8",
		"--", "/home/dan/.local/bin/aur-curator", "sandbox-exec", "--",
		"makepkg", "--noconfirm", "--nocolor",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv mismatch\n got: %q\nwant: %q", got, want)
	}
}

// hasPair reports whether flag is followed by val somewhere in a.
func hasPair(a []string, flag, val string) bool {
	for i := 0; i+1 < len(a); i++ {
		if a[i] == flag && a[i+1] == val {
			return true
		}
	}
	return false
}

func hasTriple(a []string, flag, v1, v2 string) bool {
	for i := 0; i+2 < len(a); i++ {
		if a[i] == flag && a[i+1] == v1 && a[i+2] == v2 {
			return true
		}
	}
	return false
}

// options returns the arguments before the first "--": bwrap's own.
func options(a []string) []string {
	if i := slices.Index(a, "--"); i >= 0 {
		return a[:i]
	}
	return a
}

func TestArgvNoRootBind(t *testing.T) {
	a := options(Argv(basePolicy(), "true"))
	for i := 0; i+1 < len(a); i++ {
		if (a[i] == "--ro-bind" || a[i] == "--bind") && a[i+1] == "/" {
			t.Fatalf("the root is bound (%s /); the filesystem must be an allowlist", a[i])
		}
	}
	for _, p := range []string{"/home", "/mnt", "/media", "/srv", "/root", "/var/log", "/var/cache"} {
		for i := 0; i+1 < len(a); i++ {
			if (a[i] == "--ro-bind" || a[i] == "--bind") && a[i+1] == p {
				t.Errorf("%s is bound; it must be absent", p)
			}
		}
	}
}

func TestArgvScratchTmpfs(t *testing.T) {
	a := options(Argv(basePolicy(), "true"))
	for _, p := range []string{"/tmp", "/var/tmp", "/run", "/home/dan"} {
		if !hasPair(a, "--tmpfs", p) {
			t.Errorf("missing --tmpfs %s", p)
		}
	}
}

func TestArgvWorkspace(t *testing.T) {
	ws := basePolicy().Workspace
	for _, tc := range []struct {
		name     string
		writable bool
		flag     string
		other    string
	}{
		{"build", true, "--bind", "--ro-bind"},
		{"printsrcinfo", false, "--ro-bind", "--bind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := basePolicy()
			p.WorkspaceWritable = tc.writable
			a := options(Argv(p, "true"))
			if !hasTriple(a, tc.flag, ws, ws) {
				t.Errorf("workspace not %s", tc.flag)
			}
			if hasTriple(a, tc.other, ws, ws) {
				t.Errorf("workspace also %s", tc.other)
			}
			if !hasPair(a, "--chdir", ws) {
				t.Error("no --chdir to the workspace")
			}
		})
	}
}

func TestArgvOnlyWorkspaceWritable(t *testing.T) {
	a := options(Argv(basePolicy(), "true"))
	n := 0
	for _, s := range a {
		if s == "--bind" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d writable binds; want only the workspace", n)
	}
}

func TestArgvNetwork(t *testing.T) {
	for _, on := range []bool{true, false} {
		p := basePolicy()
		p.Network = on
		a := options(Argv(p, "true"))
		if got := slices.Contains(a, "--share-net"); got != on {
			t.Errorf("Network=%v: --share-net present=%v", on, got)
		}
		if !slices.Contains(a, "--unshare-all") {
			t.Errorf("Network=%v: no --unshare-all", on)
		}
	}
}

func TestArgvNamespacesAndSession(t *testing.T) {
	a := options(Argv(basePolicy(), "true"))
	for _, f := range []string{"--unshare-all", "--unshare-user", "--disable-userns", "--new-session", "--die-with-parent", "--clearenv"} {
		if !slices.Contains(a, f) {
			t.Errorf("missing %s", f)
		}
	}
	// --disable-userns needs --unshare-user before it.
	if slices.Index(a, "--unshare-user") > slices.Index(a, "--disable-userns") {
		t.Error("--disable-userns before --unshare-user")
	}
}

func TestArgvEnvironmentExact(t *testing.T) {
	p := basePolicy()
	p.Env = []string{"A=1", "B=x=y", "EMPTY="}
	a := options(Argv(p, "true"))
	ci := slices.Index(a, "--clearenv")
	if ci < 0 {
		t.Fatal("no --clearenv")
	}
	var got []string
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "--setenv":
			if i < ci {
				t.Errorf("--setenv %s before --clearenv", a[i+1])
			}
			got = append(got, a[i+1]+"="+a[i+2])
			i += 2
		case "--unsetenv":
			t.Errorf("unexpected --unsetenv")
		}
	}
	if !reflect.DeepEqual(got, p.Env) {
		t.Fatalf("environment %q, want %q", got, p.Env)
	}
}

func TestArgvMasks(t *testing.T) {
	p := basePolicy()
	p.Masks = []Mask{
		{Path: "/etc/aur-curator.jsonc"}, // file in a system tree
		{Path: "/opt/repo", Dir: true},   // directory in a system tree
		{Path: "/home/dan/cfg.jsonc"},    // absent already (Home is a tmpfs)
		{Path: "/srv/repo", Dir: true},   // absent already (not added)
		{Path: "/usrlocal/x"},            // a prefix of /usr, not inside it
	}
	a := options(Argv(p, "true"))
	if !hasTriple(a, "--ro-bind", "/dev/null", "/etc/aur-curator.jsonc") {
		t.Error("file in /etc not covered by /dev/null")
	}
	if !hasPair(a, "--tmpfs", "/opt/repo") {
		t.Error("directory in /opt not covered by a tmpfs")
	}
	for _, s := range []string{"/home/dan/cfg.jsonc", "/srv/repo", "/usrlocal/x"} {
		if slices.Contains(a, s) {
			t.Errorf("%s mentioned; it is absent without a mask", s)
		}
	}
}

// Masks and binds land after the tmpfs mounts they may sit in, and the
// workspace comes after all of them: bwrap applies mounts in order.
func TestArgvOrder(t *testing.T) {
	p := basePolicy()
	p.Masks = []Mask{{Path: "/etc/x"}}
	p.ReadOnly = []Bind{{Src: "/host/key.kbx", Dest: "/home/dan/.gnupg/pubring.kbx"}}
	a := options(Argv(p, "true"))
	idx := func(want ...string) int {
		for i := 0; i+len(want) <= len(a); i++ {
			if slices.Equal(a[i:i+len(want)], want) {
				return i
			}
		}
		t.Fatalf("%q not found", want)
		return -1
	}
	home := idx("--tmpfs", "/home/dan")
	etc := idx("--ro-bind", "/etc", "/etc")
	mask := idx("--ro-bind", "/dev/null", "/etc/x")
	key := idx("--ro-bind", "/host/key.kbx", "/home/dan/.gnupg/pubring.kbx")
	helper := idx("--ro-bind", p.Helper, p.Helper)
	ws := idx("--bind", p.Workspace, p.Workspace)
	if mask < etc || key < home || helper < home || ws < home || ws < key {
		t.Fatalf("order: etc=%d mask=%d home=%d key=%d helper=%d ws=%d", etc, mask, home, key, helper, ws)
	}
}

func TestArgvCommandTail(t *testing.T) {
	p := basePolicy()
	a := Argv(p, "makepkg", "--printsrcinfo", "--")
	i := slices.Index(a, "--")
	want := []string{"--", p.Helper, ExecArg, "--", "makepkg", "--printsrcinfo", "--"}
	if !slices.Equal(a[i:], want) {
		t.Fatalf("tail %q, want %q", a[i:], want)
	}
}

func TestValidate(t *testing.T) {
	if err := basePolicy().Validate(); err != nil {
		t.Fatalf("base policy: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Policy)
		want   string
	}{
		{"empty workspace", func(p *Policy) { p.Workspace = "" }, "workspace is empty"},
		{"relative workspace", func(p *Policy) { p.Workspace = "builds/x" }, "not absolute"},
		{"unclean workspace", func(p *Policy) { p.Workspace = "/a/../b" }, "not clean"},
		{"root workspace", func(p *Policy) { p.Workspace = "/" }, "workspace is /"},
		{"root home", func(p *Policy) { p.Home = "/" }, "home is /"},
		{"no helper", func(p *Policy) { p.Helper = "" }, "helper is empty"},
		{"relative mask", func(p *Policy) { p.Masks = []Mask{{Path: "x"}} }, "mask"},
		{"unclean mask", func(p *Policy) { p.Masks = []Mask{{Path: "/var/lib/pacman/../x"}} }, "not clean"},
		{"relative bind", func(p *Policy) { p.ReadOnly = []Bind{{Src: "a", Dest: "/b"}} }, "bind source"},
		{"bad env", func(p *Policy) { p.Env = []string{"NOEQUALS"} }, "not NAME=value"},
		{"empty env name", func(p *Policy) { p.Env = []string{"=x"} }, "not NAME=value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := basePolicy()
			tc.mutate(&p)
			err := p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	p := basePolicy()
	cmd, err := Command(context.Background(), p, "makepkg", "--noconfirm")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.Args[1:], Argv(p, "makepkg", "--noconfirm")) {
		t.Fatalf("Args %q", cmd.Args)
	}
	if cmd.Env == nil || len(cmd.Env) != 0 {
		t.Fatalf("bwrap's own environment %q; want empty, not inherited", cmd.Env)
	}

	p.Workspace = "relative"
	if _, err := Command(context.Background(), p, "makepkg"); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if _, err := Command(context.Background(), basePolicy(), ""); err == nil {
		t.Fatal("empty command accepted")
	}
}
