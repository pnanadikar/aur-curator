package screens

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/debug"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/overlay"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// ReviewScreen is the Bubble Tea model for the package review workflow:
// diff/commit comparison, analyzer findings, and the Approve/Reject
// decision flow (tui-review.md).
type ReviewScreen struct {
	pkgName domain.PackageName
	git     git.Provider
	store   domain.ReviewStore

	engine *review.Engine
	config domain.WorkspaceConfig
	width  int
	height int

	loading   bool
	err       error
	opFailure *opFailure
	changed   bool

	diff         git.GitDiff
	review       *domain.Review
	tabs         widgets.TabBar
	diffPane     widgets.DiffPane
	findingsPane widgets.ContentPane

	modal     *widgets.ConfirmModal
	modalFlow modalFlow
	traceCtx  context.Context // correlation context for approve/reject path

	approveKey key.Binding
	rejectKey  key.Binding
	nextTabKey key.Binding
	prevTabKey key.Binding
	backKey    key.Binding
	analyzeKey key.Binding
	// scrollKey scrolls the active tab's content pane.
	scrollKey key.Binding
	topKey    key.Binding
	bottomKey key.Binding
	helpKey   key.Binding

	// helpOpen shows the full action help in place of the compact bar
	// (tui-package-details.md § Action Bar Convention).
	helpOpen bool
}

// NewReviewScreen constructs the Review screen: diff rendering, the
// Findings tab (Analyzer Manager + Policy Engine), and the Approve/
// Reject decision flow.
func NewReviewScreen(pkgName domain.PackageName, gitProvider git.Provider, store domain.ReviewStore, engine *review.Engine, cfg domain.WorkspaceConfig) ReviewScreen {
	return ReviewScreen{
		pkgName: pkgName,
		git:     gitProvider,
		store:   store,
		engine:  engine,
		config:  cfg,
		loading: true,
		tabs: widgets.NewTabBar([]widgets.Tab{
			{Label: tabDiff, Enabled: true},
			{Label: tabSRCINFO, Enabled: false},
			{Label: tabInstall, Enabled: false},
			{Label: tabPatches, Enabled: false},
			{Label: "Findings", Enabled: true},
		}),
		diffPane:     widgets.NewDiffPane(),
		findingsPane: widgets.NewContentPane(),
		approveKey: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "Approve"),
		),
		rejectKey: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("^X", "Reject"),
		),
		nextTabKey: key.NewBinding(
			key.WithKeys("ctrl+pgdown", "ctrl+right"),
			key.WithHelp("^pgdn/^→", "Next Tab"),
		),
		prevTabKey: key.NewBinding(
			key.WithKeys("ctrl+pgup", "ctrl+left"),
			key.WithHelp("^pgup/^←", "Prev Tab"),
		),
		backKey: key.NewBinding(
			key.WithKeys("esc", "ctrl+c"),
			key.WithHelp("esc", "Back"),
		),
		analyzeKey: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "Run Analyzers"),
		),
		scrollKey: key.NewBinding(
			key.WithKeys("pgup", "pgdown"),
			key.WithHelp("pgup/pgdown", "Scroll diff"),
		),
		topKey: key.NewBinding(
			key.WithKeys("home"),
			key.WithHelp("home", "Top"),
		),
		bottomKey: key.NewBinding(
			key.WithKeys("end"),
			key.WithHelp("end", "Bottom"),
		),
		helpKey: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "Help"),
		),
	}
}

// Init loads Git data and any stored review before the screen renders.
func (m ReviewScreen) Init() tea.Cmd {
	return m.loadReviewCmd()
}

// Update processes Bubble Tea messages for the review screen.
func (m ReviewScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		debug.Logf("review", "WindowSizeMsg W=%d H=%d", msg.Width, msg.Height)
		ph := m.syncLayout()
		debug.Logf("review", "  after syncLayout: paneH=%d", ph)
		if m.modal != nil {
			m.modal.SetWidth(max(m.width-8, 48))
		}
		return m, nil
	case reviewLoadedMsg:
		m.applyLoadedReview(msg)
		return m, nil
	case reviewSavedMsg:
		if msg.review != nil {
			trace.Log(msg.traceCtx, "handoff", "to", "reviewSavedMsg", "status", msg.review.Status, "pkg", m.pkgName)
		}
		if msg.err != nil {
			m.err = msg.err
			trace.Log(msg.traceCtx, "handoff", "status", "error", "pkg", m.pkgName)
			return m, tea.Sequence(
				func() tea.Msg { return msgs.ToastMsg{Text: msg.err.Error()} },
				func() tea.Msg { return msgs.ReviewDecisionBackMsg{Changed: m.changed} },
			)
		}
		trace.Log(msg.traceCtx, "handoff", "status", "persisted", "pkg", m.pkgName)
		m.review = msg.review
		m.changed = true
		var toastText string
		switch msg.review.Status {
		case domain.ReviewApproved:
			toastText = "Package change approved"
		case domain.ReviewRejected:
			toastText = "Package change rejected"
		}
		var cmds []tea.Cmd
		cmds = append(cmds,
			func() tea.Msg { return msgs.ToastMsg{Text: toastText} },
			func() tea.Msg {
				return msgs.ReviewDecisionBackMsg{Changed: m.changed, Review: msg.review, TraceCtx: msg.traceCtx}
			},
		)
		if msg.review.Status == domain.ReviewApproved && m.config.AutoMerge {
			ctx := trace.Start(context.Background(), "merge")
			trace.Log(ctx, "key", "action", "auto", "op", "merge", "pkg", m.pkgName)
			cmds = append(cmds, func() tea.Msg {
				return msgs.MergeRequestedMsg{Pkg: m.pkgName, TraceCtx: ctx}
			})
		}
		return m, tea.Sequence(cmds...)
	case msgs.OperationCompletedMsg:
		if msg.OpID == string(m.pkgName) {
			m.loading = false
			m.opFailure = nil
			if review, ok := msg.Result.(*domain.Review); ok && review != nil {
				m.review = review
				m.changed = true
				m.findingsPane.SetContent(m.renderFindingsContent())
				if review.FindingsStatus == domain.FindingsRun &&
					m.tabs.Active().Label != "Findings" {
					m.tabs.SetActive(4)
				}
			}
			m.syncLayout()
		}
		return m, nil
	case msgs.OperationFailedMsg:
		if msg.OpID == string(m.pkgName) {
			m.loading = false
			m.opFailure = &opFailure{
				kind: msg.Kind,
				err:  msg.Err,
			}
			m.syncLayout()
		}
		return m, nil
	}

	if m.modal != nil {
		result, cmd := m.modal.Update(msg)
		switch {
		case result.Confirmed:
			return m, m.handleModalConfirmed(result.Value)
		case result.Cancelled:
			m.closeModal()
			return m, cmd
		case result.Submitted:
			return m, m.handleModalSubmitted(result.Value)
		case result.Skipped:
			return m, m.handleModalSkipped()
		default:
			return m, cmd
		}
	}

	if m.loading {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if ok {
		if key.Matches(keyMsg, m.helpKey) || (m.helpOpen && keyMsg.String() == "esc") {
			m.helpOpen = !m.helpOpen
			m.syncLayout()
			return m, nil
		}

		if key.Matches(keyMsg, m.backKey) {
			return m, func() tea.Msg { return msgs.BackMsg{Changed: m.changed} }
		}

		if key.Matches(keyMsg, m.nextTabKey) {
			m.tabs.Next()
			m.showActiveTabFiles()
			m.syncLayout()
			return m, nil
		}

		if key.Matches(keyMsg, m.prevTabKey) {
			m.tabs.Prev()
			m.showActiveTabFiles()
			m.syncLayout()
			return m, nil
		}

		if key.Matches(keyMsg, m.approveKey) && m.review.Status == domain.ReviewPending {
			ctx := trace.Start(context.Background(), "approve")
			trace.Log(ctx, "key", "action", "fired", "op", "approve", "pkg", m.pkgName)
			m.traceCtx = ctx
			m.openApproveModal()
			return m, m.modal.Focus()
		}

		if key.Matches(keyMsg, m.rejectKey) && m.review.Status == domain.ReviewPending {
			ctx := trace.Start(context.Background(), "reject")
			trace.Log(ctx, "key", "action", "fired", "op", "reject", "pkg", m.pkgName)
			m.traceCtx = ctx
			m.openRejectConfirmModal()
			return m, nil
		}

		if key.Matches(keyMsg, m.analyzeKey) && m.review.Status == domain.ReviewPending {
			if m.loading {
				return m, nil
			}

			m.loading = true
			return m, m.runAnalyzersCmd()
		}
		// Only pgup/pgdown scroll the content pane; with no text field on
		// this screen there is nothing else for a key to collide with.
		if key.Matches(keyMsg, m.scrollKey) {
			switch m.tabs.Active().Label {
			case "Findings":
				return m, m.findingsPane.Update(msg)
			default:
				return m, m.diffPane.Update(msg)
			}
		}
		// Home/End jump to the top/bottom of the active tab's pane (#344).
		if key.Matches(keyMsg, m.topKey) || key.Matches(keyMsg, m.bottomKey) {
			top := key.Matches(keyMsg, m.topKey)
			switch {
			case m.tabs.Active().Label == "Findings" && top:
				m.findingsPane.GotoTop()
			case m.tabs.Active().Label == "Findings":
				m.findingsPane.GotoBottom()
			case top:
				m.diffPane.GotoTop()
			default:
				m.diffPane.GotoBottom()
			}
			return m, nil
		}
	}

	return m, nil
}

// Package returns the package this screen is currently reviewing.
func (m ReviewScreen) Package() domain.PackageName {
	return m.pkgName
}

// Refresh clears stale post-load failure state when the Review screen is
// re-entered for the same package (no new instance is constructed). This
// prevents the op-failure banner from persisting across a back-and-forth
// round trip between Package Details and Review for the same package.
func (m ReviewScreen) Refresh() ReviewScreen {
	m.opFailure = nil
	m.syncLayout()
	return m
}

// View renders the review screen.
func (m ReviewScreen) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.err != nil {
		return theme.DocStyle().Render(theme.ErrorStyle().Render(m.err.Error()) + "\n\nEsc Back")
	}

	sections := append(m.fixedPartsAboveContent(), m.renderTabContent())
	sections = append(sections, m.fixedPartsBelowContent()...)

	body := widgets.WrapScreenBody(m.width, strings.Join(sections, "\n"))
	if m.modal != nil {
		modalView := m.modal.View()
		baseView := body
		return overlay.Render(baseView, modalView, m.width, m.height)
	}
	return body
}

// syncLayout sizes the tab content panes from the measured fixed parts and
// returns the pane height.
func (m *ReviewScreen) syncLayout() int {
	paneW := max(m.width-8, 0)
	paneH := widgets.FlexHeight(m.height,
		append(m.fixedPartsAboveContent(), m.fixedPartsBelowContent()...)...)
	m.tabs.SetWidth(paneW)
	m.diffPane.SetSize(paneW, paneH)
	m.findingsPane.SetSize(paneW, paneH)
	return paneH
}

// fixedPartsAboveContent returns the fixed chrome rows rendered above the
// tab content pane, in order.
func (m ReviewScreen) fixedPartsAboveContent() []string {
	status := domain.ReviewPending
	if m.review != nil {
		status = m.review.Status
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		theme.SubtleStyle().Render("‹ Package Details"),
		"  ",
		theme.HeaderStyle().Render(fmt.Sprintf("%s - Review", m.pkgName)),
		"  ",
		theme.SubtleStyle().Render("Status: "+statusTitle(status)),
	)

	summary := fmt.Sprintf(
		"Reviewing: %s        %s, %s changed",
		reviewRangeLabel(m.diff),
		pluralize(len(m.diff.Commits), "commit"),
		pluralize(len(m.diff.Files), "file"),
	)

	tabBar := m.tabs.View("")
	if m.engine != nil {
		tabBar = m.tabs.View(findingsSummary(m.engine.Policy(), m.review))
	}

	parts := []string{}
	if m.opFailure != nil {
		parts = append(parts, theme.ErrorStyle().Render("⚠ "+m.opFailure.kind+" failed: "+m.opFailure.err.Error()))
	}
	return append(parts, header, summary, tabBar)
}

// fixedPartsBelowContent returns the fixed chrome rows rendered below the
// tab content pane, in order.
func (m ReviewScreen) fixedPartsBelowContent() []string {
	return []string{
		m.renderActionBar(),
	}
}

// actions lists the Review screen's actions in their fixed bar order
// (tui-review.md § 5 Action Bar).
func (m ReviewScreen) actions() []widgets.Action {
	status := domain.ReviewPending
	if m.review != nil {
		status = m.review.Status
	}
	pending := status == domain.ReviewPending

	return []widgets.Action{
		{Key: "^PgDn/^PgUp", Label: "Tabs", Enabled: true},
		{Key: "a", Label: "Run Analyzers", Enabled: pending, Reason: prereqPendingReview},
		{Key: "ENTER", Label: "Approve", Enabled: pending, Reason: prereqPendingReview},
		{Key: "^X", Label: "Reject", Enabled: pending, Reason: prereqPendingReview},
		{Key: "Esc", Label: "Back", Enabled: true},
	}
}

// renderActionBar renders the compact bar, or the full help while it is
// open, preceded by the analyzer progress indicator while analyzers run.
// syncLayout measures it via fixedPartsBelowContent.
func (m ReviewScreen) renderActionBar() string {
	bar := widgets.RenderActionBar(m.actions())
	if m.helpOpen {
		bar = widgets.RenderActionHelp(m.actions(), max(m.width-8, 40))
	}
	if m.loading {
		sep := "  "
		if m.helpOpen {
			sep = "\n" // keep the help's columns aligned
		}
		bar = theme.ActionBarDimmedStyle().Render("⠋ Running analyzers…") + sep + bar
	}
	return "\n" + bar
}

func (m ReviewScreen) renderTabContent() string {
	switch m.tabs.Active().Label {
	case "Findings":
		return m.findingsPane.View()
	default:
		return m.diffPane.View()
	}
}

func (m ReviewScreen) renderFindingsContent() string {
	p := m.engine.Policy()

	if m.review == nil || m.review.FindingsStatus == domain.FindingsNotRun {
		if m.review != nil && m.review.Status != domain.ReviewPending {
			return theme.SubtleStyle().Render("No findings to display.")
		}
		return theme.SubtleStyle().Render("No findings yet. Press a to run analyzers.")
	}

	if len(m.review.Findings) == 0 {
		return theme.SubtleStyle().Render("No findings — all checks passed.")
	}

	sorted := sortedFindings(p, m.review.Findings)
	counts := countBySeverity(p, sorted)
	var parts []string
	if counts[domain.SeverityHigh] > 0 {
		parts = append(parts, fmt.Sprintf("%d HIGH", counts[domain.SeverityHigh]))
	}
	if counts[domain.SeverityMedium] > 0 {
		parts = append(parts, fmt.Sprintf("%d MEDIUM", counts[domain.SeverityMedium]))
	}
	if counts[domain.SeverityLow] > 0 {
		parts = append(parts, fmt.Sprintf("%d LOW", counts[domain.SeverityLow]))
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(
		theme.HeaderStyle().Render(fmt.Sprintf(
			"Findings: %d (%s)\n",
			len(m.review.Findings),
			strings.Join(parts, ", "),
		)),
	)
	b.WriteString("\n")
	for _, f := range sorted {
		var severityStr string
		var severityStyle lipgloss.Style
		switch p.Severity(f.Category) {
		case domain.SeverityHigh:
			severityStr = "HIGH"
			severityStyle = theme.ErrorStyle()
		case domain.SeverityMedium:
			severityStr = "MEDIUM"
			severityStyle = theme.SubtleStyle().Foreground(lipgloss.Color("208"))
		case domain.SeverityLow:
			severityStr = "LOW"
			severityStyle = theme.SubtleStyle().Foreground(lipgloss.Color("112"))
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n",
			severityStyle.Render(severityStr),
			theme.SubtleStyle().Render(f.Analyzer),
			f.Summary,
		)
		if f.Location != "" {
			fmt.Fprintf(&b, "       %s\n", theme.DimmedStyle().Render(f.Location))
		}
	}
	return b.String()
}

func (m ReviewScreen) runAnalyzersCmd() tea.Cmd {
	ctx := trace.Start(context.Background(), "analyze")
	trace.Log(ctx, "key", "action", "fired", "op", "analyze", "pkg", m.pkgName)
	return func() tea.Msg {
		return msgs.AnalyzeRequestedMsg{Pkg: m.pkgName, TraceCtx: ctx}
	}
}

// loadReviewCmd ensures a Review exists for the package and loads the
// diff of its stored range (BaseCommit → UpstreamCommit) for display —
// never the live master...upstream/master, which is empty after Merge
// (#387). It performs no network fetch and never overwrites an existing
// Review — that is the exclusive role of the explicit F Fetch action.
func (m ReviewScreen) loadReviewCmd() tea.Cmd {
	ctx := context.Background()
	return func() tea.Msg {

		review, err := m.engine.EnsureReview(ctx, m.pkgName)
		if err != nil {
			return reviewLoadedMsg{err: err}
		}

		diff, err := m.git.ReviewDiff(ctx, m.pkgName, review.BaseCommit, review.UpstreamCommit)
		if err != nil {
			return reviewLoadedMsg{err: err}
		}

		return reviewLoadedMsg{
			diff:   *diff,
			review: review,
		}
	}
}

// Labels of the tabs that show file diffs (tui-review.md § Tab Bar).
const (
	tabDiff    = "Diff"
	tabSRCINFO = ".SRCINFO"
	tabInstall = ".install"
	tabPatches = "Patches"
)

// fileTabs are the tabs enabled only when the review changes a file of
// their kind (#385). The Diff tab is always enabled.
var fileTabs = []string{tabSRCINFO, tabInstall, tabPatches}

// tabKinds maps each diff tab to the file kinds it shows: Diff takes
// PKGBUILD and anything unclassified (e.g. LICENSE, stray checksum
// files); .SRCINFO, .install and Patches each take their own kind.
var tabKinds = map[string][]git.FileKind{
	tabDiff:    {git.FileKindPKGBUILD, git.FileKindOther},
	tabSRCINFO: {git.FileKindSRCINFO},
	tabInstall: {git.FileKindInstall},
	tabPatches: {git.FileKindPatch},
}

// tabFiles returns the FileDiffs shown on the diff tab label, in the order
// git reported them. It returns nil for a label that is not a diff tab.
func tabFiles(diff git.GitDiff, label string) []git.FileDiff {
	kinds := tabKinds[label]
	var files []git.FileDiff
	for _, f := range diff.Files {
		for _, k := range kinds {
			if f.Kind == k {
				files = append(files, f)
				break
			}
		}
	}
	return files
}

// setDiff stores a loaded diff, enables each file tab exactly when the
// diff changes a file of its kind, and refills the diff pane. On a first
// review (Import) every file is an addition, so each tab is enabled when
// the package has such a file.
func (m *ReviewScreen) setDiff(diff git.GitDiff) {
	m.diff = diff
	for _, label := range fileTabs {
		m.tabs.SetEnabled(label, len(tabFiles(diff, label)) > 0)
	}
	m.showActiveTabFiles()
}

// showActiveTabFiles fills the diff pane with the active tab's files and
// scrolls it to the top. The Findings tab has its own pane: nothing to do.
func (m *ReviewScreen) showActiveTabFiles() {
	label := m.tabs.Active().Label
	if _, ok := tabKinds[label]; !ok {
		return
	}
	empty := "No changes in the reviewed range."
	if len(m.diff.Files) > 0 {
		empty = "No changes to PKGBUILD or other files here; see the other tabs."
	}
	m.diffPane.SetEmptyText(empty)
	m.diffPane.SetFiles(tabFiles(m.diff, label))
	m.diffPane.GotoTop()
}

func (m ReviewScreen) saveReviewCmd(review *domain.Review) tea.Cmd {
	ctx := m.traceCtx
	return func() tea.Msg {
		trace.Log(ctx, "store", "op", "RecordDecision", "status", review.Status, "pkg", m.pkgName)
		r, err := m.engine.RecordDecision(
			ctx,
			m.pkgName,
			review.Status,
			review.Rationale,
			review.ReviewerNotes,
		)
		if err != nil {
			return reviewSavedMsg{err: err, traceCtx: ctx}
		}

		event := "reject"
		if r.Status == domain.ReviewApproved {
			event = "approve"
		}
		_ = aurlog.Log(ctx, m.config.ActivityLogPath, event, string(m.pkgName), "comment", decisionComment(r))

		return reviewSavedMsg{review: r, traceCtx: ctx}
	}
}

// decisionComment returns the free text recorded in the activity log for a
// decision: an approval's reviewer notes, or a rejection's reason. The two
// live in different fields (Review.ReviewerNotes vs Review.Rationale), so
// logging one field for both silently dropped every rejection reason.
func decisionComment(r *domain.Review) string {
	if r.Status == domain.ReviewRejected {
		return r.Rationale
	}
	return r.ReviewerNotes
}

// reviewRangeLabel names the reviewed range shown in the comparison
// summary: "<base> → <upstream>", with "(new)" for the empty base of a
// first review (tui-review.md § 2, #387). Before the diff has loaded there
// is no range yet, so it shows "…" rather than a misleading "(new)".
func reviewRangeLabel(diff git.GitDiff) string {
	if diff.UpstreamCommit == "" {
		return "…"
	}
	base := "(new)"
	if diff.BaseCommit != "" {
		base = shortSHA(diff.BaseCommit)
	}
	return base + " → " + shortSHA(diff.UpstreamCommit)
}

func shortSHA(value string) string {
	if len(value) <= 7 {
		return value
	}
	return value[:7]
}

func statusTitle(status domain.ReviewStatus) string {
	switch status {
	case domain.ReviewApproved:
		return "Approved"
	case domain.ReviewRejected:
		return "Rejected"
	default:
		return "Pending"
	}
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// loadedReviewMsg is the shared interface for review+diff messages.
type loadedReviewMsg interface {
	GetReview() *domain.Review
	GetDiff() git.GitDiff
	GetErr() error
}

// applyLoadedReview updates the UI with a loaded review and diff.
// Shared by reviewLoadedMsg case handler.
func (m *ReviewScreen) applyLoadedReview(msg loadedReviewMsg) {
	m.loading = false
	m.changed = false
	m.err = msg.GetErr()
	if msg.GetErr() != nil {
		return
	}
	m.setDiff(msg.GetDiff())
	m.review = msg.GetReview()
	m.findingsPane.SetContent(m.renderFindingsContent())
	m.syncLayout()
}
