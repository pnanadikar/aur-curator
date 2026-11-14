package sandbox

import (
	"errors"
	"strings"
	"testing"
)

// TestCheckNetwork: only lo and no abstract socket passes (#522).
func TestCheckNetwork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ifaces   []string
		err      error
		abstract []string
		want     []string // substrings of the failure; none means pass
	}{
		{"lo only", []string{"lo"}, nil, nil, nil},
		{"no interface", nil, nil, nil, nil},
		{"host interfaces", []string{"lo", "eth0", "wlan0"}, nil, nil, []string{"interfaces besides lo: eth0 wlan0"}},
		{"abstract socket", []string{"lo"}, nil, []string{"@/tmp/.X11-unix/X0"}, []string{"abstract sockets reachable: @/tmp/.X11-unix/X0"}},
		{"unreadable", nil, errors.New("boom"), nil, []string{"interfaces unreadable: boom"}},
		{"both", []string{"eth0"}, nil, []string{"@a", "@b"}, []string{"eth0", "@a @b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkNetwork(tc.ifaces, tc.err, tc.abstract)
			if len(tc.want) == 0 {
				if got != "" {
					t.Fatalf("failure %q, want pass", got)
				}
				return
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("failure %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

// TestFinishReport522: without the network the probe owes the network
// check; with it, it does not (#522).
func TestFinishReport522(t *testing.T) {
	tg := Targets{RootPath: t.TempDir(), RepositoryPath: t.TempDir()}
	six := "ok sockets\nok keyring\nok nnp\nok userns\nok env\nok hidden\n"

	rep, err := finishReport(six, "", nil, "m", tg, true)
	if err == nil || !strings.Contains(err.Error(), CheckNetwork) {
		t.Fatalf("offline, no network result: err = %v", err)
	}
	if f := rep.Failed(); len(f) != 1 || f[0].Check != CheckNetwork || f[0].Detail != "no result from the probe" {
		t.Fatalf("offline, no network result: failed %+v", f)
	}

	if _, err := finishReport(six+"ok network\n", "", nil, "m", tg, true); err != nil {
		t.Fatalf("offline, network ok: %v", err)
	}
	if _, err := finishReport(six+"fail network: interfaces besides lo: eth0\n", "", nil, "m", tg, true); err == nil || !strings.Contains(err.Error(), CheckNetwork) {
		t.Fatalf("offline, network failed: err = %v", err)
	}

	rep, err = finishReport(six, "", nil, "m", tg, false)
	if err != nil {
		t.Fatalf("networked: %v", err)
	}
	for _, r := range rep.Results {
		if r.Check == CheckNetwork {
			t.Fatalf("networked: unexpected network result %+v", r)
		}
	}
}

// TestParseProbeArgs522: -offline is off unless given.
func TestParseProbeArgs522(t *testing.T) {
	base := []string{"-root", "/r", "-workspace", "/r/ws", "-repo", "/repo", "-marker", "m", "-top", "usr"}
	a, err := parseProbeArgs(base)
	if err != nil || a.offline {
		t.Fatalf("without -offline: %+v %v", a, err)
	}
	a, err = parseProbeArgs(append(base, "-offline"))
	if err != nil || !a.offline {
		t.Fatalf("with -offline: %+v %v", a, err)
	}
}
