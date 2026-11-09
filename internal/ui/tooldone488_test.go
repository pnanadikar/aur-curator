package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// TestToolDoneFailureToastsAndLogs: a failed tool's last stderr line is
// the toast; the error log keeps the whole of it (#488).
func TestToolDoneFailureToastsAndLogs(t *testing.T) {
	root := t.TempDir()
	a := App{deps: Dependencies{Config: domain.WorkspaceConfig{RootPath: root}}}

	m, cmd := a.toolDone(msgs.ToolDoneMsg{
		What: "Diff tool", Pkg: "foo", Err: errors.New("exit status 128"),
		Stderr: "This message is displayed because 'diff.tool' is not configured.\nNo known diff tool is available.\n",
	})
	got := m.(App)
	if got.toast != "Diff tool failed: No known diff tool is available." || cmd == nil {
		t.Errorf("toast %q, cmd %v", got.toast, cmd)
	}
	log, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[tool]", `Diff tool for "foo"`, "exit status 128", "diff.tool' is not configured"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("errors.log misses %q:\n%s", want, log)
		}
	}
}

// TestToolDoneFailureWithoutStderr: the error itself is the reason.
func TestToolDoneFailureWithoutStderr(t *testing.T) {
	a := App{deps: Dependencies{Config: domain.WorkspaceConfig{RootPath: t.TempDir()}}}
	m, _ := a.toolDone(msgs.ToolDoneMsg{What: "Pager", Pkg: "foo", Err: errors.New(`exec: "nosuch": executable file not found in $PATH`)})
	if got := m.(App).toast; got != `Pager failed: exec: "nosuch": executable file not found in $PATH` {
		t.Errorf("toast = %q", got)
	}
}

// TestToolDoneSuccessIsSilent: a tool that exited cleanly needs nothing.
func TestToolDoneSuccessIsSilent(t *testing.T) {
	root := t.TempDir()
	a := App{deps: Dependencies{Config: domain.WorkspaceConfig{RootPath: root}}}
	m, cmd := a.toolDone(msgs.ToolDoneMsg{What: "Pager", Pkg: "foo", Stderr: "chatter"})
	if m.(App).toast != "" || cmd != nil {
		t.Errorf("toast %q, cmd %v", m.(App).toast, cmd)
	}
	if _, err := os.Stat(filepath.Join(root, ".aur-curator", "errors.log")); !os.IsNotExist(err) {
		t.Error("errors.log written for a successful tool")
	}
}
