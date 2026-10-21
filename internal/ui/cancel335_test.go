package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #335 — an OperationCancelledMsg is acted on only when it matches the
// active operation; a late or duplicate cancel must neither panic nor
// cancel a different operation.

// appWithActiveOp returns an App whose queue has op id active (Kind Build,
// Pkg "yay"), and the activity-log path it writes to.
func appWithActiveOp(t *testing.T, id string) (App, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "activity.log")
	a := App{deps: Dependencies{Config: domain.WorkspaceConfig{ActivityLogPath: logPath}}}
	a.ops.Enqueue(Operation{ID: id, Kind: OpBuild, Pkg: "yay"})
	if _, ok := a.ops.Dequeue(); !ok || a.ops.active == nil {
		t.Fatal("setup: no active operation")
	}
	return a, logPath
}

func updateApp(t *testing.T, a App, msg tea.Msg) App {
	t.Helper()
	model, _ := a.Update(msg)
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned %T, want App", model)
	}
	return next
}

func activityLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func TestOperationCancelledWithNoActiveOp(t *testing.T) {
	a := App{deps: Dependencies{Config: domain.WorkspaceConfig{ActivityLogPath: filepath.Join(t.TempDir(), "activity.log")}}}

	next := updateApp(t, a, msgs.OperationCancelledMsg{OpID: "op-1"}) // panicked before #335

	if next.ops.active != nil {
		t.Fatal("no operation should become active")
	}
	if next.toast != "" {
		t.Fatalf("stale cancel must not toast, got %q", next.toast)
	}
}

func TestOperationCancelledForOtherOpIsIgnored(t *testing.T) {
	a, logPath := appWithActiveOp(t, "op-1")

	next := updateApp(t, a, msgs.OperationCancelledMsg{OpID: "op-2"})

	if next.ops.active == nil || next.ops.active.ID != "op-1" {
		t.Fatalf("active operation must stay op-1, got %+v", next.ops.active)
	}
	if next.toast != "" {
		t.Fatalf("stale cancel must not toast, got %q", next.toast)
	}
	if strings.Contains(activityLog(t, logPath), "cancelled") {
		t.Fatal("stale cancel must not be logged as a cancellation")
	}
}

func TestOperationCancelledForActiveOp(t *testing.T) {
	a, logPath := appWithActiveOp(t, "op-1")

	next := updateApp(t, a, msgs.OperationCancelledMsg{OpID: "op-1"})

	if next.ops.active != nil {
		t.Fatalf("cancelled operation must be completed, still active: %+v", next.ops.active)
	}
	if !strings.Contains(next.toast, "Operation cancelled: yay") {
		t.Fatalf("toast = %q", next.toast)
	}
	if !strings.Contains(activityLog(t, logPath), "cancelled") {
		t.Fatal("cancellation not in the activity log")
	}
}
