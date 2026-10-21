// Package procgroup runs subprocesses in their own process group so a
// cancelled context stops the whole process tree, not just the direct child.
//
// Linux-only (syscall.Kill on a negative pgid).
package procgroup

import (
	"context"
	"os/exec"
	"sync"
	"syscall"

	"github.com/pramod/aur-curator/internal/trace"
)

// signalKV returns a fresh kv slice: the caller's base pairs followed by
// this signal attempt's pairs. Never appends onto base (slice aliasing),
// and never reuses a previous signal line's pairs.
func signalKV(base []any, sig, status string, pid int, err error) []any {
	kv := make([]any, 0, len(base)+10)
	kv = append(kv, base...)
	kv = append(kv, "sig", sig, "target", "pgid", "status", status, "pid", pid)
	if err != nil {
		kv = append(kv, "err", err)
	}
	return kv
}

// Run starts cmd in its own process group and waits for it to finish.
// If ctx is cancelled first, the whole group gets SIGTERM (SIGKILL if
// the SIGTERM syscall fails). cmd must not have been started; Run sets
// cmd.SysProcAttr.Setpgid. traceKV are key/value pairs (even count)
// identifying the command in trace output; Run appends its own
// "sig"/"target"/"status"/"pid"(/"err") pairs to them.
func Run(ctx context.Context, cmd *exec.Cmd, traceKV ...any) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start before launching the watcher: cmd.Process is written by
	// Start, so the watcher must not run until Start has returned
	// (reading it concurrently is a data race, caught by go test -race).
	if err := cmd.Start(); err != nil {
		return err
	}
	pgid := cmd.Process.Pid

	var wg sync.WaitGroup
	cmdDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
				trace.Log(ctx, "handoff", signalKV(traceKV, "SIGTERM", "failed", pgid, err)...)
				if err2 := syscall.Kill(-pgid, syscall.SIGKILL); err2 != nil {
					trace.Log(ctx, "handoff", signalKV(traceKV, "SIGKILL", "failed", pgid, err2)...)
				} else {
					trace.Log(ctx, "handoff", signalKV(traceKV, "SIGKILL", "ok", pgid, nil)...)
				}
			} else {
				trace.Log(ctx, "handoff", signalKV(traceKV, "SIGTERM", "ok", pgid, nil)...)
			}
		case <-cmdDone:
		}
	}()

	err := cmd.Wait()
	close(cmdDone)
	wg.Wait()

	return err
}
