package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/debug"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/overlay"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/ui/screens"
	"github.com/pramod/aur-curator/internal/version"
	"github.com/pramod/aur-curator/internal/widgets"
)

var toastDuration = 4 * time.Second

// startupNoticeDuration is how long the startup notice (#311) stays up.
// Longer than toastDuration: it appears while the Dashboard is still
// loading, and it reports something that happened before this session.
const startupNoticeDuration = 15 * time.Second

// startupNoticeMsg carries the startup notice. Its own type, not ToastMsg,
// because it is shown for startupNoticeDuration instead of toastDuration.
type startupNoticeMsg struct{ text string }

// quitKeys is the set of key bindings that quit the application.
// Add keys here to support additional quit shortcuts.
var quitKeys = []string{"ctrl+c", "ctrl+q"}

// App is the single Bubble Tea model that owns navigation and
// cross-screen state. Each screen in the nav tree is a sub-model that
// only knows how to render its own slice of state. App is the only model
// allowed to change which screen is active.
type App struct {
	screen  msgs.Screen
	history []msgs.Screen // back-navigation stack

	// shared, cross-screen state
	ops OperationsQueue // single global queue — no concurrent builds

	// one sub-model per screen
	dashboard  screens.DashboardModel
	detail     screens.PackageDetailModel
	repoStatus screens.RepositoryStatusModel
	settings   screens.SettingsModel
	review     screens.ReviewScreen
	help       screens.HelpModel

	toast         string
	toastID       int    // incremented each time a.toast is set; guards against stale dismiss timers
	startupNotice string // one-time toast at startup (#311); see WithStartupNotice
	err           error

	// quit-confirm state — active only while the modal is shown over
	// the current screen; cleared on confirm or cancel.
	quitConfirmActive bool
	quitConfirmModal  widgets.ConfirmModal

	// cached terminal dimensions — forwarded to screens constructed
	// after startup (see REV-001).
	width  int
	height int

	// lastSize is the content-area size most recently forwarded to the
	// screens. syncScreenSize re-forwards whenever screenWindowSizeMsg()
	// differs from it.
	lastSize tea.WindowSizeMsg

	// dependencies
	deps deps.Dependencies

	// after schedules a message after a delay: tea.Tick when nil. A test
	// seam (#529): the characterization test records the toast timer
	// instead of waiting for it.
	after func(time.Duration, func(time.Time) tea.Msg) tea.Cmd
}

// NewApp constructs a new App with the given dependencies.
func NewApp(deps Dependencies) App {
	return App{
		screen:     msgs.ScreenDashboard,
		dashboard:  screens.NewDashboardModel(deps),
		detail:     screens.NewPackageDetailModel(deps),
		repoStatus: screens.NewRepositoryStatusModel(deps),
		settings:   screens.NewSettingsModel(deps),
		help:       screens.NewHelpModel(),
		deps:       deps,
	}
}

// Init starts the Dashboard's first load, and the startup notice when
// there is one.
func (a App) Init() tea.Cmd {
	if cmd := a.startupNoticeCmd(); cmd != nil {
		return tea.Batch(a.dashboard.Init(), cmd)
	}
	return a.dashboard.Init()
}

// WithStartupNotice returns a with a one-time toast shown when the program
// starts — e.g. startup recovery of interrupted builds (#311). Empty text
// shows nothing.
func (a App) WithStartupNotice(text string) App {
	a.startupNotice = text
	return a
}

// toolDone reports a diff tool or pager that failed (#488): the toast
// gives the last line of its stderr, or the error, and the error log the
// whole of both. A tool that exited cleanly needs nothing.
func (a App) toolDone(msg msgs.ToolDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err == nil {
		return a, nil
	}
	stderr := strings.TrimSpace(msg.Stderr)
	_ = errorlog.Log(context.Background(), a.deps.Config.RootPath, "tool",
		sanitize.ForMessage(fmt.Sprintf("%s for %q: %v: %s", msg.What, msg.Pkg, msg.Err, stderr)))
	reason := msg.Err.Error()
	if i := strings.LastIndex(stderr, "\n"); stderr != "" {
		reason = stderr[i+1:]
	}
	return a.setToast(sanitize.ForMessage(msg.What + " failed: " + reason))
}

// startupNoticeCmd emits the startup notice as a ToastMsg, or nil if there
// is none.
func (a App) startupNoticeCmd() tea.Cmd {
	if a.startupNotice == "" {
		return nil
	}
	text := a.startupNotice
	return func() tea.Msg { return startupNoticeMsg{text: text} }
}

// setToast shows text as a toast for toastDuration (setToastFor).
func (a App) setToast(text string) (App, tea.Cmd) {
	return a.setToastFor(text, toastDuration)
}

// setToastFor sets a.toast for d and schedules its auto-dismissal. Every
// call increments toastID so that any previously scheduled dismiss timer
// becomes stale and is silently ignored when it eventually fires.
func (a App) setToastFor(text string, d time.Duration) (App, tea.Cmd) {
	a.toast = text
	a.toastID++
	id := a.toastID
	tick := a.after
	if tick == nil {
		tick = tea.Tick
	}
	return a, tick(d, func(time.Time) tea.Msg {
		return clearToastMsg{id: id}
	})
}

// Update is the top-level message handler. It delegates to update, then
// re-syncs every screen's size, so any change to the chrome around the
// screen area (toast, error, ops panel, resize, navigation) reaches all
// screens through this single path rather than per-site forwarding.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := a.update(msg)
	next, ok := model.(App)
	if !ok {
		return model, cmd
	}
	next, sizeCmd := next.syncScreenSize()
	return next, tea.Batch(cmd, sizeCmd)
}

// syncScreenSize forwards the current content-area size to every screen
// when it has changed since the last forward. Value receiver, returns the
// updated App: the lastSize write must land on the model Bubble Tea keeps.
func (a App) syncScreenSize() (App, tea.Cmd) {
	if a.width == 0 || a.height == 0 {
		return a, nil // no real window size yet
	}
	size := a.screenWindowSizeMsg()
	if size == a.lastSize {
		return a, nil
	}
	a.lastSize = size
	debug.Logf("app", "size sync: content W=%d H=%d", size.Width, size.Height)

	var cmds []tea.Cmd
	var c tea.Cmd
	a.dashboard, c = a.dashboard.Update(size)
	cmds = append(cmds, c)
	a.detail, c = a.detail.Update(size)
	cmds = append(cmds, c)
	a.repoStatus, c = a.repoStatus.Update(size)
	cmds = append(cmds, c)
	a.settings, c = a.settings.Update(size)
	cmds = append(cmds, c)
	a.help, c = a.help.Update(size)
	cmds = append(cmds, c)
	// Review is constructed lazily on first navigation; a zero-value
	// ReviewScreen must not receive a size.
	if a.review.Package() != "" {
		var rc tea.Cmd
		a.review, rc = a.review.Update(size)
		cmds = append(cmds, rc)
	}
	return a, tea.Batch(cmds...)
}

// update is a dispatcher over one handler per stage, in order
// (developer-guide.md § Update, #529).
func (a App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 1. Window size and the keys App owns. Not done: the message goes
	// on with the App handleKeys returned (ctrl+t with nothing running
	// has cleared the pending operations).
	next, cmd, done := a.handleKeys(msg)
	if done {
		return next, cmd
	}
	a = next
	// 2 and 3. Navigation, toasts and errors: they stop every message
	// they handle, so they never pass on a changed App.
	if m, cmd, done := a.handleNavigation(msg); done {
		return m, cmd
	}
	if m, cmd, done := a.handleToasts(msg); done {
		return m, cmd
	}
	// 4. Operation results: never stops a message; results go on to
	// every screen but Help, with the App and queue command returned
	// here.
	a, queueCmd := a.handleOperationResult(msg)
	// 5. Requests: every one is queued and stops here.
	if m, cmd, done := a.handleRequest(msg); done {
		return m, cmd
	}
	// 6. Everything else, and the results, to the screens.
	return a.forward(msg, queueCmd)
}

// View renders the active screen body, with the Operations Panel and
// Status Bar below it. Toast and error lines appear above the body.
// The header is always shown; the dashboard body is wrapped in a box.
func (a App) View() string {
	var top []string

	// Toast line is always reserved at a fixed position to prevent
	// layout shift when a toast appears or clears.
	top = append(top, a.toast)

	// Error stays as a plain stacked line below the toast.
	if a.err != nil {
		top = append(top, "Error: "+a.err.Error())
	}

	// Header is always shown above the body.
	top = append(top, a.viewHeader())

	body := capBody(a.viewScreen(), a.screenWindowSizeMsg().Height)

	if a.quitConfirmActive {
		body = overlay.Render(body, a.quitConfirmModal.View(), a.width, a.height)
	}

	bottom := []string{
		widgets.RenderOperationsPanel(a.operationsPanelLanes(), max(a.width-6, 40), a.operationsPanelFooter()),
		widgets.RenderStatusBar(max(a.width-6, 40), a.statusBarState()),
	}

	var out []string
	out = append(out, top...)
	out = append(out, body)
	out = append(out, bottom...)
	frame := strings.Join(out, "\n")
	if h := lipgloss.Height(frame); a.height > 0 && h > a.height {
		debug.Logf("app", "View: frame %d rows > terminal %d — top %d row(s) cut, toast %q", h, a.height, h-a.height, a.toast)
	}
	return frame
}

// capBody limits body to rows lines, cutting from the bottom, so a screen
// that renders taller than its allotment can never push App's top rows —
// the toast and the header — off-screen (#329). rows <= 0 (no window size
// yet) leaves body unchanged.
func capBody(body string, rows int) string {
	if rows <= 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= rows {
		return body
	}
	return strings.Join(lines[:rows], "\n")
}

// viewHeader renders the header: the title and running version, the
// workspace, and on a second line the Dashboard's last successful load,
// "—" before the first (#532).
func (a App) viewHeader() string {
	workspace := a.deps.Config.RootPath
	refresh := "—"
	if t := a.dashboard.LastRefresh(); !t.IsZero() {
		refresh = t.Format("15:04")
	}

	// The running version, dimmed: the same value --version prints (#481).
	title := theme.HeaderStyle().Render("aur-curator") + " " + theme.DimmedStyle().Render(version.Version)
	ws := theme.SubtleStyle().Render("Workspace: " + workspace)
	refreshLine := theme.SubtleStyle().Render("Last Refresh: " + refresh)

	return title + strings.Repeat(" ", 10) + ws + "\n" + refreshLine
}

// operationsPanelLanes converts a.ops lane states into the [4]widgets.LaneStatus
// expected by the Operations Panel widget. Fixed order: Fetch, Analyze, Build, Publish.
func (a App) operationsPanelLanes() [4]widgets.LaneStatus {
	lanes := [4]widgets.LaneStatus{
		{Name: "Fetch", Status: string(a.ops.LaneState(LaneFetch).Status)},
		{Name: "Analyze", Status: string(a.ops.LaneState(LaneAnalyze).Status)},
		{Name: "Build", Status: string(a.ops.LaneState(LaneBuild).Status)},
		{Name: "Publish", Status: string(a.ops.LaneState(LanePublish).Status)},
	}
	for i, l := range []Lane{LaneFetch, LaneAnalyze, LaneBuild, LanePublish} {
		ls := a.ops.LaneState(l)
		if len(ls.Log) > 0 {
			lanes[i].LastLine = ls.Log[len(ls.Log)-1].Message
		}
		lanes[i].Pkg = ls.Pkg
		lanes[i].Kind = ls.LastKind
	}
	return lanes
}

// statusBarState builds the data needed to render the Status Bar.
func (a App) statusBarState() widgets.StatusBarState {
	state := widgets.StatusBarState{Pending: a.ops.LenPending()}
	if a.ops.active != nil {
		state.ActiveOp = fmt.Sprintf("%s %s", a.ops.active.Kind, a.ops.active.Pkg)
	}
	// Find the most recent log entry across all lanes for LastResult.
	var lastMsg string
	for _, l := range []Lane{LaneFetch, LaneAnalyze, LaneBuild, LanePublish} {
		ls := a.ops.LaneState(l)
		if len(ls.Log) > 0 {
			lastMsg = ls.Log[len(ls.Log)-1].Message
		}
	}
	state.LastResult = lastMsg
	state.Warning = skippedWarning(a.dashboard.Skipped())
	return state
}

// skippedWarningNames is how many skipped packages the status bar names
// before "+N more".
const skippedWarningNames = 3

// skippedWarning is the status bar's text for packages the Dashboard's
// last load left out (tui-dashboard-wireframe.md § 7, #513), or "" when
// there are none.
func skippedWarning(skipped []domain.PackageName) string {
	n := len(skipped)
	if n == 0 {
		return ""
	}
	noun := "packages"
	if n == 1 {
		noun = "package"
	}
	names := make([]string, 0, skippedWarningNames+1)
	for _, pkg := range skipped[:min(n, skippedWarningNames)] {
		names = append(names, string(pkg))
	}
	if n > skippedWarningNames {
		names = append(names, fmt.Sprintf("+%d more", n-skippedWarningNames))
	}
	return fmt.Sprintf("%d %s not loaded (see the error log): %s", n, noun, strings.Join(names, ", "))
}

// viewScreen renders the currently active screen only — split out from
// View() so the toast line can wrap it without duplicating the switch.
func (a App) viewScreen() string {
	switch a.screen {
	case msgs.ScreenDashboard:
		return a.renderDashboardBox()
	case msgs.ScreenPackageDetail:
		return a.detail.View()
	case msgs.ScreenRepositoryStatus:
		return a.repoStatus.View()
	case msgs.ScreenSettings:
		return a.settings.View()
	case msgs.ScreenHelp:
		return a.help.View()
	case msgs.ScreenReview:
		return a.review.View()
	default:
		return ""
	}
}

// renderDashboardBox wraps the dashboard body in a full-width box. When
// not even one package row fits, it shows one line saying so instead, so
// the selected row is never cut off by capBody
// (tui-dashboard-wireframe.md § 6, #450).
func (a App) renderDashboardBox() string {
	if a.dashboard.TooSmall() {
		lines, cols := a.dashboard.RecommendedSize(a.height)
		return fmt.Sprintf("Terminal too small: At least %d lines and %d columns recommended.", lines, cols)
	}
	body := a.dashboard.View()
	if body == "" {
		return ""
	}
	return widgets.WrapScreenBody(a.width, body)
}

// handleKeys is update's stage 1 (#529): the window size, and the keys
// App handles before any screen. done reports that the message stops
// here; when it is false the message goes on with the returned App.
func (a App) handleKeys(msg tea.Msg) (App, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		debug.Logf("app", "WindowSizeMsg received: W=%d H=%d", msg.Width, msg.Height)
		next, cmd := a.handleWindowSize(msg)
		return next, cmd, true
	case tea.KeyMsg:
		if a.quitConfirmActive {
			return a.handleQuitConfirmKey(msg)
		}
		for _, k := range quitKeys {
			if msg.String() == k {
				next, cmd := a.requestQuit()
				return next, cmd, true
			}
		}
		if msg.String() == "ctrl+t" {
			return a.handleCancelKey()
		}
	}
	return a, nil, false
}

// handleNavigation is update's stage 2 (#529): navigation is handled
// by App alone; screens never navigate themselves. done reports that
// the message stops here.
func (a App) handleNavigation(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case msgs.BackMsg:
		m, cmd := a.handleBack(msg, nil)
		return m, cmd, true
	case msgs.ReviewDecisionBackMsg:
		m, cmd := a.handleBack(msgs.BackMsg{Changed: msg.Changed}, &msg)
		return m, cmd, true
	case msgs.NavigateMsg:
		m, cmd := a.handleNavigate(msg)
		return m, cmd, true
	case msgs.QuitRequestedMsg:
		m, cmd := a.requestQuit()
		return m, cmd, true
	}
	return a, nil, false
}

// handleToasts is update's stage 3 (#529): toasts and errors are
// consumed by App so they don't reach screens. done reports that the
// message stops here.
func (a App) handleToasts(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case msgs.ToastMsg:
		m, cmd := a.setToast(msg.Text)
		return m, cmd, true
	case msgs.ToolDoneMsg:
		m, cmd := a.toolDone(msg)
		return m, cmd, true
	case startupNoticeMsg:
		m, cmd := a.setToastFor(msg.text, startupNoticeDuration)
		return m, cmd, true
	case clearToastMsg:
		if msg.id == a.toastID {
			a.toast = ""
		}
		return a, nil, true
	case ErrorMsg:
		a.err = msg.Err
		return a, nil, true
	}
	return a, nil, false
}

// handleOperationResult is update's stage 4 (#529): App owns the
// queue. It records a result and starts the next operation, and never
// stops the message, which goes on to every screen but Help; the
// returned command is batched with the screens' there.
func (a App) handleOperationResult(msg tea.Msg) (App, tea.Cmd) {
	switch msg := msg.(type) {
	case msgs.OperationCompletedMsg:
		return a.onOperationCompleted(msg)
	case msgs.OperationFailedMsg:
		return a.onOperationFailed(msg)
	case msgs.OperationCancelledMsg:
		return a.onOperationCancelled(msg)
	}
	return a, nil
}

// onOperationCompleted completes the active operation, starts the next
// and shows the result's text, if any, as a toast.
func (a App) onOperationCompleted(msg msgs.OperationCompletedMsg) (App, tea.Cmd) {
	var queueCmd tea.Cmd
	op := *a.ops.active
	a.ops.Complete()
	var message string
	if s, ok := msg.Result.(string); ok && s != "" {
		message = s
	} else {
		message = "Completed"
	}
	a.ops.MarkCompleted(op, message)
	var next App
	next, queueCmd = a.drainQueueCmd()
	if reason, ok := msg.Result.(string); ok && reason != "" {
		var toastCmd tea.Cmd
		next, toastCmd = next.setToast(reason)
		queueCmd = tea.Batch(queueCmd, toastCmd)
	}
	a = next
	return a, queueCmd
}

// onOperationFailed logs the failure, completes the active operation,
// starts the next and shows the error as a toast.
func (a App) onOperationFailed(msg msgs.OperationFailedMsg) (App, tea.Cmd) {
	var queueCmd tea.Cmd
	op := *a.ops.active
	_ = errorlog.Log(
		msg.TraceCtx,
		a.deps.Config.RootPath,
		op.Kind.String(),
		fmt.Sprintf("%q: %s", op.Pkg, msg.Err.Error()),
	)
	a.ops.Complete()
	a.ops.MarkFailed(op, msg.Err)
	var next App
	next, queueCmd = a.drainQueueCmd()
	reason := "Operation failed: " + msg.Err.Error()
	var toastCmd tea.Cmd
	next, toastCmd = next.setToast(reason)
	queueCmd = tea.Batch(queueCmd, toastCmd)
	a = next
	return a, queueCmd
}

// onOperationCancelled records a cancel of the active operation, starts
// the next and shows a toast.
func (a App) onOperationCancelled(msg msgs.OperationCancelledMsg) (App, tea.Cmd) {
	var queueCmd tea.Cmd
	// A cancel that no longer matches the active operation — none
	// active, or a different one — is stale (late or duplicate) and
	// must neither panic nor cancel the wrong operation (#335).
	if a.ops.active == nil || a.ops.active.ID != msg.OpID {
		if msg.TraceCtx != nil {
			trace.Log(msg.TraceCtx, "msg", "type", "OperationCancelledMsg", "opID", msg.OpID, "result", "ignored-stale")
		}
		return a, nil
	}
	op := *a.ops.active
	_ = aurlog.Log(
		msg.TraceCtx,
		a.deps.Config.ActivityLogPath,
		op.Kind.String(),
		string(op.Pkg),
		"status", "cancelled",
		"reason", "user_abort",
	)
	a.ops.MarkCancelled(op)
	a.ops.Complete()
	var next App
	next, queueCmd = a.drainQueueCmd()
	var toastCmd tea.Cmd
	next, toastCmd = next.setToast("Operation cancelled: " + string(op.Pkg))
	queueCmd = tea.Batch(queueCmd, toastCmd)
	a = next
	return a, queueCmd
}

// handleRequest is update's stage 5 (#529): a user's request is queued,
// never run directly. done reports that the message stops here; every
// request does.
func (a App) handleRequest(msg tea.Msg) (App, tea.Cmd, bool) {
	var op Operation
	switch msg := msg.(type) {
	case msgs.FetchRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpFetch, Pkg: msg.Pkg, TraceCtx: msg.TraceCtx}
	case msgs.AnalyzeRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpAnalyze, Pkg: msg.Pkg, ReviewID: msg.ReviewID, TraceCtx: msg.TraceCtx}
	case msgs.BuildRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpBuild, Pkg: msg.Pkg, TraceCtx: msg.TraceCtx}
	case msgs.ConfirmRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpConfirm, Pkg: msg.Pkg, BuildID: msg.BuildID, TraceCtx: msg.TraceCtx}
	case msgs.PublishRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpPublish, Pkg: msg.Pkg, TraceCtx: msg.TraceCtx}
	case msgs.MergeRequestedMsg:
		op = Operation{ID: string(msg.Pkg), Kind: OpMerge, Pkg: msg.Pkg, TraceCtx: msg.TraceCtx}
	default:
		return a, nil, false
	}
	next, cmd := a.enqueueRequest(op)
	return next, cmd, true
}

// enqueueRequest queues op and starts it if nothing is running, with a
// toast when it is already queued or has operations ahead of it. It wraps
// op.TraceCtx with context.WithCancel and stores the cancel function on
// the Operation, for ^T.
func (a App) enqueueRequest(op Operation) (App, tea.Cmd) {
	ctx, cancel := context.WithCancel(op.TraceCtx)
	op.TraceCtx, op.Cancel = ctx, cancel
	ok, depth := a.ops.Enqueue(op)
	if !ok {
		return a.setToast("Already queued: " + string(op.Pkg))
	}
	a, queueCmd := a.drainQueueCmd()
	if depth > 0 {
		items := "items"
		if depth == 1 {
			items = "item"
		}
		var toastCmd tea.Cmd
		a, toastCmd = a.setToast(fmt.Sprintf("%d %s ahead of you in queue", depth, items))
		return a, tea.Batch(queueCmd, toastCmd)
	}
	return a, queueCmd
}

// forward is update's stage 6 (#529): it delivers msg to the screens,
// batching queueCmd (from stage 4) with their commands.
func (a App) forward(msg tea.Msg, queueCmd tea.Cmd) (App, tea.Cmd) {
	// Broadcast operation completion/failure/cancellation to every screen
	// except Help (it has no derived state), so each can independently
	// refresh its derived state regardless of which screen is currently
	// active. All other message types continue to reach only the active
	// screen (e.g. NavigateMsg).
	var cmd tea.Cmd
	switch msg.(type) {
	case msgs.OperationCompletedMsg, msgs.OperationFailedMsg, msgs.OperationCancelledMsg:
		var cmds []tea.Cmd
		var d screens.DashboardModel
		d, c := a.dashboard.Update(msg)
		if c != nil {
			cmds = append(cmds, c)
		}
		a.dashboard = d
		var dt screens.PackageDetailModel
		dt, c = a.detail.Update(msg)
		if c != nil {
			cmds = append(cmds, c)
		}
		a.detail = dt
		var rv screens.ReviewScreen
		rv, c = a.review.Update(msg)
		if c != nil {
			cmds = append(cmds, c)
		}
		a.review = rv
		var rs screens.RepositoryStatusModel
		rs, c = a.repoStatus.Update(msg)
		if c != nil {
			cmds = append(cmds, c)
		}
		a.repoStatus = rs
		var s screens.SettingsModel
		s, c = a.settings.Update(msg)
		if c != nil {
			cmds = append(cmds, c)
		}
		a.settings = s
		cmd = tea.Batch(cmds...)
	case screens.PackagesLoadedMsg, screens.ReviewDataMsg, msgs.AURUpdateMsg:
		// Dashboard-only consumers: these startup messages must reach the
		// dashboard regardless of the active screen, otherwise navigating
		// away before they resolve drops them (see #286).
		a.dashboard, cmd = a.dashboard.Update(msg)
	default:
		switch a.screen {
		case msgs.ScreenDashboard:
			a.dashboard, cmd = a.dashboard.Update(msg)
		case msgs.ScreenPackageDetail:
			a.detail, cmd = a.detail.Update(msg)
		case msgs.ScreenReview:
			a.review, cmd = a.review.Update(msg)
		case msgs.ScreenRepositoryStatus:
			a.repoStatus, cmd = a.repoStatus.Update(msg)
		case msgs.ScreenSettings:
			a.settings, cmd = a.settings.Update(msg)
		case msgs.ScreenHelp:
			a.help, cmd = a.help.Update(msg)
		}
	}
	if queueCmd != nil {
		cmd = tea.Batch(cmd, queueCmd)
	}
	return a, cmd
}

// handleQuitConfirmKey sends a key to the open quit modal, which takes
// every key: yes quits, no closes the modal.
func (a App) handleQuitConfirmKey(msg tea.KeyMsg) (App, tea.Cmd, bool) {
	result, cmd := a.quitConfirmModal.Update(msg)
	if result.Confirmed {
		return a, tea.Quit, true
	}
	if result.Cancelled {
		a.quitConfirmActive = false
		a.quitConfirmModal = widgets.ConfirmModal{}
		return a, nil, true
	}
	return a, cmd, true
}

// handleCancelKey handles ctrl+t: it cancels the running operation, or
// with none running drops the pending ones and lets the key go on to the
// screen.
func (a App) handleCancelKey() (App, tea.Cmd, bool) {
	if a.ops.active != nil {
		trace.Log(a.ops.active.TraceCtx, "key", "action", "fired", "op", "cancel", "pkg", string(a.ops.active.Pkg))
	}
	if a.ops.CancelActive() {
		return a, nil, true
	}
	if a.ops.LenPending() > 0 {
		a.ops.ClearPending()
	}
	return a, nil, false
}

// handleWindowSize forwards window-size changes to the active sub-model.
func (a App) handleWindowSize(msg tea.WindowSizeMsg) (App, tea.Cmd) {
	a.width = msg.Width
	a.height = msg.Height
	debug.Logf("app", "forwarding resize to %s (content area: W=%d H=%d)", a.screen, msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
	var cmd tea.Cmd
	switch a.screen {
	case msgs.ScreenDashboard:
		debug.Logf("app", "  -> dashboard.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		a.dashboard, cmd = a.dashboard.Update(a.screenWindowSizeMsg())
	case msgs.ScreenPackageDetail:
		debug.Logf("app", "  -> detail.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		d, c := a.detail.Update(a.screenWindowSizeMsg())
		a.detail = d
		cmd = c
	case msgs.ScreenRepositoryStatus:
		debug.Logf("app", "  -> repoStatus.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		a.repoStatus, cmd = a.repoStatus.Update(a.screenWindowSizeMsg())
	case msgs.ScreenSettings:
		debug.Logf("app", "  -> settings.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		a.settings, cmd = a.settings.Update(a.screenWindowSizeMsg())
	case msgs.ScreenHelp:
		debug.Logf("app", "  -> help.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		a.help, cmd = a.help.Update(a.screenWindowSizeMsg())
	case msgs.ScreenReview:
		debug.Logf("app", "  -> review.Update(WindowSizeMsg, content W=%d H=%d)", msg.Width, max(a.height-a.topHeight()-a.bottomHeight(), 0))
		a.review, cmd = a.review.Update(a.screenWindowSizeMsg())
	}
	return a, cmd
}

// handleBack pops the history stack and navigates to the previous screen.
// When msg.Changed is true, re-initializes PackageDetailModel to reflect
// state modifications made on the source screen, and relays the change
// signal through the back-navigation chain to the Dashboard.
// If reviewMsg is non-nil (from a ReviewDecisionBackMsg), the Dashboard
// updates state from the in-memory review using the same trace context
// that originated the operation, instead of re-fetching from disk.
func (a App) handleBack(msg msgs.BackMsg, reviewMsg *msgs.ReviewDecisionBackMsg) (tea.Model, tea.Cmd) {
	if len(a.history) == 0 {
		return a, nil
	}
	prev := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.screen = prev
	{
		ctx := trace.Start(context.Background(), "nav_back")
		trace.Log(ctx, "handoff", "from", "BackMsg", "to", prev, "changed", msg.Changed, "pkg", a.detail.Package())
	}
	if prev == msgs.ScreenPackageDetail && msg.Changed {
		a.detail = a.detail.MarkChanged().Refresh()
		return a, a.detail.Init()
	}
	if prev == msgs.ScreenDashboard && msg.Changed {
		if reviewMsg != nil && reviewMsg.Review != nil {
			var ctx context.Context
			if reviewMsg.TraceCtx != nil {
				ctx = reviewMsg.TraceCtx
			} else {
				ctx = context.Background()
			}
			var cmd tea.Cmd
			a.dashboard, cmd = a.dashboard.UpdateFromReview(ctx, a.detail.Package(), reviewMsg.Review)
			return a, cmd
		}
		return a, a.dashboard.LoadReviewCmd(context.Background(), a.detail.Package())
	}
	a.detail = a.detail.Refresh()
	return a, nil
}

// requestQuit quits immediately when no operation is active; otherwise
// it opens the confirm-to-quit modal (#179). The single quit path for
// both App's own quitKeys and screen-emitted QuitRequestedMsg (#300).
func (a App) requestQuit() (App, tea.Cmd) {
	if a.ops.active == nil {
		return a, tea.Quit
	}
	op := *a.ops.active
	var text string
	if op.Pkg != "" {
		text = fmt.Sprintf("Background task running (%s): %s", op.Kind, op.Pkg)
	} else {
		text = fmt.Sprintf("Background task running (%s)", op.Kind)
	}
	a.quitConfirmActive = true
	a.quitConfirmModal = widgets.NewChoiceModal(text)
	a.quitConfirmModal.SetWidth(a.width)
	return a, a.quitConfirmModal.Focus()
}

// handleNavigate pushes the current screen onto history (or clears the
// history entirely for Nav Menu jumps) and switches to the target screen.
// Also clears any lingering toast and errors.
func (a App) handleNavigate(msg msgs.NavigateMsg) (tea.Model, tea.Cmd) {
	if msg.FromMenu {
		a.history = nil
	} else if msg.To != a.screen {
		a.history = append(a.history, a.screen)
	}
	a.screen = msg.To
	a.toast = ""
	a.err = nil

	switch msg.To {
	case msgs.ScreenDashboard:
		a.dashboard, _ = a.dashboard.Update(a.screenWindowSizeMsg())
		return a, nil
	case msgs.ScreenPackageDetail:
		var cmd, sizeCmd tea.Cmd
		a.detail, cmd = a.detail.Update(screens.PackageSelectedMsg{Pkg: msg.Package, ShowArtifacts: msg.ShowArtifacts})
		a.detail, sizeCmd = a.detail.Update(a.screenWindowSizeMsg())
		return a, tea.Batch(cmd, sizeCmd)
	case msgs.ScreenReview:
		if a.review.Package() != msg.Package {
			a.review = screens.NewReviewScreen(msg.Package, a.deps.Git, a.deps.ReviewDB, a.deps.Reviews, a.deps.Config)
		} else {
			a.review = a.review.Refresh()
		}
		a.review, _ = a.review.Update(a.screenWindowSizeMsg())
		return a, a.review.Init()
	case msgs.ScreenRepositoryStatus:
		a.repoStatus, _ = a.repoStatus.Update(a.screenWindowSizeMsg())
		return a, a.repoStatus.Init()
	case msgs.ScreenHelp:
		a.help, _ = a.help.Update(a.screenWindowSizeMsg())
		return a, nil
	}
	return a, nil
}

// screenWindowSizeMsg is the size a screen gets: the window less the
// chrome above (topHeight) and below (bottomHeight) it.
func (a App) screenWindowSizeMsg() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{
		Width:  a.width,
		Height: max(a.height-a.topHeight()-a.bottomHeight(), 0),
	}
}

// topHeight is the height of the chrome above the screen: the toast line,
// the error line when there is an error, and the header.
func (a App) topHeight() int {
	lines := []string{a.toast}
	if a.err != nil {
		lines = append(lines, "Error: "+a.err.Error())
	}
	lines = append(lines, a.viewHeader())
	return lipgloss.Height(strings.Join(lines, "\n"))
}

// bottomHeight is the height of the chrome below the screen: the
// Operations panel and the status bar, rendered as View renders them.
func (a App) bottomHeight() int {
	return lipgloss.Height(widgets.RenderOperationsPanel(a.operationsPanelLanes(), max(a.width-6, 40), a.operationsPanelFooter())) +
		lipgloss.Height(widgets.RenderStatusBar(max(a.width-6, 40), a.statusBarState()))
}

// operationsPanelFooter returns the footer text for the operations panel.
// Shows "^T Cancel" when an operation is active (cancellable), empty otherwise.
func (a App) operationsPanelFooter() string {
	if a.ops.active != nil {
		return "^T Cancel"
	}
	return ""
}

// executeOperation returns a tea.Cmd that runs op using the appropriate
// injected dependency, or returns an OperationCompletedMsg for no-op
// operation kinds.
//
// Nil dependencies fall back to stub behavior (returning an error) so
// existing end-to-end behavior is preserved.
func (a App) executeOperation(op Operation) tea.Cmd {
	ctx := op.TraceCtx
	return func() tea.Msg {
		switch op.Kind {
		case OpBuild:
			if a.deps.BuildEngine == nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("build: build engine not configured"), TraceCtx: ctx}
			}
			review, err := a.deps.Reviews.GetReview(ctx, op.Pkg)
			if err != nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("build: failed to get review for %s: %w", op.Pkg, err), TraceCtx: ctx}
			}
			if review.Status != domain.ReviewApproved {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("build: review status is %s, not approved", review.Status), TraceCtx: ctx}
			}
			// Build needs the reviewed commit in master — not upstream's
			// tip, which may have moved on since the review (#406).
			merged, err := a.deps.Git.IsMerged(ctx, op.Pkg, review.UpstreamCommit)
			if err != nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("build: failed to check merge state for %s: %w", op.Pkg, err), TraceCtx: ctx}
			}
			if !merged {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("build: %s not yet merged (reviewed commit %s is not in master)", op.Pkg, shortSHA(review.UpstreamCommit)), TraceCtx: ctx}
			}
			// Recorded in the activity log: the UI asked for confirmation if
			// there were local commits not on upstream (workflow.md § Local
			// Commits Not on Upstream); this records what was actually built.
			localCommits := "unknown"
			if n, lcErr := a.deps.Git.LocalOnlyCommits(ctx, op.Pkg); lcErr == nil {
				localCommits = fmt.Sprint(n)
			}
			build, err := a.deps.BuildEngine.RunBuild(ctx, op.Pkg)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return msgs.OperationCancelledMsg{OpID: op.ID, TraceCtx: ctx}
				}
				_ = aurlog.Log(ctx, a.deps.Config.ActivityLogPath, "build", string(op.Pkg), "status", "failed", "error", err.Error(), "localCommits", localCommits)
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: err, TraceCtx: ctx}
			}
			_ = aurlog.Log(ctx, a.deps.Config.ActivityLogPath, "build", string(op.Pkg), "status", string(build.Status), "localCommits", localCommits)
			return msgs.OperationCompletedMsg{OpID: op.ID, Result: fmt.Sprintf("%s: build succeeded", op.Pkg), Build: build, TraceCtx: ctx}
		case OpConfirm:
			fail := func(err error) tea.Msg { return a.gateFailed(ctx, op, "confirm", err) }
			ls, err := a.lastSuccess(op.Pkg)
			if err != nil {
				return fail(err)
			}
			if ls.Confirmed {
				return fail(errors.New("already confirmed"))
			}
			// The build confirmed is the build the Confirm modal showed
			// (#525; workflow.md note 9).
			if op.BuildID != "" && ls.ID != op.BuildID {
				return fail(errors.New("a newer build replaced the one you inspected; press c again"))
			}
			if err := a.requireBuildOfHead(ctx, op.Pkg, ls); err != nil {
				return fail(err)
			}
			now := time.Now()
			ls.Confirmed = true
			ls.ConfirmedAt = &now
			if err := a.deps.BuildDB.SaveLastSuccess(op.Pkg, ls); err != nil {
				return fail(fmt.Errorf("save last success: %w", err))
			}
			_ = aurlog.Log(ctx, a.deps.Config.ActivityLogPath, "confirm", string(op.Pkg), "status", "success")
			return msgs.OperationCompletedMsg{OpID: op.ID, TraceCtx: ctx}
		case OpPublish:
			fail := func(err error) tea.Msg { return a.gateFailed(ctx, op, "publish", err) }
			if a.deps.Publisher == nil {
				return fail(errors.New("not implemented yet"))
			}
			ls, err := a.lastSuccess(op.Pkg)
			if err != nil {
				return fail(err)
			}
			if !ls.Confirmed {
				return fail(errors.New("build not confirmed"))
			}
			if err := a.requireBuildOfHead(ctx, op.Pkg, ls); err != nil {
				return fail(err)
			}
			if err := a.deps.Publisher.Publish(ctx, string(op.Pkg)); err != nil {
				return fail(err)
			}
			now := time.Now()
			ls.Published = true
			ls.PublishedAt = &now
			if err := a.deps.BuildDB.SaveLastSuccess(op.Pkg, ls); err != nil {
				return fail(fmt.Errorf("save last success: %w", err))
			}
			_ = aurlog.Log(ctx, a.deps.Config.ActivityLogPath, "publish", string(op.Pkg), "status", "success")
			return msgs.OperationCompletedMsg{OpID: op.ID, TraceCtx: ctx}
		case OpFetch:
			if a.deps.Git == nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("fetch: git provider not configured"), TraceCtx: ctx}
			}
			ctx := op.TraceCtx
			res, err := review.FetchAndReview(ctx, a.deps.Git, a.deps.Reviews, op.Pkg)
			if err != nil {
				// A Ctrl+T cancel reaches here as context.Canceled (the git
				// runner keeps it, #417): report a cancel, not a failure —
				// as Build does. Analyze and Merge do the same.
				if errors.Is(err, context.Canceled) {
					return msgs.OperationCancelledMsg{OpID: op.ID, TraceCtx: ctx}
				}
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: err, TraceCtx: ctx}
			}
			switch {
			case res.EmptyResult:
				return msgs.OperationCompletedMsg{OpID: op.ID, TraceCtx: ctx}
			case res.ReviewStarted:
				return msgs.OperationCompletedMsg{OpID: op.ID, Result: fmt.Sprintf("New upstream commit %s — review started", shortSHA(res.ReviewCommit)), TraceCtx: ctx}
			case res.Changed:
				return msgs.OperationCompletedMsg{OpID: op.ID, Result: fmt.Sprintf("Upstream changed (refs: %s → %s)", res.Before, res.After), TraceCtx: ctx}
			default:
				return msgs.OperationCompletedMsg{OpID: op.ID, Result: "Already up to date — no new upstream commits since last fetch", TraceCtx: ctx}
			}
		case OpAnalyze:
			if a.deps.Reviews == nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("analyze: dependencies not configured"), TraceCtx: ctx}
			}
			ctx := op.TraceCtx
			gitCtx, gitCancel := git.GitTimeoutCtx(ctx)
			defer gitCancel()
			// Analyze runs on the review the screen showed, never a new
			// one: no fetch, no InitiateReview (#399).
			analysis, err := a.deps.Reviews.RunAnalyzers(gitCtx, op.Pkg, op.ReviewID)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return msgs.OperationCancelledMsg{OpID: op.ID, TraceCtx: ctx}
				}
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: err, TraceCtx: ctx}
			}
			trace.Log(gitCtx, "handoff", "to", "store", "from", "reviews", "via", "RunAnalyzers", "pkg", op.Pkg, "result", analysis.Status)
			return msgs.OperationCompletedMsg{OpID: op.ID, Result: analysis, TraceCtx: ctx}
		case OpMerge:
			if a.deps.Git == nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("merge: git provider not configured"), TraceCtx: ctx}
			}
			ctx := op.TraceCtx
			gitCtx, gitCancel := git.GitTimeoutCtx(ctx)
			defer gitCancel()
			review, err := a.deps.Reviews.GetReview(gitCtx, op.Pkg)
			if err != nil {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("merge: failed to get review for %s: %w", op.Pkg, err), TraceCtx: ctx}
			}
			if review.Status != domain.ReviewApproved {
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("merge: review status is %s, not approved", review.Status), TraceCtx: ctx}
			}
			if err := a.deps.Git.Merge(gitCtx, op.Pkg, review.UpstreamCommit); err != nil {
				if errors.Is(err, context.Canceled) {
					return msgs.OperationCancelledMsg{OpID: op.ID, TraceCtx: ctx}
				}
				_ = aurlog.Log(gitCtx, a.deps.Config.ActivityLogPath, "merge", string(op.Pkg), "status", "failed", "error", err.Error())
				return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("merge: %w", err), TraceCtx: ctx}
			}
			_ = aurlog.Log(gitCtx, a.deps.Config.ActivityLogPath, "merge", string(op.Pkg))
			return msgs.OperationCompletedMsg{OpID: op.ID, Result: "Merge completed", Review: review, TraceCtx: ctx}
		}
		return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("unknown operation kind: %d", int(op.Kind)), TraceCtx: ctx}
	}
}

// lastSuccess returns the package's last successful build — the only
// record Confirm and Publish act on (domain.md § Build → Latest Attempt
// and Last Success). A missing store, a missing record, or a record that
// is not a success is an error.
func (a App) lastSuccess(pkg domain.PackageName) (*domain.Build, error) {
	if a.deps.BuildDB == nil {
		return nil, errors.New("build store not configured")
	}
	ls, err := a.deps.BuildDB.GetLastSuccess(pkg)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("get last success: %w", err)
	}
	if ls == nil || ls.Status != domain.BuildSuccess {
		return nil, errors.New("no successful build")
	}
	return ls, nil
}

// requireBuildOfHead is the commit guard for Confirm and Publish
// (workflow.md § State Transitions and Action Availability, note 9): the
// last success must be the build of local master HEAD. If HEAD cannot be
// determined — no git provider, a lookup error, or an empty commit — the
// guard refuses rather than skipping the check.
func (a App) requireBuildOfHead(ctx context.Context, pkg domain.PackageName, ls *domain.Build) error {
	if a.deps.Git == nil {
		return errors.New("cannot verify the build's commit: git not configured")
	}
	head, err := a.deps.Git.LocalHeadCommit(ctx, pkg)
	if err != nil {
		return fmt.Errorf("cannot verify the build's commit: %w", err)
	}
	if head == "" {
		return errors.New("cannot verify the build's commit: HEAD unknown")
	}
	if ls.ReviewedCommit != head {
		return errors.New("build is for an older commit; build the merged version first")
	}
	return nil
}

// gateFailed records a refused or failed Confirm/Publish in the activity
// log and returns the operation's failure message. Nothing is written to
// any build record on this path.
func (a App) gateFailed(ctx context.Context, op Operation, verb string, err error) tea.Msg {
	_ = aurlog.Log(ctx, a.deps.Config.ActivityLogPath, verb, string(op.Pkg), "status", "failed", "error", err.Error())
	return msgs.OperationFailedMsg{OpID: op.ID, Kind: op.Kind.String(), Err: fmt.Errorf("%s: %w", verb, err), TraceCtx: ctx}
}

// drainQueueCmd checks whether the queue has pending work and, if so,
// dequeues it (mutating the returned App value) and returns a Cmd that
// executes the dequeued operation. Dequeue happens synchronously here,
// inside Update's value-receiver flow, specifically so the mutation is
// part of the App value that gets returned and persisted — a Cmd
// closure must never be the thing that mutates queue state, since Cmd
// closures run later, asynchronously, over their own captured copies.
func (a App) drainQueueCmd() (App, tea.Cmd) {
	if a.ops.IsIdle() {
		return a, nil
	}
	op, ok := a.ops.Dequeue()
	if !ok {
		return a, nil
	}
	a.ops.MarkStarted(op)
	return a, a.executeOperation(op)
}

// shortSHA returns the first 7 characters of a commit hash, or the whole
// string if it is shorter. Same rule as screens.shortSHA in
// review_screen.go; the copies are tracked for consolidation in #407.
func shortSHA(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
