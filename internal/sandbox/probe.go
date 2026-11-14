package sandbox

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The self-test's checks, in the order the probe reports them
// (domain.md § Build Environment, Sequence 1). writes is checked on the
// host by SelfTest.
const (
	CheckSockets = "sockets"
	CheckKeyring = "keyring"
	CheckNNP     = "nnp"
	CheckUserns  = "userns"
	CheckEnv     = "env"
	CheckHidden  = "hidden"
	CheckNetwork = "network"
	CheckWrites  = "writes"
)

// probeChecks are the checks the probe must report, each exactly once:
// network only in a sandbox without the network (#522).
func probeChecks(offline bool) []string {
	c := []string{CheckSockets, CheckKeyring, CheckNNP, CheckUserns, CheckEnv, CheckHidden}
	if offline {
		c = append(c, CheckNetwork)
	}
	return c
}

// socketWalkExclude are trees the sockets walk skips: the read-only
// system files and the kernel's virtual filesystems.
var socketWalkExclude = []string{"/usr", "/proc", "/sys"}

// probeArgs is what the host tells the probe to expect.
type probeArgs struct {
	top       []string // allowed entries of /
	rootPath  string
	workspace string
	repo      string
	hide      []string // must be absent or read as empty
	env       []string // the exact environment, NAME=value
	skip      []string // sockets allowed to accept (the DNS bind-back)
	marker    string   // file name the probe writes into rootPath and repo
	offline   bool     // the sandbox has no network: check it (#522)
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func parseProbeArgs(args []string) (probeArgs, error) {
	var a probeArgs
	var top, hide, env, skip multi
	fsx := flag.NewFlagSet(ProbeArg, flag.ContinueOnError)
	fsx.SetOutput(io.Discard)
	fsx.Var(&top, "top", "allowed entry of /")
	fsx.StringVar(&a.rootPath, "root", "", "rootPath")
	fsx.StringVar(&a.workspace, "workspace", "", "the workspace")
	fsx.StringVar(&a.repo, "repo", "", "repositoryPath")
	fsx.Var(&hide, "hide", "path that must not be readable")
	fsx.Var(&env, "env", "expected NAME=value")
	fsx.Var(&skip, "skip-socket", "socket allowed to accept")
	fsx.StringVar(&a.marker, "marker", "", "marker file name")
	fsx.BoolVar(&a.offline, "offline", false, "the sandbox has no network")
	if err := fsx.Parse(args); err != nil {
		return a, err
	}
	a.top, a.hide, a.env, a.skip = top, hide, env, skip
	if a.rootPath == "" || a.workspace == "" || a.repo == "" || a.marker == "" || len(a.top) == 0 {
		return a, errors.New("missing -root, -workspace, -repo, -marker or -top")
	}
	return a, nil
}

// Probe is sandbox-probe: it runs inside the sandbox and writes one
// line per check to out, "ok <check>" or "fail <check>: <detail>", plus
// "info <topic>: <detail>" lines. It returns the exit status: 0 when it
// ran its checks (whatever they found), 2 on bad arguments.
func Probe(args []string, out io.Writer) int {
	a, err := parseProbeArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(out, "fail probe: %v\n", err)
		return 2
	}

	report := func(name, failure string) {
		if failure == "" {
			_, _ = fmt.Fprintf(out, "ok %s\n", name)
		} else {
			_, _ = fmt.Fprintf(out, "fail %s: %s\n", name, oneLine(failure))
		}
	}

	report(CheckSockets, checkSockets("/", socketWalkExclude, a.skip))
	report(CheckKeyring, checkKeyrings())
	report(CheckNNP, checkNNP("/proc/self/status"))
	report(CheckUserns, checkUserns())
	report(CheckEnv, checkEnv(os.Environ(), a.env))
	report(CheckHidden, checkHidden("/", a))

	// Abstract sockets belong to the network namespace: without the
	// network, any is a failure; with it, they are listed (#522).
	abs := abstractSockets("/proc/net/unix")
	if a.offline {
		ifaces, err := interfaceNames()
		report(CheckNetwork, checkNetwork(ifaces, err, abs))
	} else if len(abs) > 0 {
		_, _ = fmt.Fprintf(out, "info abstract sockets reachable: %s\n", strings.Join(abs, " "))
	}
	for _, dir := range []string{a.rootPath, a.repo} {
		if err := writeMarker(dir, a.marker); err != nil {
			_, _ = fmt.Fprintf(out, "info marker in %s not written: %s\n", dir, oneLine(err.Error()))
		}
	}
	return 0
}

// checkSockets walks root, skipping exclude, and tries to connect to
// every Unix socket outside skip. Any socket that accepts is a failure.
func checkSockets(root string, exclude, skip []string) string {
	var open []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: nothing to connect to through it
		}
		if d.IsDir() && slices.Contains(exclude, path) {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSocket == 0 || slices.Contains(skip, path) {
			return nil
		}
		c, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			_ = c.Close()
			open = append(open, path)
		}
		return nil
	})
	if len(open) > 0 {
		return "accepts a connection: " + strings.Join(open, " ")
	}
	return ""
}

// checkKeyrings fails if the session or user keyring holds a key.
// KEYCTL_READ on a keyring returns its keys' serial numbers.
func checkKeyrings() string {
	var bad []string
	for _, k := range []struct {
		name string
		id   int
	}{{"session", unix.KEY_SPEC_SESSION_KEYRING}, {"user", unix.KEY_SPEC_USER_KEYRING}} {
		buf := make([]byte, 4096)
		n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, k.id, buf, 0)
		switch {
		case errors.Is(err, unix.ENOKEY):
		case err != nil:
			bad = append(bad, fmt.Sprintf("%s keyring unreadable: %v", k.name, err))
		case n > 0:
			bad = append(bad, fmt.Sprintf("%s keyring holds %d key(s)", k.name, n/4))
		}
	}
	return strings.Join(bad, "; ")
}

// checkNNP fails unless the status file says NoNewPrivs: 1.
func checkNNP(statusPath string) string {
	f, err := os.Open(statusPath)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "NoNewPrivs:"); ok {
			if strings.TrimSpace(v) == "1" {
				return ""
			}
			return "NoNewPrivs is " + strings.TrimSpace(v)
		}
	}
	return "no NoNewPrivs line"
}

// checkUserns fails if a child can be started in a new user namespace.
// The probe cannot unshare itself (a Go program has several threads,
// which unshare(CLONE_NEWUSER) refuses for its own reasons), so it
// starts a child with CLONE_NEWUSER; the child is its own binary, which
// certainly exists, so a failure to start is the namespace refused.
func checkUserns() string {
	cmd := exec.Command("/proc/self/exe") //nolint:gosec // this binary; it never runs if the check passes
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWUSER}
	err := cmd.Start()
	if err == nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "a user namespace was created"
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "could not test: " + err.Error()
	}
	return ""
}

// checkEnv fails unless environ is exactly want, in any order.
func checkEnv(environ, want []string) string {
	got := slices.Sorted(slices.Values(environ))
	exp := slices.Sorted(slices.Values(want))
	if slices.Equal(got, exp) {
		return ""
	}
	var extra, missing []string
	for _, e := range got {
		if !slices.Contains(exp, e) {
			name, _, _ := strings.Cut(e, "=")
			extra = append(extra, name)
		}
	}
	for _, e := range exp {
		if !slices.Contains(got, e) {
			name, _, _ := strings.Cut(e, "=")
			missing = append(missing, name)
		}
	}
	return fmt.Sprintf("unexpected %v, missing or different %v", extra, missing)
}

// checkHidden checks, under fsRoot: / holds only the allowed entries;
// rootPath holds only the path down to the workspace; repositoryPath
// and every hide path are absent or empty.
func checkHidden(fsRoot string, a probeArgs) string {
	var bad []string
	at := func(p string) string { return filepath.Join(fsRoot, p) }

	if entries, err := os.ReadDir(at("/")); err != nil {
		bad = append(bad, "read /: "+err.Error())
	} else {
		for _, e := range entries {
			if !slices.Contains(a.top, e.Name()) {
				bad = append(bad, "/"+e.Name()+" is visible")
			}
		}
	}

	if f := checkChain(at(a.rootPath), at(a.workspace)); f != "" {
		bad = append(bad, f)
	}
	for _, p := range append([]string{a.repo}, a.hide...) {
		if f := checkEmpty(at(p)); f != "" {
			bad = append(bad, p+" "+f)
		}
	}
	return strings.Join(bad, "; ")
}

// checkChain fails unless each directory from root down to the
// workspace's parent holds only the next directory on the way. A
// workspace outside root leaves root to be empty.
func checkChain(root, workspace string) string {
	rel, err := filepath.Rel(root, workspace)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		if f := checkEmpty(root); f != "" {
			return "rootPath " + f
		}
		return ""
	}
	dir := root
	for _, next := range strings.Split(rel, string(filepath.Separator)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "read " + dir + ": " + err.Error()
		}
		for _, e := range entries {
			if e.Name() != next {
				return filepath.Join(dir, e.Name()) + " is visible"
			}
		}
		dir = filepath.Join(dir, next)
	}
	return ""
}

// checkEmpty fails if path exists with content: a non-empty directory,
// or a file that reads any byte (a /dev/null mask reads none).
func checkEmpty(path string) string {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		return "" // unreadable is hidden
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) > 0 {
			return fmt.Sprintf("lists %d entries", len(entries))
		}
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if n, _ := f.Read(make([]byte, 1)); n > 0 {
		return "is readable"
	}
	return ""
}

// interfaceNames lists the network interfaces the process can see.
func interfaceNames() ([]string, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(ifs))
	for i, f := range ifs {
		names[i] = f.Name
	}
	return names, nil
}

// checkNetwork fails unless the only interface is lo and no abstract
// Unix socket is reachable: a network namespace of the sandbox's own
// (#522). An interface list that cannot be read is a failure.
func checkNetwork(ifaces []string, ifaceErr error, abstract []string) string {
	var bad []string
	if ifaceErr != nil {
		bad = append(bad, "interfaces unreadable: "+ifaceErr.Error())
	}
	var other []string
	for _, n := range ifaces {
		if n != "lo" {
			other = append(other, n)
		}
	}
	if len(other) > 0 {
		bad = append(bad, "interfaces besides lo: "+strings.Join(other, " "))
	}
	if len(abstract) > 0 {
		bad = append(bad, "abstract sockets reachable: "+strings.Join(abstract, " "))
	}
	return strings.Join(bad, "; ")
}

// abstractSockets lists the abstract Unix socket names in a
// /proc/net/unix table: the eighth column, starting with '@'.
func abstractSockets(table string) []string {
	f, err := os.Open(table)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var names []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 8 && strings.HasPrefix(fields[7], "@") && !slices.Contains(names, fields[7]) {
			names = append(names, fields[7])
		}
	}
	slices.Sort(names)
	return names
}

// writeMarker tries to write a marker file into dir from inside the
// sandbox. Whether it lands on the host is SelfTest's check.
func writeMarker(dir, name string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte("aur-curator self-test\n"), 0o600)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
