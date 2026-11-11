package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseReport(t *testing.T) {
	r := parseReport("ok sockets\nfail keyring: session keyring holds 1 key(s)\ninfo abstract sockets reachable: @x\n\nstray\n")
	want := []Result{{Check: "sockets", OK: true}, {Check: "keyring", Detail: "session keyring holds 1 key(s)"}}
	if !slices.Equal(r.Results, want) {
		t.Fatalf("results %+v", r.Results)
	}
	if !slices.Equal(r.Info, []string{"abstract sockets reachable: @x", "probe: stray"}) {
		t.Fatalf("info %q", r.Info)
	}
	if f := r.Failed(); len(f) != 1 || f[0].Check != "keyring" {
		t.Fatalf("failed %+v", f)
	}
}

func TestFinishReport(t *testing.T) {
	tg := Targets{RootPath: t.TempDir(), RepositoryPath: t.TempDir()}
	all := "ok sockets\nok keyring\nok nnp\nok userns\nok env\nok hidden\n"

	rep, err := finishReport(all, "", nil, "m", tg)
	if err != nil || len(rep.Results) != 7 || len(rep.Failed()) != 0 {
		t.Fatalf("all ok: %v %+v", err, rep)
	}

	// A check the probe never reported fails: a crash midway is not a pass.
	rep, err = finishReport("ok sockets\nok keyring\n", "", errors.New("exit status 2"), "m", tg)
	if err == nil || !strings.Contains(err.Error(), "nnp") || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("missing checks: %v", err)
	}
	for _, f := range rep.Failed() {
		if f.Detail != "no result from the probe" {
			t.Errorf("%s: %q", f.Check, f.Detail)
		}
	}

	// Nothing at all from the probe and an error: the sandbox did not start.
	rep, err = finishReport("", "bwrap: No permissions to create new namespace\n", errors.New("exit status 1"), "m", tg)
	if err == nil || !strings.Contains(err.Error(), "No permissions") || rep.Results[0].Check != "start" {
		t.Fatalf("no start: %v %+v", err, rep)
	}
}

func TestCheckWrites(t *testing.T) {
	root, repo := t.TempDir(), t.TempDir()
	if r := checkWrites("m", root, repo); !r.OK {
		t.Fatalf("nothing landed: %+v", r)
	}
	landed := filepath.Join(repo, "m")
	if err := os.WriteFile(landed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := checkWrites("m", root, repo)
	if r.OK || !strings.Contains(r.Detail, repo) {
		t.Fatalf("landed write not reported: %+v", r)
	}
	if _, err := os.Stat(landed); !os.IsNotExist(err) {
		t.Fatal("landed marker not removed")
	}
}

func TestExpectedEnv(t *testing.T) {
	p := basePolicy()
	got := ExpectedEnv(p)
	want := append(slices.Clone(p.Env), "PWD="+p.Workspace)
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	if len(p.Env) != 3 {
		t.Fatal("ExpectedEnv changed the policy's environment")
	}
}

func TestTopLevel(t *testing.T) {
	p := basePolicy()
	p.Masks = []Mask{{Path: "/etc/x"}, {Path: "/srv/repo", Dir: true}}
	p.ReadOnly = append(p.ReadOnly, Bind{Src: "/x", Dest: "/run/systemd/resolve/stub-resolv.conf"})
	got := TopLevel(p)
	want := []string{"bin", "dev", "etc", "home", "lib", "lib64", "opt", "proc", "run", "sbin", "sys", "tmp", "usr", "var"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
}

// With no bwrap on PATH the self-test fails, saying the sandbox did not
// start: there is no fallback.
func TestSelfTestNoBwrap(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rep, err := SelfTest(context.Background(), basePolicy(), Targets{RootPath: "/r", RepositoryPath: "/repo"})
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("err = %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Check != "start" || rep.Results[0].OK {
		t.Fatalf("report %+v", rep)
	}
}

func TestSelfTestInvalidPolicy(t *testing.T) {
	p := basePolicy()
	p.Home = "relative"
	if _, err := SelfTest(context.Background(), p, Targets{}); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
