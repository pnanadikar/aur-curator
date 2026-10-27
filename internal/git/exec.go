package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pramod/aur-curator/internal/procgroup"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/trace"
)

// ExecRunner executes real git commands using os/exec.
type ExecRunner struct{}

// Run executes git with the supplied arguments inside dir.
// Git spawns child processes (git-remote-https, curl) that exec.CommandContext
// does not kill. procgroup.Run puts the process in its own group and kills the
// entire group when the context is cancelled.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	trace.Log(ctx, "handoff", "to", "subprocess", "from", "runner", "cmd", args)

	err := procgroup.Run(ctx, cmd, "to", "subprocess", "from", "runner", "cmd", args)

	if err != nil {
		trace.Log(ctx, "handoff", "to", "caller", "from", "runner", "cmd", args, "status", "failed", "stderr", stderr.String())
		// A process killed because ctx ended says only "signal: killed";
		// Interrupted says it timed out (and after how long) or was
		// cancelled, and keeps ctx.Err() for errors.Is (#417).
		err = Interrupted(ctx, err)
		if msg := strings.TrimSpace(sanitize.ForMessage(stderr.String())); msg != "" {
			return "", fmt.Errorf("git %v: %w: %s", args, err, msg)
		}
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	race := "cmd"
	if ctx.Err() != nil {
		race = "ctx"
	}
	trace.Log(ctx, "handoff", "to", "caller", "from", "runner", "cmd", args, "status", "ok", "race", race)
	return stdout.String(), nil
}
