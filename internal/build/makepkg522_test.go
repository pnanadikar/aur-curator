package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostIfaces counts the interfaces in the host's /proc/net/dev, as the
// fake makepkg counts them inside.
func hostIfaces(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(b), ":")
	if n < 2 {
		t.Skipf("the host has %d interface(s): cannot tell its network from a sandbox's", n)
	}
	return n
}

func (f fixture) buildWith(t *testing.T, opts BuildOptions) (string, error) {
	t.Helper()
	_, err := f.builder.Build(context.Background(), f.ws, f.logPath, opts)
	log, readErr := os.ReadFile(f.logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	return string(log), err
}

// failRun makes the fake makepkg fail the run whose first argument is arg.
func (f fixture) failRun(t *testing.T, arg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.ws, "fail"+arg), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

const wantNoNetworkHint = "aur-curator: this build had no network (#522); if the package needs it, check why in the PKGBUILD, then add yay to buildNetwork in the config"

// TestMakepkgBuilder522_BuildNetwork: a package in buildNetwork has the
// host's network in every run, and the log says so.
func TestMakepkgBuilder522_BuildNetwork(t *testing.T) {
	f := newFixture(t)
	host := hostIfaces(t)
	log, err := f.buildWith(t, BuildOptions{Package: "yay", Network: true})
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, log)
	}
	for _, arg := range []string{"--printsrcinfo", "--verifysource", "--holdver"} {
		if want := fmt.Sprintf("RUN %s IFACES=%d", arg, host); !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if lines := strings.Split(log, "\n"); lines[1] != "aur-curator: network: for the whole build (buildNetwork)" {
		t.Errorf("log line 2 = %q", lines[1])
	}
	if strings.Contains(log, "building without network") {
		t.Errorf("download line for a buildNetwork package:\n%s", log)
	}
}

// TestMakepkgBuilder522_BuildFailsOffline: a build without the network
// that fails says so and names buildNetwork, in the log and at the end
// of the error, whatever the failure was.
func TestMakepkgBuilder522_BuildFailsOffline(t *testing.T) {
	f := newFixture(t)
	f.failRun(t, "--holdver")
	log, err := f.buildWith(t, BuildOptions{Package: "yay"})
	if err == nil {
		t.Fatalf("Build succeeded:\n%s", log)
	}
	if !strings.Contains(log, wantNoNetworkHint+"\n") {
		t.Errorf("log lacks the hint:\n%s", log)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "makepkg failed: ") || !strings.Contains(msg, "FAKE FAILURE in --holdver") ||
		!strings.HasSuffix(msg, "; "+strings.TrimPrefix(wantNoNetworkHint, "aur-curator: ")) {
		t.Errorf("error = %q", msg)
	}
}

// TestMakepkgBuilder522_NoHint: no hint when the package had the network,
// nor when the download failed (it had the network).
func TestMakepkgBuilder522_NoHint(t *testing.T) {
	t.Run("buildNetwork", func(t *testing.T) {
		f := newFixture(t)
		f.failRun(t, "--holdver")
		log, err := f.buildWith(t, BuildOptions{Package: "yay", Network: true})
		if err == nil || strings.Contains(err.Error(), "#522") || strings.Contains(log, "this build had no network") {
			t.Errorf("err = %v\n%s", err, log)
		}
	})
	t.Run("download", func(t *testing.T) {
		f := newFixture(t)
		f.failRun(t, "--verifysource")
		log, err := f.buildWith(t, BuildOptions{Package: "yay"})
		if err == nil || !strings.HasPrefix(err.Error(), "makepkg failed downloading the sources: ") ||
			!strings.Contains(err.Error(), "FAKE FAILURE in --verifysource") {
			t.Fatalf("err = %v\n%s", err, log)
		}
		if strings.Contains(err.Error(), "#522") || strings.Contains(log, "this build had no network") {
			t.Errorf("hint after a download failure: %v\n%s", err, log)
		}
		if strings.Contains(log, "RUN --holdver") || strings.Contains(log, "building without network") {
			t.Errorf("the build ran after a failed download:\n%s", log)
		}
	})
}

// TestNoNetworkHint: the exact text (domain.md § Build Environment).
func TestNoNetworkHint(t *testing.T) {
	if got := "aur-curator: " + noNetworkHint("yay"); got != wantNoNetworkHint {
		t.Errorf("hint = %q", got)
	}
	if networkLogLine(false) != "aur-curator: network: for downloading only" ||
		networkLogLine(true) != "aur-curator: network: for the whole build (buildNetwork)" {
		t.Errorf("network lines %q, %q", networkLogLine(false), networkLogLine(true))
	}
}

// TestMakepkgBuilder522_SrcinfoFailsOffline: --printsrcinfo without the
// network that fails gets the hint too: top-level code that fetches can
// leave the PKGBUILD unreadable (tor-browser-bin). With the network, no
// hint.
func TestMakepkgBuilder522_SrcinfoFailsOffline(t *testing.T) {
	f := newFixture(t)
	f.failRun(t, "--printsrcinfo")
	log, err := f.buildWith(t, BuildOptions{Package: "yay"})
	if err == nil {
		t.Fatalf("Build succeeded:\n%s", log)
	}
	if !strings.Contains(log, wantNoNetworkHint+"\n") {
		t.Errorf("log lacks the hint:\n%s", log)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "makepkg --printsrcinfo failed: ") || !strings.HasSuffix(msg, "; "+strings.TrimPrefix(wantNoNetworkHint, "aur-curator: ")) {
		t.Errorf("error = %q", msg)
	}

	f = newFixture(t)
	f.failRun(t, "--printsrcinfo")
	log, err = f.buildWith(t, BuildOptions{Package: "yay", Network: true})
	if err == nil || strings.Contains(err.Error(), "#522") || strings.Contains(log, "this build had no network") {
		t.Errorf("hint with buildNetwork: %v\n%s", err, log)
	}
}
