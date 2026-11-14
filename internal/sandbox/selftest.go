package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/procgroup"
)

// Targets are the host paths the self-test proves hidden.
type Targets struct {
	RootPath       string
	RepositoryPath string
	// Hidden are files that must not be readable inside: the config
	// file, activityLogPath.
	Hidden []string
	// SkipSockets are sockets allowed to accept: the DNS bind-back.
	SkipSockets []string
}

// Result is one check's outcome.
type Result struct {
	Check  string
	OK     bool
	Detail string
}

// Report is what the self-test found: one Result per check, in order,
// and informational lines (abstract sockets, markers not written).
type Report struct {
	Results []Result
	Info    []string
}

// Failed returns the failing results.
func (r Report) Failed() []Result {
	var f []Result
	for _, res := range r.Results {
		if !res.OK {
			f = append(f, res)
		}
	}
	return f
}

// SelfTest runs the probe in a sandbox built from p (the build's own
// policy) and checks on the host that its writes into rootPath and
// repositoryPath did not land. The error is non-nil when the sandbox
// could not start or any check failed; it names every failing check.
// The Report holds whatever was found either way.
func SelfTest(ctx context.Context, p Policy, t Targets) (Report, error) {
	marker := fmt.Sprintf(".aur-curator-selftest-%d-%d", os.Getpid(), time.Now().UnixNano())
	args := []string{ProbeArg,
		"-root", t.RootPath,
		"-workspace", p.Workspace,
		"-repo", t.RepositoryPath,
		"-marker", marker,
	}
	for _, n := range TopLevel(p) {
		args = append(args, "-top", n)
	}
	for _, h := range t.Hidden {
		args = append(args, "-hide", h)
	}
	for _, e := range ExpectedEnv(p) {
		args = append(args, "-env", e)
	}
	for _, s := range t.SkipSockets {
		args = append(args, "-skip-socket", s)
	}
	if !p.Network {
		args = append(args, "-offline")
	}

	cmd, err := Command(ctx, p, p.Helper, args...)
	if err != nil {
		return Report{}, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := procgroup.Run(ctx, cmd, "to", "subprocess", "from", "sandbox", "cmd", "bwrap", "probe", "selftest")

	return finishReport(stdout.String(), stderr.String(), runErr, marker, t, !p.Network)
}

// finishReport turns the probe's output into the self-test's result:
// "start" when the sandbox never ran, "no result" for a check the probe
// did not report, then the host-side writes check. offline: the sandbox
// had no network, so the probe owes the network check too.
func finishReport(stdout, stderr string, runErr error, marker string, t Targets, offline bool) (Report, error) {
	rep := parseReport(stdout)
	if runErr != nil && len(rep.Results) == 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = runErr.Error()
		}
		rep.Results = []Result{{Check: "start", Detail: oneLine(detail)}}
		return rep, fmt.Errorf("sandbox self-test: the sandbox did not start: %s", oneLine(detail))
	}
	for _, c := range probeChecks(offline) {
		if !slices.ContainsFunc(rep.Results, func(r Result) bool { return r.Check == c }) {
			rep.Results = append(rep.Results, Result{Check: c, Detail: "no result from the probe"})
		}
	}
	rep.Results = append(rep.Results, checkWrites(marker, t.RootPath, t.RepositoryPath))

	if failed := rep.Failed(); len(failed) > 0 {
		names := make([]string, len(failed))
		for i, f := range failed {
			names[i] = f.Check
		}
		return rep, fmt.Errorf("sandbox self-test failed: %s", strings.Join(names, ", "))
	}
	return rep, nil
}

// ExpectedEnv is the environment a command sees inside a sandbox
// built from p: p.Env, plus PWD, which bwrap sets to the --chdir
// directory itself after --clearenv.
func ExpectedEnv(p Policy) []string {
	return append(slices.Clone(p.Env), "PWD="+p.Workspace)
}

// parseReport reads the probe's lines. Unknown lines are kept as info,
// so nothing the probe says is lost.
func parseReport(out string) Report {
	var r Report
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "ok "):
			r.Results = append(r.Results, Result{Check: strings.TrimPrefix(line, "ok "), OK: true})
		case strings.HasPrefix(line, "fail "):
			name, detail, _ := strings.Cut(strings.TrimPrefix(line, "fail "), ": ")
			r.Results = append(r.Results, Result{Check: name, Detail: detail})
		case strings.HasPrefix(line, "info "):
			r.Info = append(r.Info, strings.TrimPrefix(line, "info "))
		case strings.TrimSpace(line) != "":
			r.Info = append(r.Info, "probe: "+line)
		}
	}
	return r
}

// checkWrites fails if the probe's marker reached rootPath or
// repositoryPath on the host. A marker found is removed.
func checkWrites(marker string, dirs ...string) Result {
	var landed []string
	for _, d := range dirs {
		p := filepath.Join(d, marker)
		if _, err := os.Lstat(p); err == nil {
			landed = append(landed, d)
			_ = os.Remove(p)
		} else if !errors.Is(err, os.ErrNotExist) {
			landed = append(landed, d+" (cannot tell: "+err.Error()+")")
		}
	}
	if len(landed) > 0 {
		return Result{Check: CheckWrites, Detail: "a write from inside reached " + strings.Join(landed, ", ")}
	}
	return Result{Check: CheckWrites, OK: true}
}

// TopLevel returns the entries / may hold inside a sandbox built from
// p: the system trees, the symlinks, the fresh and scratch mounts, and
// the first component of every path the policy adds.
func TopLevel(p Policy) []string {
	var names []string
	add := func(path string) {
		first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if first != "" && !slices.Contains(names, first) {
			names = append(names, first)
		}
	}
	for _, t := range systemTrees {
		add(t)
	}
	for _, s := range symlinks {
		add(s[0])
	}
	for _, t := range append([]string{"/dev", "/proc"}, scratch...) {
		add(t)
	}
	add(p.Home)
	add(p.Workspace)
	add(p.Helper)
	for _, b := range p.ReadOnly {
		add(b.Dest)
	}
	for _, m := range p.Masks {
		if inSystemTree(m.Path) {
			add(m.Path)
		}
	}
	slices.Sort(names)
	return names
}
