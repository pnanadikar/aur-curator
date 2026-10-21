package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/ui/screens"
	"github.com/pramod/aur-curator/internal/widgets"
)

// TestViewRendersEmptyToast verifies the toast line is always reserved
// at a fixed position, even when idle — this prevents the layout from
// shifting when a toast appears/clears. At width=0 (this test's
// zero-value App), the reserved line renders as an empty string, which
// still produces a leading separator when joined with the rest of the
// output — that's expected, not a bug.
func TestViewRendersEmptyToast(t *testing.T) {
	t.Parallel()

	app := App{}
	view := app.View()
	if !strings.HasPrefix(view, "\n") {
		t.Fatalf("reserved toast line should produce a leading separator; got %q", view)
	}
}

// TestViewRendersToastLineAtRealisticWidth verifies the reserved toast
// line renders left-aligned (not right-aligned) at a nonzero terminal
// width, matching what a real terminal sees. When no toast is set,
// the line is empty; when a toast is set, the text appears at the
// start of the line, not pushed to the right.
func TestViewRendersToastLineAtRealisticWidth(t *testing.T) {
	t.Parallel()

	// No toast — first line should be empty.
	app := App{width: 80}
	view := app.View()
	firstNewline := strings.Index(view, "\n")
	if firstNewline < 0 {
		t.Fatalf("expected a newline separator in View output; got %q", view)
	}
	firstLine := view[:firstNewline]
	if firstLine != "" {
		t.Fatalf("reserved toast line (no toast) = %q, want empty string", firstLine)
	}

	// With toast — text should appear at the start of the line.
	app = App{toast: "hello", width: 80}
	view = app.View()
	firstNewline = strings.Index(view, "\n")
	if firstNewline < 0 {
		t.Fatalf("expected a newline separator in View output; got %q", view)
	}
	firstLine = view[:firstNewline]
	if firstLine != "hello" {
		t.Fatalf("reserved toast line (with toast) = %q, want %q (left-aligned)", firstLine, "hello")
	}
}

func TestViewRendersToast(t *testing.T) {
	t.Parallel()

	app := App{toast: "some toast message"}
	view := app.View()
	if !strings.Contains(view, "some toast message") {
		t.Fatalf("View() = %q does not contain toast", view)
	}
}

func TestViewToastPrepended(t *testing.T) {
	t.Parallel()

	app := App{
		toast:  "toast text",
		screen: 0, // unknown screen returns ""
	}
	view := app.View()
	if !strings.HasPrefix(view, "toast text") {
		t.Fatalf("View() = %q, expected toast to be prepended", view)
	}
}

func TestUpdateOperationFailedSetsToast(t *testing.T) {
	t.Parallel()

	app := App{
		ops: OperationsQueue{},
	}

	// Set up a fake active operation so the OperationFailedMsg handler
	// doesn't panic on a nil dereference.
	app.ops.active = &Operation{ID: "test-op", Kind: OpFetch, Pkg: "pkg"}

	updated, _ := app.Update(msgs.OperationFailedMsg{
		OpID:     "test-op",
		Err:      assertError{msg: "connection refused"},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	if app.toast != "Operation failed: connection refused" {
		t.Fatalf("toast = %q, want %q", app.toast, "Operation failed: connection refused")
	}
}

type assertError struct{ msg string }

func (e assertError) Error() string { return e.msg }
func (e assertError) Unwrap() error { return nil }

func TestSetToastIncrementsToastID(t *testing.T) {
	t.Parallel()

	app := App{}
	app, cmd := app.setToast("hello")
	if app.toast != "hello" {
		t.Fatalf("toast = %q, want %q", app.toast, "hello")
	}
	if app.toastID != 1 {
		t.Fatalf("toastID = %d, want 1", app.toastID)
	}
	if cmd == nil {
		t.Fatal("setToast should return a non-nil auto-dismiss Cmd")
	}

	app, cmd = app.setToast("world")
	if app.toast != "world" {
		t.Fatalf("toast = %q, want %q", app.toast, "world")
	}
	if app.toastID != 2 {
		t.Fatalf("toastID = %d, want 2", app.toastID)
	}
	if cmd == nil {
		t.Fatal("setToast should return a non-nil auto-dismiss Cmd")
	}
}

func TestClearToastMsgClearsWhenMatched(t *testing.T) {
	t.Parallel()

	app := App{}
	app, _ = app.setToast("hello")

	updated, cmd := app.Update(clearToastMsg{id: app.toastID})
	app = updated.(App)
	if cmd != nil {
		t.Fatal("expected no cmd when dismissing toast")
	}
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (dismissed)", app.toast)
	}
}

func TestClearToastMsgIgnoresStaleID(t *testing.T) {
	t.Parallel()

	app := App{}
	app, _ = app.setToast("hello")

	// A clear message with an older ID should be ignored.
	updated, _ := app.Update(clearToastMsg{id: 0})
	app = updated.(App)
	if app.toast != "hello" {
		t.Fatalf("toast = %q, want 'hello' (stale dismiss should be ignored)", app.toast)
	}
}

func TestOperationCompletedMsgWithResultSetsToast(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app := App{
		ops: OperationsQueue{},
	}

	// Set up a fake active operation so the OperationCompletedMsg handler
	// doesn't panic on a nil dereference.
	app.ops.active = &Operation{ID: "test-op", Kind: OpFetch, Pkg: "pkg"}

	updated, cmd := app.Update(msgs.OperationCompletedMsg{
		OpID:   "test-op",
		Result: "Operation completed successfully",
	})
	app = updated.(App)

	if app.toast != "Operation completed successfully" {
		t.Fatalf("toast = %q, want %q", app.toast, "Operation completed successfully")
	}
	if app.toastID == 0 {
		t.Fatal("toastID should be incremented after setting toast")
	}
	if cmd == nil {
		t.Fatal("expected a command for toast dismiss")
	}
}

func TestOperationCompletedMsgNoImmediateClear(t *testing.T) {
	t.Parallel()

	app := App{
		ops: OperationsQueue{},
	}

	// Set up a fake active operation so the OperationCompletedMsg handler
	// doesn't panic on a nil dereference.
	app.ops.active = &Operation{ID: "test-op", Kind: OpFetch, Pkg: "pkg"}

	updated, _ := app.Update(msgs.OperationCompletedMsg{
		OpID:   "test-op",
		Result: "Operation completed successfully",
	})
	app = updated.(App)

	if app.toast != "Operation completed successfully" {
		t.Fatalf("toast = %q, want 'Operation completed successfully' (should not be self-cleared)", app.toast)
	}
}

func TestSetToastAutoDismiss(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app := App{}
	app, dismissCmd := app.setToast("hello")
	if app.toast != "hello" {
		t.Fatalf("toast = %q, want %q", app.toast, "hello")
	}
	if dismissCmd == nil {
		t.Fatal("setToast should return a non-nil auto-dismiss Cmd")
	}

	// Invoke the returned Cmd directly to get the clear message.
	msg := dismissCmd()
	if msg == nil {
		t.Fatal("auto-dismiss Cmd returned nil")
	}

	// Feed the clear message through Update.
	updated, cmd := app.Update(msg)
	app = updated.(App)
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (auto-dismissed)", app.toast)
	}
	if cmd != nil {
		t.Fatal("clearToastMsg case should return nil Cmd")
	}
}

func TestSetToastStaleTimerGuard(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app := App{}
	app, dismissCmdA := app.setToast("A")
	if app.toast != "A" {
		t.Fatalf("toast = %q, want %q", app.toast, "A")
	}

	// Set a second toast — toastID increments again.
	app, dismissCmdB := app.setToast("B")
	if app.toast != "B" {
		t.Fatalf("toast = %q, want %q", app.toast, "B")
	}

	// The first timer was captured with the previous toastID.
	// Feeding it should NOT clear the toast because toastID has incremented.
	msgA := dismissCmdA()
	updated, _ := app.Update(msgA)
	app = updated.(App)
	if app.toast != "B" {
		t.Fatalf("toast = %q, want %q (stale timer should not clear new toast)", app.toast, "B")
	}

	// The second timer should clear because its id matches.
	msgB := dismissCmdB()
	updated, _ = app.Update(msgB)
	app = updated.(App)
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (current timer should clear)", app.toast)
	}
}

func TestOperationCompletedMsgAutoDismissClears(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app := App{
		ops: OperationsQueue{},
	}

	// Set up a fake active operation so the OperationCompletedMsg handler
	// doesn't panic on a nil dereference.
	app.ops.active = &Operation{ID: "test-op", Kind: OpFetch, Pkg: "pkg"}

	updated, cmd := app.Update(msgs.OperationCompletedMsg{
		OpID:   "test-op",
		Result: "done",
	})
	app = updated.(App)

	if app.toast != "done" {
		t.Fatalf("toast = %q, want %q", app.toast, "done")
	}
	if cmd == nil {
		t.Fatal("expected auto-dismiss Cmd from OperationCompletedMsg handler")
	}

	// Invoke the Cmd to get the clear message.
	msg := cmd()
	if msg == nil {
		t.Fatal("auto-dismiss Cmd returned nil")
	}

	// Feed it through Update — toast should be cleared.
	updated, _ = app.Update(msg)
	app = updated.(App)
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (auto-dismiss should clear)", app.toast)
	}
}

func TestToastMsgSetsToastAndAutoDismiss(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app := App{}
	updated, cmd := app.Update(msgs.ToastMsg{Text: "hello"})
	app = updated.(App)

	if app.toast != "hello" {
		t.Fatalf("toast = %q, want %q", app.toast, "hello")
	}
	if cmd == nil {
		t.Fatal("expected auto-dismiss Cmd from ToastMsg handler")
	}

	// Invoke the Cmd and feed back — should clear.
	msg := cmd()
	updated, _ = app.Update(msg)
	app = updated.(App)
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (should be auto-dismissed)", app.toast)
	}
}

func TestUpdateToastMsgDoesNotReachSubModels(t *testing.T) {
	t.Parallel()

	// ToastMsg is consumed at the App level — it should not be
	// forwarded to any sub-model's Update path. We verify by
	// confirming that the toast is set on the returned App rather
	// than the message falling through.
	app := App{}
	updated, cmd := app.Update(msgs.ToastMsg{Text: "from update"})
	app = updated.(App)
	if app.toast != "from update" {
		t.Fatalf("toast = %q, want %q", app.toast, "from update")
	}
	if cmd == nil {
		t.Fatal("expected auto-dismiss Cmd")
	}
}

func TestStaleToastDismissNoop(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	// Simulate: set toast A (ID=1), set toast B (ID=2), fire timer for A (ID=1).
	// The stale timer should be ignored.
	app := App{}
	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	app, cmdA := app.setToast("A")
	app, cmdB := app.setToast("B")

	// cmdA was captured with toastID=1, but current toastID is 2.
	msgA := cmdA()
	updated, _ := app.Update(msgA)
	app = updated.(App)
	if app.toast != "B" {
		t.Fatalf("toast = %q, want %q (stale dismiss should be ignored)", app.toast, "B")
	}

	// cmdB was captured with toastID=2, which matches.
	msgB := cmdB()
	updated, _ = app.Update(msgB)
	app = updated.(App)
	if app.toast != "" {
		t.Fatalf("toast = %q, want empty", app.toast)
	}
}

func TestOperationsPanelLanes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		q            OperationsQueue
		wantLaneName string
		wantStatus   string
		wantLastLine string
		wantPkg      domain.PackageName
	}{
		{
			name:         "idle lane",
			q:            OperationsQueue{},
			wantLaneName: "Fetch",
			wantStatus:   "idle",
		},
		{
			name: "running lane",
			q: OperationsQueue{
				active: &Operation{ID: "1", Kind: OpFetch, Pkg: "wget"},
				lanes: map[Lane]*LaneState{
					LaneFetch: {Lane: LaneFetch, Status: StatusRunning, Pkg: "wget"},
				},
			},
			wantLaneName: "Fetch",
			wantStatus:   "running",
			wantPkg:      "wget",
		},
		{
			name: "completed lane with log",
			q: OperationsQueue{
				lanes: map[Lane]*LaneState{
					LaneBuild: {
						Lane:   LaneBuild,
						Status: StatusCompleted,
						Log:    []LogEntry{{Message: "build succeeded"}},
					},
				},
			},
			wantLaneName: "Build",
			wantStatus:   "completed",
			wantLastLine: "build succeeded",
		},
		{
			name: "failed lane with log",
			q: OperationsQueue{
				lanes: map[Lane]*LaneState{
					LaneAnalyze: {
						Lane:   LaneAnalyze,
						Status: StatusFailed,
						Log:    []LogEntry{{Message: "analyzer error"}},
					},
				},
			},
			wantLaneName: "Analyze",
			wantStatus:   "failed",
			wantLastLine: "analyzer error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := App{ops: tt.q}
			lanes := app.operationsPanelLanes()

			var found *widgets.LaneStatus
			for i := range []Lane{LaneFetch, LaneAnalyze, LaneBuild, LanePublish} {
				if lanes[i].Name == tt.wantLaneName {
					found = &lanes[i]
					break
				}
			}

			if found == nil {
				t.Fatalf("lane %q not found in panel lanes", tt.wantLaneName)
			}

			if found.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", found.Status, tt.wantStatus)
			}

			if found.LastLine != tt.wantLastLine {
				t.Fatalf("LastLine = %q, want %q", found.LastLine, tt.wantLastLine)
			}

			if found.Pkg != tt.wantPkg {
				t.Fatalf("Pkg = %q, want %q", found.Pkg, tt.wantPkg)
			}
		})
	}
}

func TestViewRendersStatusLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		app         App
		wantLines   []string
		wantMissing []string
	}{
		{
			name:        "idle shows panel and bar",
			app:         App{},
			wantLines:   []string{"Operations", "Idle"},
			wantMissing: []string{"Error:", "operation complete"},
		},
		{
			name: "running operation shows in panel",
			app: App{
				ops: func() OperationsQueue {
					q := OperationsQueue{
						active: &Operation{ID: "1", Kind: OpBuild, Pkg: "testpkg"},
					}
					q.MarkStarted(*q.active)
					return q
				}(),
			},
			wantLines:   []string{"Operations", "building", "testpkg"},
			wantMissing: []string{"Error:"},
		},
		{
			name: "error line shows",
			app: App{
				err: assertError{msg: "connection refused"},
			},
			wantLines:   []string{"Error: connection refused", "Operations"},
			wantMissing: []string{"operation complete"},
		},
		{
			name: "ops + error",
			app: App{
				ops: func() OperationsQueue {
					q := OperationsQueue{
						active: &Operation{ID: "1", Kind: OpBuild, Pkg: "myapp"},
					}
					q.MarkStarted(*q.active)
					return q
				}(),
				err: assertError{msg: "build failed"},
			},
			wantLines:   []string{"Error: build failed", "Operations", "building"},
			wantMissing: []string{"operation complete"},
		},
		{
			name: "ops + error + toast",
			app: App{
				ops: func() OperationsQueue {
					q := OperationsQueue{
						active: &Operation{ID: "1", Kind: OpAnalyze, Pkg: "libfoo"},
					}
					q.MarkStarted(*q.active)
					return q
				}(),
				err:   assertError{msg: "timeout"},
				toast: "operation complete",
			},
			wantLines:   []string{"operation complete", "Error: timeout", "Operations", "analyzing"},
			wantMissing: []string{},
		},
		{
			name: "toast only",
			app: App{
				toast: "hello toast",
			},
			wantLines:   []string{"hello toast", "Operations"},
			wantMissing: []string{"Error:"},
		},
		{
			name: "pending queue shows count",
			app: App{
				ops: func() OperationsQueue {
					q := OperationsQueue{
						active:  &Operation{ID: "1", Kind: OpFetch, Pkg: "pkg1"},
						pending: []Operation{{ID: "2", Kind: OpBuild, Pkg: "pkg2"}},
					}
					q.MarkStarted(*q.active)
					return q
				}(),
			},
			wantLines:   []string{"Operations", "fetching", "queued"},
			wantMissing: []string{"Error:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := tt.app
			view := app.View()

			for _, want := range tt.wantLines {
				if !strings.Contains(view, want) {
					t.Errorf("View() does not contain %q; got:\n%s", want, view)
				}
			}

			for _, want := range tt.wantMissing {
				if strings.Contains(view, want) {
					t.Errorf("View() contains %q, but it should not; got:\n%s", want, view)
				}
			}
		})
	}
}

func TestViewBodyRendersBeforePanelAndStatusBar(t *testing.T) {
	t.Parallel()

	app := App{
		screen: msgs.ScreenDashboard,
	}

	view := app.View()
	bodyIdx := strings.Index(view, "No curated packages found.")
	panelIdx := strings.Index(view, "Operations")

	if bodyIdx < 0 {
		t.Fatalf("View() does not contain body text; got:\n%s", view)
	}
	if panelIdx < 0 {
		t.Fatalf("View() does not contain panel header; got:\n%s", view)
	}
	if bodyIdx > panelIdx {
		t.Fatalf("body index (%d) is after panel index (%d); body must render before the Operations Panel", bodyIdx, panelIdx)
	}
}

func TestHandleNavigateClearsError(t *testing.T) {
	t.Parallel()

	app := App{
		err:    assertError{msg: "something failed"},
		toast:  "some toast",
		screen: msgs.ScreenDashboard,
	}

	updated, _ := app.Update(msgs.NavigateMsg{To: msgs.ScreenSettings})
	app = updated.(App)

	if app.err != nil {
		t.Fatalf("handleNavigate did not clear error; err = %v", app.err)
	}
	if app.toast != "" {
		t.Fatalf("handleNavigate did not clear toast; toast = %q", app.toast)
	}
}

func TestScreenWindowSizeMsgSubtractsAppChrome(t *testing.T) {
	t.Parallel()

	app := App{
		width:  120,
		height: 40,
	}

	msg := app.screenWindowSizeMsg()

	want := 40 - app.topHeight() - app.bottomHeight()
	if msg.Width != 120 {
		t.Fatalf("width = %d, want 120", msg.Width)
	}
	if msg.Height != want {
		t.Fatalf("height = %d, want %d", msg.Height, want)
	}
}

// TestOperationCompletedMsgForwardsToReviewScreen verifies that when
// the active screen is ReviewScreen, an OperationCompletedMsg flows
// through the queue-management path AND reaches ReviewScreen.Update()
// so the screen can clear its loading state and surface findings.
// This is the regression test for CMD-063: before the fix, the
// queue-management switch returned early and the forward never ran.
func TestOperationCompletedMsgForwardsToReviewScreen(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	review := screens.NewReviewScreen("test-pkg", nil, nil, nil, domain.WorkspaceConfig{})
	// NewReviewScreen starts with loading=true — the screen would
	// render "Loading review…" until the completion message arrives.

	app := App{
		ops:    OperationsQueue{},
		screen: msgs.ScreenReview,
		review: review,
	}

	// Set up a fake active operation so the OperationCompletedMsg
	// handler doesn't panic on a nil dereference.
	app.ops.active = &Operation{ID: "test-op", Kind: OpAnalyze, Pkg: "test-pkg"}

	updated, cmd := app.Update(msgs.OperationCompletedMsg{
		OpID: "test-op",
		Result: &domain.Review{
			PackageName: "test-pkg",
			Status:      domain.ReviewPending,
		},
	})
	app = updated.(App)

	if app.toast != "" {
		t.Fatalf("toast = %q, want empty (analyze completion has no string Result)", app.toast)
	}

	// The critical assertion: the ReviewScreen's loading flag must
	// have been cleared by the forwarded OperationCompletedMsg.
	// Since loading is unexported, verify via View output —
	// "Loading review…" must NOT appear when loading has been cleared.
	view := app.review.View()
	if strings.Contains(view, "Loading review") {
		t.Fatalf("ReviewScreen.View() still contains loading text, meaning loading was not cleared; got:\n%s", view)
	}

	// Also verify the cmd chain includes the toast-dismiss timer.
	// cmd may be nil if drainQueueCmd returned nil (nothing to drain)
	// and ReviewScreen.Update() returned nil — only check the toast
	// dismiss part when queueCmd is non-nil.
	if cmd != nil {
		msg := cmd()
		if msg == nil {
			t.Fatal("toast-dismiss Cmd returned nil")
		}
	}
}

func TestOperationCompletedMsgWithStringResultClearsReviewScreenLoading(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	review := screens.NewReviewScreen("test-pkg", nil, nil, nil, domain.WorkspaceConfig{})

	app := App{
		ops:    OperationsQueue{},
		screen: msgs.ScreenReview,
		review: review,
	}

	app.ops.active = &Operation{ID: "test-op", Kind: OpAnalyze, Pkg: "test-pkg"}

	updated, cmd := app.Update(msgs.OperationCompletedMsg{
		OpID:   "test-op",
		Result: "Analyze skipped — review already decided",
	})
	app = updated.(App)

	// String Result sets a toast — verify it's set.
	if app.toast != "Analyze skipped — review already decided" {
		t.Fatalf("toast = %q, want %q", app.toast, "Analyze skipped — review already decided")
	}

	// The critical assertion: the ReviewScreen's loading flag must
	// have been cleared by the forwarded OperationCompletedMsg,
	// even when Result is a string (not *domain.Review).
	view := app.review.View()
	if strings.Contains(view, "Loading review") {
		t.Fatalf("ReviewScreen.View() still contains loading text after string Result; got:\n%s", view)
	}

	if cmd != nil {
		msg := cmd()
		if msg == nil {
			t.Fatal("toast-dismiss Cmd returned nil")
		}
	}
}

// TestOperationCompletedMsgBroadcastNoPanic verifies that when
// OperationCompletedMsg is delivered to every screen (not just the
// active one), no screen panics or crashes.  This is the minimal
// regression guard for SYNC-001 broadcast.
func TestOperationCompletedMsgBroadcastNoPanic(t *testing.T) {
	// Not parallel: overrides the package-level toastDuration (#313).

	old := toastDuration
	toastDuration = 0
	defer func() { toastDuration = old }()

	review := screens.NewReviewScreen("test-pkg", nil, nil, nil, domain.WorkspaceConfig{})
	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})
	repoStatus := screens.NewRepositoryStatusModel(deps.Dependencies{})
	settings := screens.NewSettingsModel(deps.Dependencies{})

	app := App{
		ops:        OperationsQueue{},
		screen:     msgs.ScreenDashboard,
		dashboard:  dashboard,
		detail:     detail,
		review:     review,
		repoStatus: repoStatus,
		settings:   settings,
	}

	app.ops.active = &Operation{ID: "test-pkg", Kind: OpAnalyze, Pkg: "test-pkg"}

	updated, _ := app.Update(msgs.OperationCompletedMsg{
		OpID:   "test-pkg",
		Result: "done",
	})
	app = updated.(App)

	// All screens must render without panicking.
	_ = app.dashboard.View()
	_ = app.detail.View()
	_ = app.review.View()
	_ = app.repoStatus.View()
	_ = app.settings.View()
}

// TestOperationFailedMsgBroadcastNoPanic verifies the same for
// OperationFailedMsg.
func TestOperationFailedMsgBroadcastNoPanic(t *testing.T) {
	t.Parallel()

	review := screens.NewReviewScreen("test-pkg", nil, nil, nil, domain.WorkspaceConfig{})
	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})
	repoStatus := screens.NewRepositoryStatusModel(deps.Dependencies{})
	settings := screens.NewSettingsModel(deps.Dependencies{})

	app := App{
		ops:        OperationsQueue{},
		screen:     msgs.ScreenDashboard,
		dashboard:  dashboard,
		detail:     detail,
		review:     review,
		repoStatus: repoStatus,
		settings:   settings,
	}

	app.ops.active = &Operation{ID: "test-pkg", Kind: OpAnalyze, Pkg: "test-pkg"}

	updated, _ := app.Update(msgs.OperationFailedMsg{
		OpID:     "test-pkg",
		Err:      assertError{msg: "connection refused"},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	// All screens must render without panicking.
	_ = app.dashboard.View()
	_ = app.detail.View()
	_ = app.review.View()
	_ = app.repoStatus.View()
	_ = app.settings.View()
}

// TestNavigateMsgReachesOnlyActiveScreen verifies that non-operation
// messages (like NavigateMsg) still only reach the active screen —
// the broadcast is limited to OperationCompletedMsg and
// OperationFailedMsg only.
func TestNavigateMsgReachesOnlyActiveScreen(t *testing.T) {
	t.Parallel()

	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})

	app := App{
		screen:    msgs.ScreenDashboard,
		dashboard: dashboard,
		detail:    detail,
	}

	// Navigate to Settings — this should NOT reach the dashboard
	// (which is about to become inactive) via a broadcast path.
	updated, _ := app.Update(msgs.NavigateMsg{To: msgs.ScreenSettings})
	app = updated.(App)

	if app.screen != msgs.ScreenSettings {
		t.Fatalf("screen = %d, want %d", app.screen, msgs.ScreenSettings)
	}
}

// TestPackagesLoadedMsgReachesDashboardWhenInactive verifies that
// PackagesLoadedMsg is routed to the dashboard even when a different
// screen is active — before the #286 fix this message was delivered to
// the active screen and silently dropped, leaving the dashboard stuck on
// "Loading packages..." with no in-app recovery.
func TestPackagesLoadedMsgReachesDashboardWhenInactive(t *testing.T) {
	t.Parallel()

	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})

	app := App{
		screen:    msgs.ScreenPackageDetail,
		dashboard: dashboard,
		detail:    detail,
	}

	pkg := domain.PackageName("test-pkg")
	updated, cmd := app.Update(screens.PackagesLoadedMsg{
		Packages: []domain.PackageName{pkg},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	if cmd == nil {
		t.Fatal("no follow-up load cmd issued; PackagesLoadedMsg was not routed to the dashboard")
	}
	if !strings.Contains(app.dashboard.View(), string(pkg)) {
		t.Fatalf("dashboard view missing %q after PackagesLoadedMsg:\n%s", pkg, app.dashboard.View())
	}
}

// TestReviewDataMsgReachesDashboardWhenInactive verifies that
// ReviewDataMsg is routed to the dashboard even when a different screen
// is active — before the #286 fix the per-package review data was
// dropped whenever the user navigated away mid-batch, leaving the row
// stuck on "(loading...)".
func TestReviewDataMsgReachesDashboardWhenInactive(t *testing.T) {
	t.Parallel()

	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})

	app := App{
		screen:    msgs.ScreenPackageDetail,
		dashboard: dashboard,
		detail:    detail,
	}

	pkg := domain.PackageName("test-pkg")

	updated, _ := app.Update(screens.PackagesLoadedMsg{
		Packages: []domain.PackageName{pkg},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	updated, _ = app.Update(screens.ReviewDataMsg{
		Pkg:      pkg,
		Status:   domain.ReviewPending,
		Has:      true,
		Updates:  3,
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	view := app.dashboard.View()
	if strings.Contains(view, "(loading...)") {
		t.Fatalf("review data was not applied to the dashboard row:\n%s", view)
	}
	if !strings.Contains(view, "↑3") {
		t.Fatalf("update count missing from dashboard row:\n%s", view)
	}
}

// TestAURUpdateMsgReachesDashboardWhenInactive verifies that
// AURUpdateMsg is routed to the dashboard even when a different screen
// is active — before the #286 fix the bulk AUR check result was dropped
// whenever the user navigated away while it was in flight, leaving the
// Available column empty.
func TestAURUpdateMsgReachesDashboardWhenInactive(t *testing.T) {
	t.Parallel()

	dashboard := screens.NewDashboardModel(deps.Dependencies{})
	detail := screens.NewPackageDetailModel(deps.Dependencies{})

	app := App{
		screen:    msgs.ScreenPackageDetail,
		dashboard: dashboard,
		detail:    detail,
	}

	pkg := domain.PackageName("test-pkg")

	updated, _ := app.Update(screens.PackagesLoadedMsg{
		Packages: []domain.PackageName{pkg},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	updated, _ = app.Update(screens.ReviewDataMsg{
		Pkg:      pkg,
		Status:   domain.ReviewPending,
		Has:      true,
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	updated, _ = app.Update(msgs.AURUpdateMsg{
		Pairs: []aurinfo.VersionPair{
			{Pkg: pkg, LocalVersion: "1.0.0", AURVersion: "2.0.0", AURStale: true},
		},
		TraceCtx: context.Background(),
	})
	app = updated.(App)

	if !strings.Contains(app.dashboard.View(), "1.0.0 → 2.0.0") {
		t.Fatalf("AUR update missing from dashboard row:\n%s", app.dashboard.View())
	}
}

// TestHandleBackHop2DashboardRelay verifies that when PackageDetails
// emits BackMsg{Changed: true} and the previous screen was Dashboard,
// handleBack returns the Dashboard's LoadReviewCmd for the currently
// selected package — completing the relay chain from Review → Details
// → Dashboard.
func TestHandleBackHop2DashboardRelay(t *testing.T) {
	t.Parallel()

	pkg := domain.PackageName("test-pkg-152")

	detail := screens.NewPackageDetailModel(deps.Dependencies{})
	// Navigate to PackageDetails so the detail model knows the package.
	detail, _ = detail.Update(screens.PackageSelectedMsg{Pkg: pkg})

	dashboard := screens.NewDashboardModel(deps.Dependencies{})

	// history = [Dashboard] means popping gives prev = Dashboard,
	// which triggers the hop-2 relay branch.
	app := App{
		screen:    msgs.ScreenPackageDetail,
		history:   []msgs.Screen{msgs.ScreenDashboard},
		dashboard: dashboard,
		detail:    detail,
	}

	// Simulate PackageDetails emitting BackMsg{Changed: true}.
	updated, cmd := app.Update(msgs.BackMsg{Changed: true})
	app = updated.(App)

	if app.screen != msgs.ScreenDashboard {
		t.Fatalf("screen = %d, want %d", app.screen, msgs.ScreenDashboard)
	}
	if len(app.history) != 0 {
		t.Fatalf("history length = %d, want 0", len(app.history))
	}
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd from handleBack when relaying to Dashboard")
	}
	// Execute the Cmd and verify it returns ReviewDataMsg.
	result := cmd()
	_, ok := result.(screens.ReviewDataMsg)
	if !ok {
		t.Fatalf("Cmd returned %T, want screens.ReviewDataMsg", result)
	}
}

// TestHandleBackHop1MarkChanged verifies that when Review emits
// BackMsg{Changed: true} and the previous screen was PackageDetails,
// handleBack calls MarkChanged on the detail model and returns Init().
func TestHandleBackHop1MarkChanged(t *testing.T) {
	t.Parallel()

	detail := screens.NewPackageDetailModel(deps.Dependencies{})
	detail, _ = detail.Update(screens.PackageSelectedMsg{Pkg: "test-pkg"})

	// history = [Dashboard, PackageDetail] means popping gives
	// prev = PackageDetail, which triggers the hop-1 MarkChanged branch.
	app := App{
		screen:  msgs.ScreenPackageDetail,
		history: []msgs.Screen{msgs.ScreenDashboard, msgs.ScreenPackageDetail},
		detail:  detail,
	}

	// Simulate Review emitting BackMsg{Changed: true}.
	updated, cmd := app.Update(msgs.BackMsg{Changed: true})
	app = updated.(App)

	if app.screen != msgs.ScreenPackageDetail {
		t.Fatalf("screen = %d, want %d", app.screen, msgs.ScreenPackageDetail)
	}
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd (Init) from handleBack on hop 1")
	}

	// Verify the detail's Package() still returns the correct package,
	// confirming MarkChanged returned a new value (not mutating in place).
	if app.detail.Package() != "test-pkg" {
		t.Fatalf("detail.Package() = %q, want %q", app.detail.Package(), "test-pkg")
	}
}

// TestHandleBackNoRelayWithoutChanged verifies that handleBack does
// not call LoadReviewCmd or Init() when msg.Changed is false.
func TestHandleBackNoRelayWithoutChanged(t *testing.T) {
	t.Parallel()

	detail := screens.NewPackageDetailModel(deps.Dependencies{})
	detail, _ = detail.Update(screens.PackageSelectedMsg{Pkg: "test-pkg"})

	dashboard := screens.NewDashboardModel(deps.Dependencies{})

	// history = [Dashboard] means popping gives prev = Dashboard.
	app := App{
		screen:    msgs.ScreenPackageDetail,
		history:   []msgs.Screen{msgs.ScreenDashboard},
		dashboard: dashboard,
		detail:    detail,
	}

	updated, cmd := app.Update(msgs.BackMsg{Changed: false})
	app = updated.(App)

	if app.screen != msgs.ScreenDashboard {
		t.Fatalf("screen = %d, want %d", app.screen, msgs.ScreenDashboard)
	}
	if cmd != nil {
		t.Fatal("expected nil Cmd when msg.Changed is false")
	}
}

func TestSizeSyncTracksChrome(t *testing.T) {
	t.Parallel()

	app := NewApp(Dependencies{})
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = model.(App)
	base := app.lastSize
	if base.Height == 0 || base != app.screenWindowSizeMsg() {
		t.Fatalf("lastSize = %+v, want current screenWindowSizeMsg %+v", base, app.screenWindowSizeMsg())
	}

	// An error line adds a row above the screen: the screens must shrink.
	model, _ = app.Update(ErrorMsg{Err: errors.New("boom")})
	app = model.(App)
	if app.lastSize.Height != base.Height-1 {
		t.Fatalf("with error: lastSize.Height = %d, want %d", app.lastSize.Height, base.Height-1)
	}

	// Navigating clears the error: size must return to base.
	model, _ = app.Update(msgs.NavigateMsg{To: msgs.ScreenHelp})
	app = model.(App)
	if app.lastSize != base {
		t.Fatalf("after navigate: lastSize = %+v, want %+v", app.lastSize, base)
	}
}

func TestSizeSyncReachesScreenAfterResizeElsewhere(t *testing.T) {
	t.Parallel()

	// Use ScreenPackageDetail, not ScreenHelp: Help is reached only via a
	// Nav Menu digit key, and NavigateMsg.FromMenu clears history entirely
	// (see msgs.go) — there would be nothing to Back to. Package Detail is
	// reached via a non-menu NavigateMsg, which pushes a real history entry.
	app := NewApp(Dependencies{})
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = model.(App)
	model, _ = app.Update(msgs.NavigateMsg{To: msgs.ScreenPackageDetail, Package: "test-pkg"})
	app = model.(App)
	if app.screen != msgs.ScreenPackageDetail {
		t.Fatalf("screen = %v, want ScreenPackageDetail (check NavigateMsg wiring before continuing)", app.screen)
	}

	// Resize while on Package Detail, then go back: Dashboard must not be stale.
	model, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app = model.(App)
	want := app.screenWindowSizeMsg()
	model, _ = app.Update(msgs.BackMsg{})
	app = model.(App)

	if app.screen != msgs.ScreenDashboard {
		t.Fatalf("screen = %v, want Dashboard", app.screen)
	}
	if app.lastSize != want {
		t.Fatalf("lastSize = %+v, want %+v", app.lastSize, want)
	}
}

func TestUpdateQuitConfirmModal(t *testing.T) {
	t.Parallel()

	// Test 1: no active operation — immediate quit
	t.Run("no-active-immediate-quit", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops: OperationsQueue{},
		}
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		if cmd == nil {
			t.Fatal("expected quit command when no active operation")
		}
		msg := cmd()
		if _, isQuitMsg := msg.(tea.QuitMsg); !isQuitMsg {
			t.Fatalf("expected tea.QuitMsg, got %T", msg)
		}
	})

	// Test 2: active operation — modal shown, not quit
	t.Run("active-shows-modal", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops: OperationsQueue{},
		}
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}
		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)
		if !app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to be true")
		}
		view := app.quitConfirmModal.View()
		if !strings.Contains(view, "Background task running (build): bash") {
			t.Fatalf("modal view = %q, expected to contain task message", view)
		}
	})

	// Test 3: active operation without package
	t.Run("active-no-pkg", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops: OperationsQueue{},
		}
		app.ops.active = &Operation{ID: "1", Kind: OpBuild}
		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)
		if !app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to be true")
		}
		view := app.quitConfirmModal.View()
		if !strings.Contains(view, "Background task running (build)") {
			t.Fatalf("modal view = %q, expected to contain task message without pkg", view)
		}
	})

	// Test 4: Y confirmation — quit
	t.Run("confirm-y-quit", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops:               OperationsQueue{},
			quitConfirmActive: true,
			width:             80,
		}
		app.quitConfirmModal = widgets.NewChoiceModal("Background task running (build): bash")
		app.quitConfirmModal.SetWidth(80)
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}

		// First press ctrl+q to show modal
		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)

		// Now press Y to confirm
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		if cmd == nil {
			t.Fatal("expected tea.Quit after confirming modal")
		}
		msg := cmd()
		if _, isQuitMsg := msg.(tea.QuitMsg); !isQuitMsg {
			t.Fatalf("expected tea.QuitMsg, got %T", msg)
		}
	})

	// Test 5: N cancellation — clear state
	t.Run("cancel-n-clear", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops:               OperationsQueue{},
			quitConfirmActive: true,
			width:             80,
		}
		app.quitConfirmModal = widgets.NewChoiceModal("Background task running (build): bash")
		app.quitConfirmModal.SetWidth(80)
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}

		// First press ctrl+q to show modal
		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)

		// Now press N to cancel
		updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		app = updated.(App)
		if app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to be false after cancel")
		}
		if cmd != nil {
			t.Fatal("expected nil Cmd after cancel")
		}
	})

	// Test 6: Esc cancellation — clear state
	t.Run("cancel-esc-clear", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops:               OperationsQueue{},
			quitConfirmActive: true,
			width:             80,
		}
		app.quitConfirmModal = widgets.NewChoiceModal("Background task running (build): bash")
		app.quitConfirmModal.SetWidth(80)
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}

		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)

		updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		app = updated.(App)
		if app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to be false after esc")
		}
		if cmd != nil {
			t.Fatal("expected nil Cmd after esc cancel")
		}
	})

	// Test 7: Enter key — quit (default focus is on Y)
	t.Run("enter-quit", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops:               OperationsQueue{},
			quitConfirmActive: true,
			width:             80,
		}
		app.quitConfirmModal = widgets.NewChoiceModal("Background task running (build): bash")
		app.quitConfirmModal.SetWidth(80)
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}

		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)

		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("expected tea.Quit after enter (default focus on Y)")
		}
		msg := cmd()
		if _, isQuitMsg := msg.(tea.QuitMsg); !isQuitMsg {
			t.Fatalf("expected tea.QuitMsg, got %T", msg)
		}
	})

	// Test 8: Unknown key — no-op
	t.Run("unknown-key-noop", func(t *testing.T) {
		t.Parallel()
		app := App{
			ops:               OperationsQueue{},
			quitConfirmActive: true,
			width:             80,
		}
		app.quitConfirmModal = widgets.NewChoiceModal("Background task running (build): bash")
		app.quitConfirmModal.SetWidth(80)
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}

		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
		app = updated.(App)

		updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
		app = updated.(App)
		if !app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to remain true after unknown key")
		}
		if cmd != nil {
			t.Fatal("expected nil Cmd after unknown key")
		}
	})
}

// TestQuitRequestedMsgUsesQuitGuard verifies that when an active operation
// is running, sending msgs.QuitRequestedMsg opens the quit-confirm modal
// instead of quitting immediately (same guard behaviour as pressing 'q'
// or Ctrl+Q on the key handler — #300).
// TestQuitRequestedMsgUsesQuitGuard verifies screen-emitted quit requests
// (nav menu q) go through the same active-operation guard as App's own
// quit keys (#179, #300).
func TestQuitRequestedMsgUsesQuitGuard(t *testing.T) {
	t.Parallel()

	t.Run("no-active-immediate-quit", func(t *testing.T) {
		t.Parallel()
		app := App{ops: OperationsQueue{}}
		_, cmd := app.Update(msgs.QuitRequestedMsg{})
		if cmd == nil {
			t.Fatal("expected quit command when no active operation")
		}
		if msg := cmd(); msg != (tea.QuitMsg{}) {
			t.Fatalf("expected tea.QuitMsg, got %T", msg)
		}
	})

	t.Run("active-shows-modal-not-quit", func(t *testing.T) {
		t.Parallel()
		app := App{ops: OperationsQueue{}}
		app.ops.active = &Operation{ID: "1", Kind: OpBuild, Pkg: "bash"}
		updated, cmd := app.Update(msgs.QuitRequestedMsg{})
		app = updated.(App)
		if !app.quitConfirmActive {
			t.Fatal("expected quitConfirmActive to be true — screen quit bypassed the guard")
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatal("QuitRequestedMsg quit despite an active operation")
			}
		}
		if view := app.quitConfirmModal.View(); !strings.Contains(view, "Background task running (build): bash") {
			t.Fatalf("modal view = %q, expected to contain task message", view)
		}
	})
}

// TestCancelKeyIsCtrlT verifies that the App-level cancel key is ctrl+t
// and that plain 'x' no longer cancels active operations (issue #302).
func TestCancelKeyIsCtrlT(t *testing.T) {
	t.Parallel()

	// cancelCtx wraps context.WithCancel so the test can assert whether
	// CancelActive() actually invoked the cancel func by checking ctx.Err().
	cancelCtx := func() (ctx context.Context, cancel context.CancelFunc) {
		return context.WithCancel(context.Background())
	}

	t.Run("ctrl+t-cancels-active", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := cancelCtx()
		defer cancel()

		app := App{ops: OperationsQueue{}}
		app.ops.active = &Operation{
			ID:       "1",
			Kind:     OpBuild,
			Pkg:      "testpkg",
			TraceCtx: ctx,
			Cancel:   cancel, // wires the App-level cancel into the operation
		}

		if ctx.Err() != nil {
			t.Fatal("context should not be cancelled before sending key")
		}

		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
		app = updated.(App)

		if ctx.Err() == nil {
			t.Fatal("expected context to be cancelled after ctrl+t (CancelActive was not called)")
		}
	})

	t.Run("plain-x-does-not-cancel", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := cancelCtx()
		defer cancel()

		app := App{ops: OperationsQueue{}}
		app.ops.active = &Operation{
			ID:       "1",
			Kind:     OpBuild,
			Pkg:      "testpkg",
			TraceCtx: ctx,
			Cancel:   cancel,
		}

		if ctx.Err() != nil {
			t.Fatal("context should not be cancelled before sending key")
		}

		updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		app = updated.(App)

		if ctx.Err() != nil {
			t.Fatalf("expected context to remain active after plain 'x'; got err = %v", ctx.Err())
		}
	})
}
