package screens

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/debug"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/overlay"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// detailLoadedMsg carries the result of the initial data load performed
// in Init. err is set only when the review or diff cannot be loaded; data
// fields are then zero and must not be used. Every other check is
// best-effort: a failure leaves its field at a "not known" value and adds
// a row to warnings, which also carries the local-commits notice.
type detailLoadedMsg struct {
	review           *domain.Review
	diff             git.GitDiff
	commitsBehind    int
	status           git.RepositoryStatus
	metadata         git.RepositoryMetadata
	build            *domain.Build
	lastSuccessBuild *domain.Build
	isMerged         bool
	headCommit       string
	warnings         []string
	err              error
}

// DetailTab identifies a tab within the Package Details screen.
type DetailTab int

const (
	TabSummary DetailTab = iota
	TabCommits
	TabBuildLog
	TabArtifactHistory
)

// keyMap holds all key bindings for the Package Details screen.
type keyMap struct {
	nextTabKey key.Binding
	prevTabKey key.Binding
	backKey    key.Binding
	helpKey    key.Binding
	enterKey   key.Binding
	fetchKey   key.Binding
	buildKey   key.Binding
	mergeKey   key.Binding
	confirmKey key.Binding
	publishKey key.Binding
	scrollKey  key.Binding
	topKey     key.Binding
	bottomKey  key.Binding
}

// newDetailKeys creates the default key bindings for Package Details.
func newDetailKeys() keyMap {
	return keyMap{
		nextTabKey: key.NewBinding(
			key.WithKeys("ctrl+pgdown", "ctrl+right"),
			key.WithHelp("^pgdn/^→", "Next Tab"),
		),
		prevTabKey: key.NewBinding(
			key.WithKeys("ctrl+pgup", "ctrl+left"),
			key.WithHelp("^pgup/^←", "Prev Tab"),
		),
		helpKey: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "Help"),
		),
		backKey: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "Back"),
		),
		enterKey: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "Open Review"),
		),
		fetchKey: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "Fetch"),
		),
		mergeKey: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "Merge"),
		),
		buildKey: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "Build"),
		),
		confirmKey: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "Confirm"),
		),
		publishKey: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "Publish"),
		),
		scrollKey: key.NewBinding(
			key.WithKeys("pgup", "pgdown"),
			key.WithHelp("pgup/pgdown", "Scroll"),
		),
		topKey: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "Top"),
		),
		bottomKey: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "Bottom"),
		),
	}
}

// buildLogLoadedMsg carries the result of reading a package's latest
// build log. Exactly one of content, notice and err describes it: the
// log text; a normal state worth saying (no build yet, waiting for
// output); or a failure, which is shown and never masked (#343).
type buildLogLoadedMsg struct {
	pkg     domain.PackageName
	content string
	notice  string
	err     error
}

// artifactLoadedMsg carries the artifact history for a package. artifacts and
// err may both be set: artifacts contains the records that could be read, and
// err describes the records that could not.
type artifactLoadedMsg struct {
	pkg       domain.PackageName
	artifacts []domain.Artifact
	err       error
}

// opFailure records a post-load operation failure that must be surfaced
// via a non-blocking banner without discarding the screen's content.
type opFailure struct {
	kind string // human-readable operation label (e.g. "Fetch", "Build")
	err  error
}

// PackageDetailModel represents the full details view for a single
// package, organized into tabs.
type detailFocus int

const (
	detailFocusContent detailFocus = iota
)

type PackageDetailModel struct {
	deps    deps.Dependencies
	pkgName domain.PackageName
	tab     DetailTab
	focus   detailFocus

	width  int
	height int

	// helpOpen shows the full action help in place of the compact bar
	// (tui-package-details.md § Action Bar Convention).
	helpOpen bool

	loading         bool
	buildLogLoading bool // true only between a completed Build and buildLogLoadedMsg
	err             error

	opFailure *opFailure

	review           *domain.Review
	diff             git.GitDiff
	commitsBehind    int
	status           git.RepositoryStatus
	metadata         git.RepositoryMetadata
	build            *domain.Build
	lastSuccessBuild *domain.Build
	isMerged         bool
	headCommit       string

	buildLogContent string
	buildLogNotice  string
	buildLogErr     error
	// followLog jumps to the bottom of the build log once the reload
	// that End started arrives (#343).
	followLog bool
	// buildRequestedAt is when this screen last requested a build. Until
	// a build record that started after it exists, the latest record is
	// the previous build, whose log must not be shown as this one's
	// (#343). Zero when no request is pending.
	buildRequestedAt time.Time
	artifactHistory  []domain.Artifact
	artifactsLoading bool
	artifactErr      error
	contentPane      widgets.ContentPane
	tabs             widgets.TabBar
	keys             keyMap

	changed bool

	// gate runs the Build confirmation flow (workflow.md § Build
	// Confirmation), shared with the Dashboard.
	gate BuildGate

	// warnings are the rows rendered under the stats row — see
	// detailLoadedMsg.
	warnings []string
}

// NewPackageDetailModel constructs a new PackageDetailModel.
func NewPackageDetailModel(deps deps.Dependencies) PackageDetailModel {
	return PackageDetailModel{
		deps:        deps,
		tab:         TabSummary,
		contentPane: widgets.NewContentPane(),
		tabs:        widgets.NewTabBar([]widgets.Tab{{Label: "Summary", Enabled: true}, {Label: "Commits", Enabled: true}, {Label: "Build Log", Enabled: true}, {Label: "Artifacts", Enabled: true}}),
		keys:        newDetailKeys(),
		gate:        NewBuildGate(deps),
	}
}

// Init returns the initialization command for the detail screen.
func (m PackageDetailModel) Init() tea.Cmd {
	return m.loadDetailCmd()
}

// loadDetailCmd loads all data needed for Package Details.
func (m PackageDetailModel) loadDetailCmd() tea.Cmd {
	ctx := context.Background()
	return func() tea.Msg {
		review, err := m.deps.Reviews.GetReview(ctx, m.pkgName)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return detailLoadedMsg{err: err}
		}

		diff, err := m.deps.Git.Diff(ctx, m.pkgName)
		if err != nil {
			return detailLoadedMsg{err: err}
		}

		// Everything below is best-effort. A failed check leaves its field
		// at a "not known" value and adds a warning row: it never fails the
		// whole screen, and a failure is never shown as a real value.
		var warnings []string
		warn := func(check, what string, err error) {
			trace.Log(ctx, "loadDetailCmd", "check", check, "pkg", m.pkgName, "error", err.Error())
			warnings = append(warnings, "Could not check "+what+": "+strings.ReplaceAll(err.Error(), "\n", " "))
		}

		commitsBehind, err := m.deps.Git.CommitsBehind(ctx, m.pkgName)
		if err != nil {
			commitsBehind = -1 // renders as "—"
			warn("CommitsBehind", "commits behind", err)
		}

		var status git.RepositoryStatus
		if s, err := m.deps.Git.Status(ctx, m.pkgName); err != nil {
			warn("Status", "working tree status", err)
		} else if s != nil {
			status = *s
		}

		var metadata git.RepositoryMetadata
		if md, err := m.deps.Git.Metadata(ctx, m.pkgName); err != nil {
			warn("Metadata", "repository metadata", err)
		} else if md != nil {
			metadata = *md
		}

		var build *domain.Build
		if m.deps.BuildEngine != nil {
			bs := m.deps.BuildEngine.BuildStore()
			if bs != nil {
				b, err := bs.Get(m.pkgName)
				if err == nil && b != nil {
					build = b
				}
			}
		}

		var lastSuccessBuild *domain.Build
		if m.deps.BuildEngine != nil {
			bs := m.deps.BuildEngine.BuildStore()
			if bs != nil {
				ls, err := bs.GetLastSuccess(m.pkgName)
				if err == nil && ls != nil {
					lastSuccessBuild = ls
				}
			}
		}

		var isMerged bool
		if review != nil && review.UpstreamCommit != "" {
			merged, err := m.deps.Git.IsMerged(ctx, m.pkgName, review.UpstreamCommit)
			if err != nil {
				warn("IsMerged", "merge state", err)
			} else {
				isMerged = merged
			}
		}

		headCommit, err := m.deps.Git.LocalHeadCommit(ctx, m.pkgName)
		if err != nil {
			headCommit = ""
			warn("LocalHeadCommit", "local HEAD", err)
		}

		if n, err := m.deps.Git.LocalOnlyCommits(ctx, m.pkgName); err != nil {
			warn("LocalOnlyCommits", "for local commits not on upstream", err)
		} else if n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d local commit(s) not on upstream", n))
		}

		return detailLoadedMsg{
			review:           review,
			diff:             *diff,
			commitsBehind:    commitsBehind,
			status:           status,
			metadata:         metadata,
			build:            build,
			lastSuccessBuild: lastSuccessBuild,
			isMerged:         isMerged,
			headCommit:       headCommit,
			warnings:         warnings,
		}
	}
}

// loadBuildLogCmd loads the content of the latest build log for the current package.
func (m PackageDetailModel) loadBuildLogCmd() tea.Cmd {
	pkg, d, requestedAt := m.pkgName, m.deps, m.buildRequestedAt
	return func() tea.Msg {
		out := buildLogLoadedMsg{pkg: pkg}
		fail := func(ctx context.Context, err error) tea.Msg {
			out.err = err
			trace.Log(ctx, "buildLog", "pkg", pkg, "error", err.Error())
			_ = errorlog.Log(ctx, d.Config.RootPath, "build_log", fmt.Sprintf("%s: %v", pkg, err))
			return out
		}
		ctx := trace.Start(context.Background(), "build_log_load")

		if d.BuildEngine == nil || d.BuildEngine.BuildStore() == nil {
			return fail(ctx, errors.New("build store not configured"))
		}
		bs := d.BuildEngine.BuildStore()

		build, err := bs.Get(pkg)
		switch {
		case errors.Is(err, domain.ErrNotFound), err == nil && build == nil:
			out.notice = "No builds yet"
			return out
		case err != nil:
			return fail(ctx, fmt.Errorf("read build record: %w", err))
		}
		if !requestedAt.IsZero() && build.StartTime.Before(requestedAt) {
			// Still the previous build's record: the requested build has
			// not started yet (it may be queued behind other operations).
			out.notice = "Waiting for the build to start…"
			return out
		}
		if build.LogLocation == "" {
			out.notice = "No log recorded for this build"
			return out
		}

		running := build.Status == domain.BuildPending || build.Status == domain.BuildRunning
		content, err := bs.ReadBuildLog(build.ID, build.LogLocation)
		// The JSON store reports a missing log file as domain.ErrNotFound.
		missing := errors.Is(err, domain.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
		switch {
		case missing && running:
			out.notice = "Waiting for output…" // makepkg has not created the log yet
			return out
		case missing:
			return fail(ctx, fmt.Errorf("build log %s is missing", build.LogLocation))
		case err != nil:
			return fail(ctx, fmt.Errorf("read build log %s: %w", build.LogLocation, err))
		case strings.TrimSpace(content) == "" && running:
			out.notice = "Waiting for output…"
			return out
		case strings.TrimSpace(content) == "":
			out.notice = "Build log is empty"
			return out
		}
		out.content = content
		return out
	}
}

// Update processes messages for the package detail screen.
func (m PackageDetailModel) Update(msg tea.Msg) (PackageDetailModel, tea.Cmd) {
	gate, gateCmd, handled := m.gate.Update(msg)
	m.gate = gate
	if m.gate.Requested() {
		// The build was just requested: show the Build Log tab now, not
		// after the build completes (#343). The log is not loaded yet —
		// the latest record is still the previous build until this one
		// starts — so End is what loads it.
		m.gate = m.gate.Reset()
		m.tab = TabBuildLog
		m.buildLogLoading = false
		m.buildLogContent = ""
		m.buildLogErr = nil
		m.buildLogNotice = "Build queued — press End to load its output"
		m.buildRequestedAt = time.Now()
		m.syncLayout()
		m.refreshContent()
	}
	if handled {
		return m, gateCmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		debug.Logf("detail", "WindowSizeMsg W=%d H=%d", msg.Width, msg.Height)
		m.gate.SetWidth(m.width)
		m.syncLayout()
		return m, nil
	case detailLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.review = msg.review
		m.diff = msg.diff
		m.commitsBehind = msg.commitsBehind
		m.status = msg.status
		m.metadata = msg.metadata
		m.build = msg.build
		m.lastSuccessBuild = msg.lastSuccessBuild
		m.isMerged = msg.isMerged
		m.headCommit = msg.headCommit
		m.warnings = msg.warnings
		m.refreshContent()
		m.syncLayout()
		return m, nil
	case msgs.OperationCompletedMsg:
		if m.pkgName == "" {
			return m, nil
		}
		if msg.OpID != string(m.pkgName) {
			return m, nil
		}
		m.opFailure = nil
		m.buildRequestedAt = time.Time{}
		if msg.Build != nil {
			m.tab = TabBuildLog
			m.buildLogLoading = true
			m.syncLayout()
			m.refreshContent()
			return m, tea.Batch(m.Init(), m.loadBuildLogCmd())
		}
		m.syncLayout()
		return m, m.Init()
	case msgs.OperationCancelledMsg:
		// A cancelled build is not recorded (#304): the previous record
		// stays, so stop waiting for a build that will never start.
		if m.pkgName != "" && msg.OpID == string(m.pkgName) && !m.buildRequestedAt.IsZero() {
			m.buildRequestedAt = time.Time{}
			m.buildLogContent = ""
			m.buildLogErr = nil
			m.buildLogNotice = "Build cancelled"
			m.refreshContent()
		}
		return m, nil
	case msgs.OperationFailedMsg:
		if m.pkgName == "" {
			return m, nil
		}
		if msg.OpID != string(m.pkgName) {
			return m, nil
		}
		m.opFailure = &opFailure{
			kind: msg.Kind,
			err:  msg.Err,
		}
		// A build that failed before recording itself would otherwise
		// leave the tab waiting for it forever.
		m.buildRequestedAt = time.Time{}
		m.syncLayout()
		return m, nil
	case PackageSelectedMsg:
		m.gate = m.gate.Reset()
		m.pkgName = msg.Pkg
		m.tab = TabSummary
		m.loading = true
		m.err = nil
		m.opFailure = nil
		m.review = nil
		m.changed = false
		m.buildLogContent = ""
		m.buildLogNotice = ""
		m.buildLogErr = nil
		m.followLog = false
		m.buildRequestedAt = time.Time{}
		m.artifactHistory = nil
		m.artifactsLoading = false
		m.artifactErr = nil
		m.refreshContent()
		return m, m.loadDetailCmd()
	case buildLogLoadedMsg:
		if msg.pkg != m.pkgName {
			return m, nil // a load for a package this screen no longer shows
		}
		m.buildLogLoading = false
		m.buildLogContent = msg.content
		m.buildLogNotice = msg.notice
		m.buildLogErr = msg.err
		m.refreshContent()
		if m.followLog {
			m.followLog = false
			m.contentPane.GotoBottom()
		}
		return m, nil
	case artifactLoadedMsg:
		if msg.pkg != m.pkgName {
			return m, nil
		}
		m.artifactsLoading = false
		m.artifactHistory = msg.artifacts
		m.artifactErr = msg.err
		m.refreshContent()
		return m, nil
	}

	if m.loading {
		return m, nil
	}

	if m.err != nil {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if key.Matches(keyMsg, m.keys.helpKey) || (m.helpOpen && key.Matches(keyMsg, m.keys.backKey)) {
		m.helpOpen = !m.helpOpen
		m.syncLayout()
		return m, nil
	}

	if key.Matches(keyMsg, m.keys.backKey) {
		changed := m.changed
		return m, func() tea.Msg { return msgs.BackMsg{Changed: changed} }
	}

	if key.Matches(keyMsg, m.keys.enterKey) {
		return m, func() tea.Msg {
			return msgs.NavigateMsg{To: msgs.ScreenReview, Package: m.pkgName}
		}
	}

	if key.Matches(keyMsg, m.keys.fetchKey) && m.deps.Git != nil {
		return m, func() tea.Msg {
			ctx := trace.Start(context.Background(), "fetch")
			trace.Log(ctx, "key", "action", "fired", "op", "fetch", "pkg", m.pkgName)
			return msgs.FetchRequestedMsg{Pkg: m.pkgName, TraceCtx: ctx}
		}
	}

	if key.Matches(keyMsg, m.keys.mergeKey) && m.isMergeEnabled() {
		return m, func() tea.Msg {
			ctx := trace.Start(context.Background(), "merge")
			trace.Log(ctx, "key", "action", "fired", "op", "merge", "pkg", m.pkgName)
			return msgs.MergeRequestedMsg{Pkg: m.pkgName, TraceCtx: ctx}
		}
	}

	if key.Matches(keyMsg, m.keys.confirmKey) && m.isConfirmEnabled() {
		pkg := m.pkgName
		return m, func() tea.Msg {
			ctx := trace.Start(context.Background(), "confirm")
			trace.Log(ctx, "key", "action", "fired", "op", "confirm", "pkg", pkg)
			return msgs.ConfirmRequestedMsg{Pkg: pkg, TraceCtx: ctx}
		}
	}

	if key.Matches(keyMsg, m.keys.publishKey) && m.isPublishEnabled() {
		pkg := m.pkgName
		return m, func() tea.Msg {
			ctx := trace.Start(context.Background(), "publish")
			trace.Log(ctx, "key", "action", "fired", "op", "publish", "pkg", pkg)
			return msgs.PublishRequestedMsg{Pkg: pkg, TraceCtx: ctx}
		}
	}

	if key.Matches(keyMsg, m.keys.buildKey) && m.isBuildEnabled() {
		var cmd tea.Cmd
		m.gate, cmd = m.gate.Start(m.pkgName)
		return m, cmd
	}

	// Nav menu digit keys.
	switch keyMsg.String() {
	case "1":
		return m, func() tea.Msg {
			return msgs.NavigateMsg{To: msgs.ScreenDashboard, FromMenu: true}
		}
	case "2":
		return m, func() tea.Msg {
			return msgs.NavigateMsg{To: msgs.ScreenRepositoryStatus, FromMenu: true}
		}
	case "3":
		return m, nil
	case "4":
		return m, func() tea.Msg {
			return msgs.NavigateMsg{To: msgs.ScreenHelp, FromMenu: true}
		}
	case "q":
		return m, func() tea.Msg { return msgs.QuitRequestedMsg{} }
	}

	// Tab navigation (ctrl+pgup/ctrl/left and ctrl+pgdown/ctrl/right)
	if key.Matches(keyMsg, m.keys.prevTabKey) {
		m.tab = (m.tab + 3) % 4
		m.syncLayout()
		if m.tab == TabArtifactHistory {
			m.artifactsLoading = true
			m.artifactErr = nil
		}
		m.refreshContent()
		return m, m.tabLoadCmd()
	}
	if key.Matches(keyMsg, m.keys.nextTabKey) {
		m.tab = (m.tab + 1) % 4
		m.syncLayout()
		if m.tab == TabArtifactHistory {
			m.artifactsLoading = true
			m.artifactErr = nil
		}
		m.refreshContent()
		return m, m.tabLoadCmd()
	}

	// Content-pane scrolling claims pgup/pgdown, and home/end (#344).
	if m.focus == detailFocusContent {
		switch {
		case key.Matches(keyMsg, m.keys.scrollKey):
			cmd := m.contentPane.Update(msg)
			return m, cmd
		case key.Matches(keyMsg, m.keys.topKey):
			m.contentPane.GotoTop()
			return m, nil
		case key.Matches(keyMsg, m.keys.bottomKey) && m.tab == TabBuildLog:
			// End on the Build Log tab re-reads the log, then jumps to
			// its bottom: the way to see a running build's latest output
			// (#343).
			m.contentPane.GotoBottom()
			m.followLog = true
			return m, m.loadBuildLogCmd()
		case key.Matches(keyMsg, m.keys.bottomKey):
			m.contentPane.GotoBottom()
			return m, nil
		}
	}

	return m, nil
}

// Package returns the package this screen is currently showing.
func (m PackageDetailModel) Package() domain.PackageName {
	return m.pkgName
}

// MarkChanged records that this package's state changed while the
// user was away from Package Details (e.g. a review decision was
// recorded), so the next Back navigation relays that to the Dashboard.
func (m PackageDetailModel) MarkChanged() PackageDetailModel {
	m.changed = true
	return m
}

// refresh clears stale post-load failure state when the Package Details
// screen is re-entered via Back navigation (no new instance is
// constructed). This prevents the op-failure banner from persisting
// across a Review round-trip.
func (m PackageDetailModel) Refresh() PackageDetailModel {
	m.opFailure = nil
	m.syncLayout()
	return m
}

// fixedPartsAboveContent returns the fixed chrome rows rendered above the
// tab content pane, in order.
func (m PackageDetailModel) fixedPartsAboveContent() []string {
	parts := []string{
		m.renderNavMenu(),
		"",
	}
	if m.opFailure != nil {
		parts = append(parts, theme.ErrorStyle().Render("⚠ "+m.opFailure.kind+" failed: "+m.opFailure.err.Error()))
	}
	parts = append(parts,
		m.renderHeader(),
		m.renderStatsRow(),
	)
	parts = append(parts, m.renderWarnings()...)
	parts = append(parts, m.renderTabBar())
	return parts
}

// renderWarnings returns one row per warning from the last load (failed
// best-effort checks and the local-commits notice). Rows are single-line
// and truncated to the pane width, so the measured layout matches what is
// drawn.
func (m PackageDetailModel) renderWarnings() []string {
	if len(m.warnings) == 0 {
		return nil
	}
	style := theme.SubtleStyle().Foreground(lipgloss.Color("220")).MaxWidth(max(m.width-8, 20))
	rows := make([]string, len(m.warnings))
	for i, w := range m.warnings {
		rows[i] = style.Render("⚠ " + w)
	}
	return rows
}

// fixedPartsBelowContent returns the fixed chrome rows rendered below the
// tab content pane, in order.
func (m PackageDetailModel) fixedPartsBelowContent() []string {
	return []string{m.renderActionBar()}
}

func (m *PackageDetailModel) syncLayout() {
	paneWidth := max(m.width-8, 0)
	m.contentPane.SetSize(paneWidth, m.contentHeight())
	m.tabs.SetWidth(paneWidth)
}

// contentHeight is the rows available to the tab content pane: total height
// minus the screen frame and the measured fixed parts.
func (m PackageDetailModel) contentHeight() int {
	parts := append(m.fixedPartsAboveContent(), m.fixedPartsBelowContent()...)
	return widgets.FlexHeight(m.height, parts...)
}

// View renders the package detail screen.
func (m PackageDetailModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.pkgName == "" {
		return theme.DocStyle().Render("Package Details (no package selected)\n\nEsc Back")
	}

	if m.loading {
		return theme.DocStyle().Render("Loading " + string(m.pkgName) + "…\n\nEsc Back")
	}

	if m.err != nil {
		return theme.DocStyle().Render(theme.ErrorStyle().Render(m.err.Error()) + "\n\nEsc Back")
	}

	parts := append(m.fixedPartsAboveContent(), m.renderTabContent())
	parts = append(parts, m.fixedPartsBelowContent()...)

	body := widgets.WrapScreenBody(m.width, strings.Join(parts, "\n"))
	if m.gate.ModalOpen() {
		return overlay.Render(body, m.gate.View(), m.width, m.height)
	}
	return body
}

func (m PackageDetailModel) renderNavMenu() string {
	return widgets.RenderNavMenu(widgets.DefaultNavItems())
}

// lifecycleState derives the package's lifecycle state from the loaded
// data. It is the single place Package Details derives it: the header and
// Confirm/Publish availability (#340) all read it.
func (m PackageDetailModel) lifecycleState() lifecycle.State {
	var reviewStatus domain.ReviewStatus
	if m.review != nil {
		reviewStatus = m.review.Status
	}
	return lifecycle.DeriveState(lifecycle.Inputs{
		ReviewStatus: reviewStatus,
		HasReview:    m.review != nil,
		IsMerged:     m.isMerged,
		HeadCommit:   m.headCommit,
		LastSuccess:  m.lastSuccessBuild,
	})
}

// isConfirmEnabled: Confirm (T11) is available in Built only
// (workflow.md § Dashboard / Package Details).
func (m PackageDetailModel) isConfirmEnabled() bool {
	return m.lifecycleState() == lifecycle.Built
}

// isPublishEnabled: Publish (T12) is available in Confirmed only.
func (m PackageDetailModel) isPublishEnabled() bool {
	return m.lifecycleState() == lifecycle.Confirmed
}

func (m PackageDetailModel) renderHeader() string {
	lifecycleState := m.lifecycleState()
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		theme.SubtleStyle().Render("‹ Dashboard"),
		"  ",
		theme.HeaderStyle().Render(string(m.pkgName)),
		"  ",
		theme.SubtleStyle().Render("Lifecycle: "),
		theme.HeaderStyle().Render(string(lifecycleState)),
	)
}

func (m PackageDetailModel) renderStatsRow() string {
	commitsBehind := "—"
	if m.commitsBehind >= 0 {
		if m.commitsBehind == 0 {
			commitsBehind = "0"
		} else {
			commitsBehind = fmt.Sprintf("%d", m.commitsBehind)
		}
	}

	var statusStr string
	switch {
	case m.status.Clean:
		statusStr = "Clean"
	case m.status.Detached:
		statusStr = "Detached HEAD"
	case m.status.HasUntracked:
		statusStr = "Has untracked files"
	case m.status.HasModified:
		statusStr = "Uncommitted changes"
	default:
		statusStr = "—"
	}

	branch := "—"
	if m.metadata.CurrentBranch != "" {
		branch = m.metadata.CurrentBranch
	}

	url := "—"
	if m.metadata.OriginURL != "" {
		url = m.metadata.OriginURL
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		theme.SubtleStyle().Render("Commits Behind: "+commitsBehind),
		"  ",
		theme.SubtleStyle().Render("Status: "+statusStr),
		"  ",
		theme.SubtleStyle().Render("Branch: "+branch),
		"  ",
		theme.SubtleStyle().Render("Origin: "+url),
	)
}

func (m PackageDetailModel) renderTabBar() string {
	m.tabs.SetActive(int(m.tab))

	return m.tabs.View("")
}

func (m PackageDetailModel) renderTabContent() string {
	return m.contentPane.View()
}

func (m PackageDetailModel) renderSummaryTab() string {
	var b strings.Builder

	// Review Summary
	b.WriteString("\n" + theme.HeaderStyle().Render("Review Summary") + "\n")
	if m.review == nil {
		b.WriteString(theme.SubtleStyle().Render("No review") + "\n")
	} else {
		b.WriteString(theme.SubtleStyle().Render(m.renderFindingsSummary()) + "\n")
		statusStr := strings.ToUpper(string(m.review.Status)[:1]) + string(m.review.Status)[1:]
		b.WriteString(theme.SubtleStyle().Render(fmt.Sprintf("Status: %s", statusStr)) + "\n")
		decisionStr := "—"
		if !m.review.DecisionTime.IsZero() {
			decisionStr = m.review.DecisionTime.Format("2006-01-02 15:04:05")
		}
		b.WriteString(theme.SubtleStyle().Render(fmt.Sprintf("Decided on: %s", decisionStr)) + "\n")
	}

	// Latest Build
	b.WriteString("\n" + theme.HeaderStyle().Render("Latest Build") + "\n")
	if m.build == nil {
		b.WriteString(theme.SubtleStyle().Render("No builds recorded") + "\n")
	} else {
		statusStr := string(m.build.Status)
		startTime := "—"
		if !m.build.StartTime.IsZero() {
			startTime = m.build.StartTime.Format("2006-01-02 15:04:05")
		}
		b.WriteString(theme.SubtleStyle().Render(fmt.Sprintf("Status: %s  Start: %s", statusStr, startTime)) + "\n")
		if m.build.EndTime != nil && !m.build.EndTime.IsZero() {
			duration := m.build.EndTime.Sub(m.build.StartTime)
			b.WriteString(theme.SubtleStyle().Render(fmt.Sprintf("Duration: %s", duration.Round(time.Second))) + "\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m PackageDetailModel) renderFindingsSummary() string {
	return findingsSummary(m.deps.Policy, m.review)
}

func (m PackageDetailModel) isMergeEnabled() bool {
	return isMergeAvailable(m.lifecycleState())
}

func (m PackageDetailModel) isBuildEnabled() bool {
	return isBuildAvailable(m.lifecycleState())
}

func (m PackageDetailModel) loadArtifactHistoryCmd() tea.Cmd {
	pkg, d := m.pkgName, m.deps
	return func() tea.Msg {
		out := artifactLoadedMsg{pkg: pkg}
		fail := func(ctx context.Context, err error) tea.Msg {
			out.err = err
			trace.Log(ctx, "artifacts_load", "pkg", pkg, "error", err.Error())
			_ = errorlog.Log(ctx, d.Config.RootPath, "artifacts", fmt.Sprintf("%s: %v", pkg, err))
			return out
		}
		ctx := trace.Start(context.Background(), "artifacts_load")

		if d.BuildEngine == nil {
			return fail(ctx, errors.New("artifact store not configured"))
		}
		artStore := d.BuildEngine.ArtifactStore()
		if artStore == nil {
			return fail(ctx, errors.New("artifact store not configured"))
		}

		arts, err := artStore.GetArtifactsByPackage(pkg)

		if arts == nil {
			arts = []domain.Artifact{}
		}

		if err != nil {
			trace.Log(ctx, "artifacts_load", "pkg", pkg, "error", err.Error())
			_ = errorlog.Log(ctx, d.Config.RootPath, "artifacts", fmt.Sprintf("%s: %v", pkg, err))
		}

		return artifactLoadedMsg{pkg: pkg, artifacts: arts, err: err}
	}
}

func formatSize(size int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	if size <= 0 {
		return "0 B"
	}
	log := math.Log(float64(size)) / math.Log(1024)
	val := int64(log)
	if val >= int64(len(units)) {
		val = int64(len(units)) - 1
	}
	value := float64(size) / math.Pow(1024, float64(val))
	return fmt.Sprintf("%.1f %s", value, units[val])
}

func renderCommitsContent(commits []git.Commit) string {
	if len(commits) == 0 {
		return theme.SubtleStyle().Render("No commits in diff")
	}

	var b strings.Builder
	for _, c := range commits {
		hash := c.Hash
		if len(hash) > 8 {
			hash = hash[:8]
		}
		line := fmt.Sprintf("  %s  %s  %s  %s",
			theme.DiffHeaderStyle().Render(hash),
			theme.SubtleStyle().Render(c.Author),
			theme.DiffContextStyle().Render(c.Subject),
			theme.SubtleStyle().Render(c.Date),
		)
		b.WriteString(line + "\n")
	}
	return b.String()
}

// renderBuildLogView renders the Build Log tab: an error, a notice, or
// the log itself (#343).
func renderBuildLogView(content, notice string, err error) string {
	switch {
	case err != nil:
		return theme.ErrorStyle().Render("Could not load the build log: " + err.Error())
	case notice != "":
		return theme.SubtleStyle().Render(notice)
	}
	return renderBuildLogContent(content)
}

func renderBuildLogContent(buildLogContent string) string {
	if buildLogContent == "" {
		return theme.SubtleStyle().Render("No build log available")
	}

	var b strings.Builder
	b.WriteString(theme.HeaderStyle().Render("Build Log") + "\n")
	b.WriteString("\n")
	b.WriteString(sanitize.ForLogDisplay(buildLogContent))
	return b.String()
}

// refreshContent sets the content pane from the current tab's data.
// It is the only place tab content is assigned, so a data change can
// never leave the pane stale.
func (m *PackageDetailModel) refreshContent() {
	switch m.tab {
	case TabCommits:
		m.contentPane.SetContent(renderCommitsContent(m.diff.Commits))
	case TabBuildLog:
		if m.buildLogLoading {
			m.contentPane.SetContent(theme.SubtleStyle().Render("Loading build log…"))
			return
		}
		m.contentPane.SetContent(renderBuildLogView(m.buildLogContent, m.buildLogNotice, m.buildLogErr))
	case TabArtifactHistory:
		m.contentPane.SetContent(renderArtifactsView(m.artifactsLoading, m.artifactHistory, m.artifactErr))
	default:
		m.contentPane.SetContent(m.renderSummaryTab())
	}
}

// tabLoadCmd returns the async load a tab needs on entry, or nil.
func (m PackageDetailModel) tabLoadCmd() tea.Cmd {
	switch m.tab {
	case TabBuildLog:
		return m.loadBuildLogCmd()
	case TabArtifactHistory:
		return m.loadArtifactHistoryCmd()
	}
	return nil
}

// renderArtifactsView renders the Artifacts tab: a loading indicator, a
// whole-store error, partial-record errors with per-record lines, or the
// normal artifact content.
func renderArtifactsView(loading bool, artifacts []domain.Artifact, err error) string {
	if loading {
		return theme.SubtleStyle().Render("Loading artifacts…")
	}
	if err != nil {
		// Distinguish whole-store failure from partial-record failure.
		if _, ok := err.(interface{ Unwrap() []error }); ok {
			// Partial: some records unreadable (errors.Join always returns
			// this interface, even for a single record).
			unwrapped := err.(interface{ Unwrap() []error }).Unwrap()
			var b strings.Builder
			if len(unwrapped) == 1 {
				b.WriteString(theme.ErrorStyle().Render("⚠ 1 artifact record could not be read (it may belong to any package):"))
				b.WriteString("\n")
			} else {
				b.WriteString(theme.ErrorStyle().Render(fmt.Sprintf("⚠ %d artifact records could not be read (they may belong to any package):", len(unwrapped))))
				b.WriteString("\n")
			}
			for _, e := range unwrapped {
				b.WriteString(theme.ErrorStyle().Render("  " + strings.ReplaceAll(e.Error(), "\n", " ")))
				b.WriteString("\n")
			}
			b.WriteString("\n")
			if len(artifacts) == 0 {
				b.WriteString(theme.SubtleStyle().Render("No artifacts recorded for this package"))
			} else {
				b.WriteString(renderArtifactHistoryContent(artifacts))
			}
			return b.String()
		}
		// Whole-store failure: render only the error.
		msg := err.Error()
		msg = strings.ReplaceAll(msg, "\n", "; ")
		return theme.ErrorStyle().Render("Could not load artifacts: " + msg)
	}
	if len(artifacts) == 0 {
		return theme.SubtleStyle().Render("No artifacts recorded for this package")
	}
	return renderArtifactHistoryContent(artifacts)
}

func renderArtifactHistoryContent(artifacts []domain.Artifact) string {
	if len(artifacts) == 0 {
		return theme.SubtleStyle().Render("No artifacts recorded for this package")
	}

	var b strings.Builder
	b.WriteString(theme.HeaderStyle().Render("Artifact History") + "\n")
	b.WriteString("\n")

	for i, art := range artifacts {
		fmt.Fprintf(&b, "  %d. %s (%s) %s\n", i+1, art.Filename, art.Architecture, art.Version)
		if art.Checksum != "" {
			fmt.Fprintf(&b, "     Checksum: %s\n", art.Checksum)
		}
		if art.Size > 0 {
			fmt.Fprintf(&b, "     Size: %s\n", formatSize(art.Size))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// actions lists Package Details' actions in their fixed bar order, with
// availability (tui-package-details.md § Action Bar Convention).
func (m PackageDetailModel) actions() []widgets.Action {
	gitReason := ""
	if m.deps.Git == nil {
		gitReason = reasonNoGit
	}

	mergeWhy := ""
	if !m.isMergeEnabled() {
		mergeWhy = mergeReason(m.lifecycleState())
	}

	return []widgets.Action{
		{Key: "ENTER", Label: "Open Review", Enabled: m.review != nil, Reason: reasonNoReview},
		{Key: "^PgDn/^PgUp", Label: "Tabs", Enabled: true},
		{Key: "F", Label: "Fetch", Enabled: m.deps.Git != nil, Reason: gitReason},
		{Key: "M", Label: "Merge", Enabled: m.isMergeEnabled(), Reason: mergeWhy},
		{Key: "B", Label: "Build", Enabled: m.isBuildEnabled(), Reason: prereqMergedOrLater},
		{Key: "C", Label: "Confirm", Enabled: m.isConfirmEnabled(), Reason: prereqBuilt},
		{Key: "P", Label: "Publish", Enabled: m.isPublishEnabled(), Reason: prereqConfirmed},
		{Key: "Esc", Label: "Back", Enabled: true},
	}
}

// renderActionBar renders the compact bar, or the full help while it is
// open. Its height is measured by contentHeight via fixedPartsBelowContent.
func (m PackageDetailModel) renderActionBar() string {
	if m.helpOpen {
		return "\n" + widgets.RenderActionHelp(m.actions(), max(m.width-8, 40))
	}
	return "\n" + widgets.RenderActionBar(m.actions())
}
