package ui

// Characterization test for App.update (#529, F1): a scripted run through
// every branch of update, recorded after each message into a golden file.
// Splitting update must leave the file byte-identical. Regenerate it only
// for a deliberate change in App's behaviour:
//
//	go test ./internal/ui -run TestCharacterizeUpdate529 -update-golden529
//
// and read the diff in review (docs/plans/529-split-app-update.md).

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/ui/screens"
)

var updateGolden529 = flag.Bool("update-golden529", false, "rewrite testdata/characterize529.golden")

// timerMsg is what the after seam returns instead of a tea.Tick: the
// delay and the message the timer would deliver.
type timerMsg struct {
	d   time.Duration
	msg tea.Msg
}

// step is one entry of the script: a message (or one built from the
// current App), or a setup that changes the App directly where no message
// reaches a branch.
type step struct {
	label string
	msg   tea.Msg
	msgFn func(App) tea.Msg
	setup func(App) App
}

// keyMsg builds the tea.KeyMsg that pressing s sends: a named key
// (esc, enter, ctrl+…) or runes.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+q":
		return tea.KeyMsg{Type: tea.KeyCtrlQ}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// characterizeScript is the message script, in stage order (#529); each
// step is a message, or a setup that changes the App directly.
func characterizeScript() []step {
	bg := context.Background()
	key := func(s string) step { return step{label: "key " + s, msg: keyMsg(s)} }
	nav := func(to msgs.Screen, pkg domain.PackageName, menu bool) step {
		return step{label: fmt.Sprintf("navigate %v %s menu=%v", to, pkg, menu), msg: msgs.NavigateMsg{To: to, Package: pkg, FromMenu: menu}}
	}
	// noCancelQueue: an active operation without a cancel function and
	// one pending, the only state in which ctrl+t drops pending ones.
	noCancelQueue := func(a App) App {
		a.ops = OperationsQueue{}
		a.ops.Enqueue(Operation{ID: "pkg-x", Kind: OpFetch, Pkg: "pkg-x", TraceCtx: bg})
		a.ops.Enqueue(Operation{ID: "pkg-y", Kind: OpBuild, Pkg: "pkg-y", TraceCtx: bg})
		a.ops.Dequeue()
		return a
	}
	emptyQueue := func(a App) App { a.ops = OperationsQueue{}; return a }
	script := []step{
		// Stage 1: window size and keys.
		{label: "window 140x40", msg: tea.WindowSizeMsg{Width: 140, Height: 40}},
		key("j"),
		// Stage 3: toasts and errors.
		{label: "toast", msg: msgs.ToastMsg{Text: "hello"}},
		{label: "clear stale toast", msg: clearToastMsg{id: 0}},
		{label: "clear current toast", msgFn: func(a App) tea.Msg { return clearToastMsg{id: a.toastID} }},
		{label: "error", msg: ErrorMsg{Err: errors.New("boom")}},
		{label: "startup notice", msg: startupNoticeMsg{text: "Recovered 1 interrupted build"}},
		{label: "tool done ok", msg: msgs.ToolDoneMsg{What: "Pager", Pkg: "pkg-a"}},
		{label: "tool done failed", msg: msgs.ToolDoneMsg{What: "Diff tool", Pkg: "pkg-a", Err: errors.New("exit status 1"), Stderr: "first line\nlast line"}},
		// Stage 2: navigation, and stage 6 delivery to each screen.
		nav(msgs.ScreenPackageDetail, "pkg-a", false),
		key("j"),
		nav(msgs.ScreenSettings, "", true),
		key("j"),
		nav(msgs.ScreenHelp, "", true),
		key("j"),
		nav(msgs.ScreenRepositoryStatus, "", false),
		key("j"),
		{label: "back changed", msg: msgs.BackMsg{Changed: true}},
		{label: "back", msg: msgs.BackMsg{}},
		nav(msgs.ScreenPackageDetail, "pkg-a", false),
		{label: "review decision back", msg: msgs.ReviewDecisionBackMsg{Changed: true, Review: &domain.Review{PackageName: "pkg-a", Status: domain.ReviewApproved}, TraceCtx: bg}},
		nav(msgs.ScreenReview, "pkg-a", false),
		key("j"),
		nav(msgs.ScreenDashboard, "", true),
		// Dashboard rows, so a result can come back with a command.
		{label: "packages loaded on the Dashboard", msg: screens.PackagesLoadedMsg{Packages: []domain.PackageName{"pkg-a", "pkg-b"}, TraceCtx: bg}},
		{label: "review data pkg-a", msg: screens.ReviewDataMsg{Pkg: "pkg-a", Has: true, Status: domain.ReviewPending, TraceCtx: bg}},
		// Stage 5: every request kind idle, 1 ahead, 2 ahead, already queued.
	}
	for _, r := range []struct {
		name string
		mk   func(domain.PackageName) tea.Msg
	}{
		{"fetch", func(p domain.PackageName) tea.Msg { return msgs.FetchRequestedMsg{Pkg: p, TraceCtx: bg} }},
		{"analyze", func(p domain.PackageName) tea.Msg {
			return msgs.AnalyzeRequestedMsg{Pkg: p, ReviewID: "r1", TraceCtx: bg}
		}},
		{"build", func(p domain.PackageName) tea.Msg { return msgs.BuildRequestedMsg{Pkg: p, TraceCtx: bg} }},
		{"confirm", func(p domain.PackageName) tea.Msg {
			return msgs.ConfirmRequestedMsg{Pkg: p, BuildID: "b1", TraceCtx: bg}
		}},
		{"publish", func(p domain.PackageName) tea.Msg { return msgs.PublishRequestedMsg{Pkg: p, TraceCtx: bg} }},
		{"merge", func(p domain.PackageName) tea.Msg { return msgs.MergeRequestedMsg{Pkg: p, TraceCtx: bg} }},
	} {
		script = append(script, step{label: "setup: empty queue", setup: emptyQueue})
		for _, p := range []domain.PackageName{"q-1", "q-2", "q-3", "q-1"} {
			script = append(script, step{label: r.name + " " + string(p), msg: r.mk(p)})
		}
	}
	script = append(script, step{label: "setup: empty queue", setup: emptyQueue})
	script = append(script, []step{
		// Stage 4 and 6: results.
		{label: "fetch pkg-a", msg: msgs.FetchRequestedMsg{Pkg: "pkg-a", TraceCtx: bg}},
		{label: "analyze pkg-b", msg: msgs.AnalyzeRequestedMsg{Pkg: "pkg-b", ReviewID: "r2", TraceCtx: bg}},
		{label: "build pkg-c", msg: msgs.BuildRequestedMsg{Pkg: "pkg-c", TraceCtx: bg}},
		{label: "fetch pkg-a again", msg: msgs.FetchRequestedMsg{Pkg: "pkg-a", TraceCtx: bg}},
		{label: "analyze pkg-b again", msg: msgs.AnalyzeRequestedMsg{Pkg: "pkg-b", TraceCtx: bg}},
		{label: "build pkg-c again", msg: msgs.BuildRequestedMsg{Pkg: "pkg-c", TraceCtx: bg}},
		{label: "confirm pkg-d", msg: msgs.ConfirmRequestedMsg{Pkg: "pkg-d", BuildID: "b1", TraceCtx: bg}},
		{label: "confirm pkg-d again", msg: msgs.ConfirmRequestedMsg{Pkg: "pkg-d", TraceCtx: bg}},
		{label: "publish pkg-e", msg: msgs.PublishRequestedMsg{Pkg: "pkg-e", TraceCtx: bg}},
		{label: "publish pkg-e again", msg: msgs.PublishRequestedMsg{Pkg: "pkg-e", TraceCtx: bg}},
		{label: "merge pkg-f", msg: msgs.MergeRequestedMsg{Pkg: "pkg-f", TraceCtx: bg}},
		{label: "merge pkg-f again", msg: msgs.MergeRequestedMsg{Pkg: "pkg-f", TraceCtx: bg}},
		{label: "pkg-a completed with its review", msg: msgs.OperationCompletedMsg{OpID: "pkg-a", Result: "Fetched pkg-a", Review: &domain.Review{PackageName: "pkg-a", Status: domain.ReviewPending}, TraceCtx: bg}},
		{label: "pkg-b failed", msg: msgs.OperationFailedMsg{OpID: "pkg-b", Kind: "analyze", Err: errors.New("analyzer exploded"), TraceCtx: bg}},
		{label: "stale cancel", msg: msgs.OperationCancelledMsg{OpID: "nope", TraceCtx: bg}},
		{label: "stale cancel no ctx", msg: msgs.OperationCancelledMsg{OpID: "nope"}},
		key("ctrl+t"),
		{label: "pkg-c cancelled", msg: msgs.OperationCancelledMsg{OpID: "pkg-c", TraceCtx: bg}},
		{label: "pkg-d completed (no text)", msg: msgs.OperationCompletedMsg{OpID: "pkg-d", Result: 42, TraceCtx: bg}},
		{label: "pkg-e completed (empty text)", msg: msgs.OperationCompletedMsg{OpID: "pkg-e", Result: "", TraceCtx: bg}},
		// Stage 6: results reach screens other than the active one; the
		// Review screen still holds pkg-a and reloads on its results.
		{label: "setup: empty queue", setup: emptyQueue},
		nav(msgs.ScreenPackageDetail, "pkg-a", false),
		{label: "build pkg-a from Detail", msg: msgs.BuildRequestedMsg{Pkg: "pkg-a", TraceCtx: bg}},
		{label: "pkg-a failed", msg: msgs.OperationFailedMsg{OpID: "pkg-a", Kind: "build", Err: errors.New("makepkg exited 4"), TraceCtx: bg}},
		{label: "fetch pkg-a from Detail", msg: msgs.FetchRequestedMsg{Pkg: "pkg-a", TraceCtx: bg}},
		{label: "pkg-a completed", msg: msgs.OperationCompletedMsg{OpID: "pkg-a", Result: "Fetched pkg-a", TraceCtx: bg}},
		// Stage 6: startup data reaches the Dashboard from another screen.
		nav(msgs.ScreenSettings, "", true),
		{label: "packages loaded", msg: screens.PackagesLoadedMsg{}},
		{label: "review data", msg: screens.ReviewDataMsg{}},
		{label: "aur update", msg: msgs.AURUpdateMsg{}},
		// Stage 1: ctrl+t with nothing to cancel drops pending ones and goes on.
		{label: "setup: active without cancel, one pending", setup: noCancelQueue},
		key("ctrl+t"),
		// Stage 1 and 2: quitting, over a Dashboard with rows.
		nav(msgs.ScreenDashboard, "", true),
		{label: "packages loaded again", msg: screens.PackagesLoadedMsg{Packages: []domain.PackageName{"pkg-a", "pkg-b"}, TraceCtx: bg}},
		{label: "review data pkg-a again", msg: screens.ReviewDataMsg{Pkg: "pkg-a", Has: true, Status: domain.ReviewPending, TraceCtx: bg}},
		{label: "review data pkg-b", msg: screens.ReviewDataMsg{Pkg: "pkg-b", Has: true, Status: domain.ReviewPending, TraceCtx: bg}},
		key("j"),
		key("ctrl+q"),
		key("k"),
		key("esc"),
		key("ctrl+c"),
		key("y"),
		{label: "quit requested with an operation running", msg: msgs.QuitRequestedMsg{}},
		key("enter"),
		{label: "setup: empty queue", setup: emptyQueue},
		{label: "quit requested, nothing running", msg: msgs.QuitRequestedMsg{}},
		key("ctrl+c"),
	}...)
	return script
}

var clockRE = regexp.MustCompile(`\b\d{1,2}:\d{2}\b`)

// TestCharacterizeUpdate529 runs the script and compares the record with
// the golden file.
func TestCharacterizeUpdate529(t *testing.T) {
	root := t.TempDir()
	d := Dependencies{}
	d.Config.RootPath = root
	d.Config.ActivityLogPath = filepath.Join(root, "activity.log")
	app := NewApp(d)
	app.after = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return timerMsg{d: d, msg: fn(time.Time{})} }
	}

	var b strings.Builder
	for i, s := range characterizeScript() {
		fmt.Fprintf(&b, "=== %02d %s\n", i+1, s.label)
		if s.setup != nil {
			app = s.setup(app)
		} else {
			msg := s.msg
			if s.msgFn != nil {
				msg = s.msgFn(app)
			}
			m, cmd := app.Update(msg)
			next, ok := m.(App)
			if !ok {
				t.Fatalf("step %d: Update returned %T", i+1, m)
			}
			app = next
			fmt.Fprintf(&b, "cmd: %s\n", describeCmd(cmd))
		}
		b.WriteString(describeApp(app))
	}
	// The workspace is a temporary directory: its path differs per run.
	got := strings.ReplaceAll(b.String(), root, "<root>")

	golden := filepath.Join("testdata", "characterize529.golden")
	if *updateGolden529 {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden529 to create it): %v", err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("differs from %s at line %d:\n got: %q\nwant: %q", golden, i+1, g, w)
			}
		}
	}
}

// describeOp records every field of an Operation the queue keeps, except
// the context itself.
func describeOp(op Operation) string {
	return fmt.Sprintf("{%s %s pkg=%s review=%q build=%q cancel=%v}", op.ID, op.Kind, op.Pkg, op.ReviewID, op.BuildID, op.Cancel != nil)
}

// describeApp records App's state and screen after a step.
func describeApp(a App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "screen: %v history: %v\n", a.screen, a.history)
	fmt.Fprintf(&b, "toast: %q (id %d) err: %v quitModal: %v\n", a.toast, a.toastID, a.err, a.quitConfirmActive)
	if a.ops.active != nil {
		fmt.Fprintf(&b, "active: %s\n", describeOp(*a.ops.active))
	} else {
		b.WriteString("active: none\n")
	}
	var pending []string
	for _, p := range a.ops.pending {
		pending = append(pending, describeOp(p))
	}
	fmt.Fprintf(&b, "pending: %v\n", pending)
	for _, l := range a.operationsPanelLanes() {
		fmt.Fprintf(&b, "lane: %+v\n", l)
	}
	b.WriteString("view:\n")
	for _, l := range strings.Split(ansi.Strip(a.View()), "\n") {
		b.WriteString(strings.TrimRight("| "+clockRE.ReplaceAllString(l, "HH:MM"), " ") + "\n")
	}
	return b.String()
}

// describeCmd runs cmd, flattening batches, and describes what it
// produces. A panic (a nil dependency in a screen's command) or a command
// still running after a second is recorded as such: F1 does not change
// what commands do, only that update returns them.
func describeCmd(cmd tea.Cmd) string {
	if cmd == nil {
		return "nil"
	}
	type result struct {
		msg tea.Msg
		err string
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Sprintf("panic: %v", r)}
			}
		}()
		ch <- result{msg: cmd()}
	}()
	select {
	case r := <-ch:
		if r.err != "" {
			return r.err
		}
		return describeMsg(r.msg)
	case <-time.After(time.Second):
		return "blocked"
	}
}

// describeMsg records a message a command produced: its type, and the
// fields that matter for the known ones.
func describeMsg(m tea.Msg) string {
	switch m := m.(type) {
	case nil:
		return "msg nil"
	case tea.BatchMsg:
		var parts []string
		for _, c := range m {
			parts = append(parts, describeCmd(c))
		}
		return "batch[" + strings.Join(parts, ", ") + "]"
	case timerMsg:
		return fmt.Sprintf("timer(%s → %s)", m.d, describeMsg(m.msg))
	case clearToastMsg:
		return fmt.Sprintf("clearToast(%d)", m.id)
	case msgs.OperationFailedMsg:
		return fmt.Sprintf("OperationFailed(%s %s: %v)", m.OpID, m.Kind, m.Err)
	case msgs.OperationCompletedMsg:
		if s, ok := m.Result.(string); ok {
			return fmt.Sprintf("OperationCompleted(%s %q)", m.OpID, s)
		}
		return fmt.Sprintf("OperationCompleted(%s %T)", m.OpID, m.Result)
	case msgs.OperationCancelledMsg:
		return fmt.Sprintf("OperationCancelled(%s)", m.OpID)
	case msgs.ToastMsg:
		return fmt.Sprintf("Toast(%q)", m.Text)
	}
	return fmt.Sprintf("%T", m)
}
