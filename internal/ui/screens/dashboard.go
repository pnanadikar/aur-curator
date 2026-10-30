package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/debug"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/overlay"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// PackagesLoadedMsg carries the result of a catalog.Packages() call —
// the curated package set discovered by a live filesystem scan.
//
// Skipped contains packages that were discovered but excluded from display
// because they lack both origin and upstream remotes (i.e., they were not
// added through the Import flow). Each skipped package has already had a
// warning written to the persistent error log.
type PackagesLoadedMsg struct {
	Packages []domain.PackageName
	Skipped  []domain.PackageName
	Err      error
	TraceCtx context.Context
}

// ReviewDataMsg carries the review status and upstream commit count for a
// single package. Loaded in parallel with other packages after the
// initial catalog load.
type ReviewDataMsg struct {
	Pkg        domain.PackageName
	Status     domain.ReviewStatus
	Has        bool
	Updates    int
	IsMerged   bool
	Review     *domain.Review
	HeadCommit string
	// LocalCommits is LocalOnlyCommits' result; LocalCommitsErr non-nil
	// means the check failed and LocalCommits is meaningless.
	LocalCommits    int
	LocalCommitsErr error
	// Versions are the versions the State column shows (#428).
	Versions widgets.StateVersions
	TraceCtx context.Context
}

// CardID identifies a summary card by name rather than positional index.
// Each constant corresponds to one card in the dashboard's SummaryCard row.
type CardID string

const (
	CardPackages  CardID = "packages"
	CardAUR       CardID = "aur"
	CardFetched   CardID = "fetched"
	CardToReview  CardID = "to_review"
	CardApproved  CardID = "approved"
	CardBuilt     CardID = "built"
	CardFailed    CardID = "failed"
	CardConfirmed CardID = "confirmed"
	CardMerged    CardID = "merged"
)

// cardDef describes one summary card: its ID, the short label shown on
// the card, and the descriptive text shown in the toolbar's Filter: line
// while the card is selected.
type cardDef struct {
	ID     CardID
	Label  string
	Filter string
}

// dashboardCards defines the summary cards in display order. It is the
// only place card order, labels and filter text are set
// (tui-dashboard-wireframe.md § 2).
var dashboardCards = []cardDef{
	{ID: CardPackages, Label: "Packages", Filter: "Packages"},
	{ID: CardAUR, Label: "AUR", Filter: "AUR fetch pending"},
	{ID: CardFetched, Label: "Fetched", Filter: "Fetched (not merged)"},
	{ID: CardToReview, Label: "To review", Filter: "Review Pending"},
	{ID: CardApproved, Label: "Approved", Filter: "Approved (not merged)"},
	{ID: CardMerged, Label: "Merged", Filter: "Merged (not built)"},
	{ID: CardBuilt, Label: "Built", Filter: "Built (not confirmed)"},
	{ID: CardConfirmed, Label: "Confirmed", Filter: "Confirmed (unpublished)"},
	{ID: CardFailed, Label: "Failed", Filter: "Last build failed"},
}

// CardFilter is a predicate that determines whether a package matches
// the filter semantics of a given summary card. It receives the
// package name, lifecycle state, and the package's LATEST BUILD ATTEMPT
// (or nil) — the record that says how the most recent build went. The
// last success is not passed: lifecycle already reflects it (#309).
type CardFilter func(pkg domain.PackageName, lc lifecycle.State, build *domain.Build) bool

// reviewInputs holds the raw arguments needed to call
// lifecycle.DeriveState for a package. Cached so that
// OperationCompletedMsg can re-derive without a disk read.
type reviewInputs struct {
	Status     domain.ReviewStatus
	HasReview  bool
	IsMerged   bool
	HeadCommit string
}

// DashboardModel represents the main overview screen listing all
// packages with their lifecycle status and next actions.
// localCommitsResult is one package's last LocalOnlyCommits result. It is
// for display only: the Build key re-checks live (BuildGate).
type localCommitsResult struct {
	n   int
	err error
}

type DashboardModel struct {
	deps          deps.Dependencies
	packages      []domain.PackageName
	lifecycle     map[domain.PackageName]lifecycle.State
	reviewInputs  map[domain.PackageName]reviewInputs
	reviewsByPkg  map[domain.PackageName]*domain.Review
	updates       map[domain.PackageName]int // pkg -> commitsBehind
	localCommits  map[domain.PackageName]localCommitsResult
	aurUpdates    []aurinfo.VersionPair // bulk AUR update check results
	aurStaleByPkg map[domain.PackageName]bool
	aurChecked    bool  // an AUR check result has arrived (#331)
	aurCheckErr   error // non-nil: the last AUR check failed as a whole
	loadErr       error
	loading       bool
	allData       bool
	lastRefresh   time.Time
	lastHeight    int
	width         int

	// helpOpen shows the full action help in place of the compact bar
	// (tui-package-details.md § Action Bar Convention).
	helpOpen bool

	// gate runs the Build confirmation flow (workflow.md § Build
	// Confirmation), shared with Package Details (#320).
	gate BuildGate

	summary    map[CardID]widgets.Card
	cards      []widgets.SummaryCard
	cardSel    int // index into dashboardCards (-1 = none)
	cardID     CardID
	cardFilter CardFilter

	cardFilterMap map[CardID]CardFilter

	toolbar widgets.DashboardToolbar
	pkgList widgets.LifecycleList
	keys    dashboardKeys

	// stateVersions are the State column's versions per package (#428).
	stateVersions map[domain.PackageName]widgets.StateVersions
}

type dashboardKeys struct {
	nextCardKey key.Binding
	prevCardKey key.Binding
	fetchKey    key.Binding
	mergeKey    key.Binding
	buildKey    key.Binding
	confirmKey  key.Binding
	publishKey  key.Binding
	refreshKey  key.Binding
	sortKey     key.Binding
	helpKey     key.Binding
}

func newDashboardKeys() dashboardKeys {
	return dashboardKeys{
		nextCardKey: key.NewBinding(
			key.WithKeys("ctrl+pgdown"),
			key.WithHelp("^pgdn", "Next Card"),
		),
		prevCardKey: key.NewBinding(
			key.WithKeys("ctrl+pgup"),
			key.WithHelp("^pgup", "Prev Card"),
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
		refreshKey: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "Refresh"),
		),
		sortKey: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("^O", "Sort"),
		),
		helpKey: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "Help"),
		),
	}
}

// NewDashboardModel constructs a new DashboardModel.
func NewDashboardModel(d deps.Dependencies) (m DashboardModel) {
	m = DashboardModel{
		deps:         d,
		loading:      true,
		cardSel:      0,
		cardID:       CardPackages,
		keys:         newDashboardKeys(),
		gate:         NewBuildGate(d),
		updates:      make(map[domain.PackageName]int),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		cardFilterMap: map[CardID]CardFilter{
			CardPackages: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return true
			},
			CardAUR: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				if m.aurStaleByPkg == nil {
					return false
				}
				return m.aurStaleByPkg[pkg]
			},
			CardFetched: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				up, ok := m.updates[pkg]
				return ok && up > 0
			},
			CardToReview: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return lc == lifecycle.ReviewPending
			},
			CardApproved: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return lc == lifecycle.Approved
			},
			CardMerged: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return lc == lifecycle.Merged
			},
			CardBuilt: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return lc == lifecycle.Built
			},
			CardConfirmed: func(pkg domain.PackageName, lc lifecycle.State, _ *domain.Build) bool {
				return lc == lifecycle.Confirmed
			},
			CardFailed: func(pkg domain.PackageName, lc lifecycle.State, build *domain.Build) bool {
				return inBuildFlow(lc) && build != nil && build.Status == domain.BuildFailed
			},
		},
		aurStaleByPkg: make(map[domain.PackageName]bool),
	}
	m.toolbar = widgets.NewDashboardToolbar()
	m.cardFilter = m.cardFilterMap[CardPackages]
	m.toolbar.SetFilter(cardFilterText(CardPackages))
	m.toolbar.SetSortEnabled(true)
	return
}

// isApprovedOrMerged reports whether the lifecycle state is one that
// actively participates in the build flow (Approved, Merged, Built, or Confirmed).
func isApprovedOrMerged(lc lifecycle.State) bool {
	return lc == lifecycle.Approved || lc == lifecycle.Merged || lc == lifecycle.Built || lc == lifecycle.Confirmed
}

// inBuildFlow reports whether a package has an approved review and may
// therefore have build attempts worth reporting: the isApprovedOrMerged
// states plus Published, which a failed rebuild no longer leaves (#309).
func inBuildFlow(lc lifecycle.State) bool {
	return isApprovedOrMerged(lc) || lc == lifecycle.Published
}

// cardFilterText returns the descriptive text shown in the toolbar's
// Filter: line for a card, or "" for an unknown ID.
func cardFilterText(id CardID) string {
	for _, def := range dashboardCards {
		if def.ID == id {
			return def.Filter
		}
	}
	return ""
}

// cardIDs returns the card IDs in dashboard rendering order.
func cardIDs() []CardID {
	ids := make([]CardID, len(dashboardCards))
	for i, def := range dashboardCards {
		ids[i] = def.ID
	}
	return ids
}

// cardIDFromIndex returns the CardID for a given 0-based card index.
func cardIDFromIndex(i int) CardID {
	ids := cardIDs()
	if i < 0 || i >= len(ids) {
		return ""
	}
	return ids[i]
}

// Init kicks off the initial package-list load.
func (m DashboardModel) Init() tea.Cmd {
	ctx := trace.Start(context.Background(), "startup")
	return m.loadPackagesCmd(ctx)
}

func (m DashboardModel) loadPackagesCmd(ctx context.Context) tea.Cmd {
	catalog := m.deps.Catalog
	return func() tea.Msg {
		trace.Log(ctx, "loadPackagesCmd", "state", "entered")
		pkgs, err := catalog.Packages(ctx)
		var skipped []domain.PackageName
		var valid []domain.PackageName
		for _, pkg := range pkgs {
			meta, metaErr := m.deps.Git.Metadata(ctx, pkg)
			if metaErr != nil || !git.HasExpectedRemotes(*meta) {
				skipped = append(skipped, pkg)
				trace.Log(ctx, "loadPackagesCmd", "pkg", pkg, "validated", "no")
				_ = errorlog.Logf(
					ctx,
					m.deps.Config.RootPath,
					"catalog",
					"skipped %q: no origin/upstream remotes found",
					pkg,
				)
				continue
			}
			trace.Log(ctx, "loadPackagesCmd", "pkg", pkg, "validated", "yes")
			review, reviewErr := m.deps.Reviews.EnsureReview(ctx, pkg)
			if reviewErr != nil {
				trace.Log(ctx, "loadPackagesCmd", "pkg", pkg, "backfilled", "no", "error", reviewErr.Error())
				// EnsureReview failure is non-fatal — package still
				// has valid remotes, it just won't have a backfilled
				// review record. Continue including it in the valid
				// list; review loading will surface hasReview==false.
				_ = errorlog.Logf(
					ctx,
					m.deps.Config.RootPath,
					"catalog",
					"backfill failed for %q: %v",
					pkg,
					reviewErr,
				)
			} else {
				hasExisting := review != nil
				trace.Log(ctx, "loadPackagesCmd", "pkg", pkg, "backfilled", "yes", "hasExistingReview", hasExisting)
				_ = review // backfilled; review is now Pending
			}
			valid = append(valid, pkg)
		}
		return PackagesLoadedMsg{Packages: valid, Skipped: skipped, Err: err, TraceCtx: ctx}
	}
}

// Update processes messages for the dashboard screen.
func (m DashboardModel) Update(msg tea.Msg) (DashboardModel, tea.Cmd) {
	gate, gateCmd, handled := m.gate.Update(msg)
	m.gate = gate
	if handled {
		return m, gateCmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.toolbar.SetWidth(msg.Width)
		m.width = msg.Width
		m.gate.SetWidth(msg.Width)
		m.lastHeight = msg.Height
		if m.allData {
			m.pkgList.SetPageSize(m.listPageSize(msg.Height))
		}
		return m, nil

	case PackagesLoadedMsg:
		m.loading = false
		oldPackages := m.packages
		m.packages = msg.Packages
		m.loadErr = msg.Err
		if err := msg.Err; err != nil {
			return m, nil
		}
		m.lastRefresh = time.Now()

		// Prune derived state for packages no longer on disk
		pkgSet := make(map[domain.PackageName]struct{}, len(msg.Packages))
		for _, pkg := range msg.Packages {
			pkgSet[pkg] = struct{}{}
		}
		for _, oldPkg := range oldPackages {
			if _, found := pkgSet[oldPkg]; !found {
				delete(m.lifecycle, oldPkg)
				delete(m.reviewInputs, oldPkg)
				delete(m.updates, oldPkg)
				delete(m.reviewsByPkg, oldPkg)
			}
		}

		// Load review/git data for each package
		traceCtx := msg.TraceCtx
		cmds := make([]tea.Cmd, len(msg.Packages)+1)
		for i, pkg := range msg.Packages {
			pkg := pkg
			trace.Log(traceCtx, "loadPackagesCmd", "pkg", pkg, "reviewLoaded", "yes")
			cmds[i] = m.LoadReviewCmd(traceCtx, pkg)
		}
		cmds[len(msg.Packages)] = m.loadAURUpdatesCmd(traceCtx, msg.Packages)
		return m, tea.Batch(cmds...)

	case ReviewDataMsg:
		trace.Log(msg.TraceCtx, "msg", "type", "ReviewDataMsg", "pkg", msg.Pkg)
		if m.lifecycle == nil {
			m.lifecycle = make(map[domain.PackageName]lifecycle.State)
		}
		if m.reviewInputs == nil {
			m.reviewInputs = make(map[domain.PackageName]reviewInputs)
		}
		if m.reviewsByPkg == nil {
			m.reviewsByPkg = make(map[domain.PackageName]*domain.Review)
		}
		status := domain.ReviewPending
		if msg.Status != "" {
			status = msg.Status
		}
		build, buildErr := m.getLastSuccessBuild(msg.Pkg)
		m.logReadFailure(msg.TraceCtx, msg.Pkg, "last successful build", buildErr)
		newState := lifecycle.DeriveState(lifecycle.Inputs{
			ReviewStatus: status,
			HasReview:    msg.Has,
			IsMerged:     msg.IsMerged,
			HeadCommit:   msg.HeadCommit,
			LastSuccess:  build,
		})
		trace.Log(msg.TraceCtx, "dashboard", "msg", "ReviewDataMsg", "pkg", msg.Pkg, "status", status, "hasReview", msg.Has, "isMerged", msg.IsMerged, "buildStatus", buildRecordStatus(build), "newLifecycleState", newState)
		if msg.Has {
			if m.localCommits == nil {
				m.localCommits = make(map[domain.PackageName]localCommitsResult)
			}
			m.localCommits[msg.Pkg] = localCommitsResult{n: msg.LocalCommits, err: msg.LocalCommitsErr}
			if m.stateVersions == nil {
				m.stateVersions = make(map[domain.PackageName]widgets.StateVersions)
			}
			m.stateVersions[msg.Pkg] = msg.Versions
		} else {
			// The count was not checked; show nothing rather than a stale value.
			delete(m.localCommits, msg.Pkg)
			delete(m.stateVersions, msg.Pkg)
		}
		m.lifecycle[msg.Pkg] = newState
		m.reviewInputs[msg.Pkg] = reviewInputs{
			Status:     status,
			HasReview:  msg.Has,
			IsMerged:   msg.IsMerged,
			HeadCommit: msg.HeadCommit,
		}
		if msg.Review != nil {
			m.reviewsByPkg[msg.Pkg] = msg.Review
		} else {
			delete(m.reviewsByPkg, msg.Pkg)
		}
		m.updates[msg.Pkg] = msg.Updates
		if len(m.lifecycle) == len(m.packages) {
			m.allData = true
		}
		m.summary = m.buildSummary()
		m.buildCards()
		if m.allData {
			cursor := m.pkgList.Cursor()
			var selectedPkg domain.PackageName
			visible := m.pkgList.VisibleEntries()
			if cursor >= 0 && cursor < len(visible) {
				selectedPkg = visible[cursor].Name
			}
			m.buildEntries(msg.TraceCtx, "ReviewDataMsg")
			if selectedPkg != "" {
				entries := m.pkgList.VisibleEntries()
				for i, e := range entries {
					if e.Name == selectedPkg {
						m.pkgList.SetCursor(i)
						break
					}
				}
			}
		}
		return m, nil

	case msgs.AURUpdateMsg:
		trace.Log(msg.TraceCtx, "msg", "type", "AURUpdateMsg", "pairCount", len(msg.Pairs))
		m.aurChecked = true
		m.aurCheckErr = msg.Err
		m.aurUpdates = msg.Pairs
		// Rebuild, never merge: a failed check (no pairs) must not keep
		// the previous check's staleness in the card count (#331).
		m.aurStaleByPkg = make(map[domain.PackageName]bool)
		for _, pair := range msg.Pairs {
			m.aurStaleByPkg[pair.Pkg] = pair.AURStale
		}
		m.summary = m.buildSummary()
		m.buildCards()
		if m.allData {
			cursor := m.pkgList.Cursor()
			var selectedPkg domain.PackageName
			visible := m.pkgList.VisibleEntries()
			if cursor >= 0 && cursor < len(visible) {
				selectedPkg = visible[cursor].Name
			}
			m.buildEntries(msg.TraceCtx, "AURUpdateMsg")
			if selectedPkg != "" {
				entries := m.pkgList.VisibleEntries()
				for i, e := range entries {
					if e.Name == selectedPkg {
						m.pkgList.SetCursor(i)
						break
					}
				}
			}
		}
		return m, nil

	case msgs.OperationCompletedMsg:
		trace.Log(msg.TraceCtx, "handoff", "to", "dashboard", "from", "app", "via", "OperationCompletedMsg")
		if m.lifecycle == nil {
			return m, nil
		}
		pkgName, ok := m.findPkgByOpID(msg.OpID)
		if !ok {
			return m, nil
		}
		if msg.Review != nil {
			trace.Log(msg.TraceCtx, "handoff", "to", "memory", "from", "OperationCompletedMsg", "pkg", pkgName)
			var cmd tea.Cmd
			m, cmd = m.UpdateFromReview(msg.TraceCtx, pkgName, msg.Review)
			return m, cmd
		}
		if msg.Build != nil {
			// Re-derive from LastSuccess per R1 (DeriveState uses last-success, not latest attempt).
			lastSuccess, buildErr := m.getLastSuccessBuild(pkgName)
			m.logReadFailure(msg.TraceCtx, pkgName, "last successful build", buildErr)
			if inputs, cached := m.reviewInputs[pkgName]; cached {
				trace.Log(msg.TraceCtx, "OperationCompletedMsg", "build", "cache-hit", "pkg", pkgName)
				newState := lifecycle.DeriveState(lifecycle.Inputs{
					ReviewStatus: inputs.Status,
					HasReview:    inputs.HasReview,
					IsMerged:     inputs.IsMerged,
					HeadCommit:   inputs.HeadCommit,
					LastSuccess:  lastSuccess,
				})
				m.lifecycle[pkgName] = newState
				cursor := m.pkgList.Cursor()
				var selectedPkg domain.PackageName
				visible := m.pkgList.VisibleEntries()
				if cursor >= 0 && cursor < len(visible) {
					selectedPkg = visible[cursor].Name
				}
				m.summary = m.buildSummary()
				m.buildCards()
				m.buildEntries(msg.TraceCtx, "OperationCompletedMsg")
				if selectedPkg != "" {
					entries := m.pkgList.VisibleEntries()
					for i, e := range entries {
						if e.Name == selectedPkg {
							m.pkgList.SetCursor(i)
							break
						}
					}
				}
				return m, nil
			}
			trace.Log(msg.TraceCtx, "OperationCompletedMsg", "build", "cache-miss", "pkg", pkgName)
		}
		trace.Log(msg.TraceCtx, "handoff", "to", "disk", "from", "memory", "via", "LoadReviewCmd", "pkg", pkgName)
		return m, m.LoadReviewCmd(msg.TraceCtx, pkgName)

	case msgs.OperationFailedMsg:
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(keyMsg, m.keys.helpKey) || (m.helpOpen && keyMsg.String() == "esc") {
			m.helpOpen = !m.helpOpen
			m.pkgList.SetPageSize(m.listPageSize(m.lastHeight))
			return m, nil
		}
		if key.Matches(keyMsg, m.keys.nextCardKey) {
			// Cycle to next implemented card, skipping stubs (no predicate entry).
			for {
				m.cardSel = (m.cardSel + 1) % len(cardIDs())
				m.cardID = cardIDFromIndex(m.cardSel)
				if m.cardFilterMap[m.cardID] != nil {
					break
				}
			}
			ctx := trace.Start(context.Background(), "next_card")
			m.applyCardFilter()
			m.buildEntries(ctx, "next_card")
			m.pkgList.SetCursor(0)
			return m, nil
		}
		if key.Matches(keyMsg, m.keys.prevCardKey) {
			// Cycle to previous implemented card, skipping stubs.
			for {
				m.cardSel = (m.cardSel - 1 + len(cardIDs())) % len(cardIDs())
				m.cardID = cardIDFromIndex(m.cardSel)
				if m.cardFilterMap[m.cardID] != nil {
					break
				}
			}
			ctx := trace.Start(context.Background(), "prev_card")
			m.applyCardFilter()
			m.buildEntries(ctx, "prev_card")
			m.pkgList.SetCursor(0)
			return m, nil
		}
		if key.Matches(keyMsg, m.keys.fetchKey) {
			entries := m.pkgList.VisibleEntries()
			if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
				ctx := trace.Start(context.Background(), "fetch")
				trace.Log(ctx, "key", "action", "fired", "op", "fetch", "pkg", entries[m.pkgList.Cursor()].Name)
				return m, func() tea.Msg {
					return msgs.FetchRequestedMsg{Pkg: entries[m.pkgList.Cursor()].Name, TraceCtx: ctx}
				}
			}
		}
		if key.Matches(keyMsg, m.keys.buildKey) {
			entries := m.pkgList.VisibleEntries()
			if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
				entry := entries[m.pkgList.Cursor()]
				if isBuildAvailable(entry.Lifecycle) {
					var cmd tea.Cmd
					m.gate, cmd = m.gate.Start(entry.Name)
					return m, cmd
				}
			}
		}
		if key.Matches(keyMsg, m.keys.confirmKey) {
			entries := m.pkgList.VisibleEntries()
			if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
				pkg := entries[m.pkgList.Cursor()].Name
				if m.isConfirmEnabled(pkg) {
					ctx := trace.Start(context.Background(), "confirm")
					trace.Log(ctx, "key", "action", "fired", "op", "confirm", "pkg", pkg)
					return m, func() tea.Msg {
						return msgs.ConfirmRequestedMsg{Pkg: pkg, TraceCtx: ctx}
					}
				}
			}
		}
		if key.Matches(keyMsg, m.keys.publishKey) {
			entries := m.pkgList.VisibleEntries()
			if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
				pkg := entries[m.pkgList.Cursor()].Name
				if m.isPublishEnabled(pkg) {
					ctx := trace.Start(context.Background(), "publish")
					trace.Log(ctx, "key", "action", "fired", "op", "publish", "pkg", pkg)
					return m, func() tea.Msg {
						return msgs.PublishRequestedMsg{Pkg: pkg, TraceCtx: ctx}
					}
				}
			}
		}
		if key.Matches(keyMsg, m.keys.mergeKey) {
			entries := m.pkgList.VisibleEntries()
			if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
				pkg := entries[m.pkgList.Cursor()].Name
				if m.isMergeEnabled(pkg) {
					ctx := trace.Start(context.Background(), "merge")
					trace.Log(ctx, "key", "action", "fired", "op", "merge", "pkg", pkg)
					return m, func() tea.Msg {
						return msgs.MergeRequestedMsg{Pkg: pkg, TraceCtx: ctx}
					}
				}
			}
		}
		if key.Matches(keyMsg, m.keys.refreshKey) {
			// Always re-fire the catalog load, even when the package list is
			// currently empty — an empty list can mean a genuine startup
			// failure (#287), and this is the only in-app way to retry it.
			// Show the loading state only in that case: a normal refresh of
			// an already-populated dashboard keeps today's no-flicker,
			// update-in-place behavior.
			if len(m.packages) == 0 {
				m.loading = true
			}
			m.lastRefresh = time.Now()
			tracedCtx := trace.Start(context.Background(), "refresh")
			trace.Log(tracedCtx, "key", "action", "refresh")
			ctx, _ := git.GitTimeoutCtx(tracedCtx) // cancel discarded — this ctx escapes into PackagesLoadedMsg.TraceCtx for later async reuse
			return m, m.loadPackagesCmd(ctx)
		}
		if key.Matches(keyMsg, m.keys.sortKey) && m.cardID == CardPackages {
			m.toolbar.ToggleSort()
			if m.allData {
				ctx := trace.Start(context.Background(), "toggle_sort")
				m.buildEntries(ctx, "toggle_sort")
			}
			return m, nil
		}
		switch keyMsg.String() {
		case "1":
			return m, nil
		case "2":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenRepositoryStatus, FromMenu: true} }
		case "3":
			return m, nil
		case "4":
			ctx := trace.Start(context.Background(), "nav_help")
			trace.Log(ctx, "key", "action", "nav", "target", "ScreenHelp")
			return m, func() tea.Msg {
				return msgs.NavigateMsg{To: msgs.ScreenHelp, FromMenu: true}
			}
		case "q":
			ctx := trace.Start(context.Background(), "nav_quit")
			trace.Log(ctx, "key", "action", "nav", "target", "quit")
			return m, func() tea.Msg { return msgs.QuitRequestedMsg{} }
		}
	}

	// Forward to toolbar
	cmd := m.toolbar.Update(msg)

	// Forward to package list (only when not searching)
	if m.toolbar.Value() == "" && m.allData {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "up", "k":
				m.pkgList.CursorUp()
			case "down", "j":
				m.pkgList.CursorDown()
			case "enter":
				entries := m.pkgList.VisibleEntries()
				if len(entries) > 0 && m.pkgList.Cursor() < len(entries) {
					pkg := entries[m.pkgList.Cursor()].Name
					return m, func() tea.Msg {
						return msgs.NavigateMsg{
							To:      msgs.ScreenPackageDetail,
							Package: pkg,
						}
					}
				}
			}
		}
		// pkgList is read-only; no Update method needed
	}

	return m, cmd
}

// fixedPartsAboveList returns the parts rendered above the package list,
// in render order: nav menu, summary cards, toolbar, and the table's
// header row (#448).
func (m DashboardModel) fixedPartsAboveList() []string {
	return []string{m.renderNavMenu(), m.renderSummaryCards(), m.toolbar.View(), m.pkgList.HeaderRow()}
}

// listRowsLeft returns the rows left for the package list after the
// screen's own chrome — the nav menu, summary cards, toolbar, header row
// and action bar, plus the section-box frame the App wraps around this
// screen — with no floor (widgets.FlexRows). The App has already
// subtracted its own chrome from height before it reaches here.
func (m DashboardModel) listRowsLeft(height int) int {
	parts := append(m.fixedPartsAboveList(), m.renderActionBar())
	return widgets.FlexRows(height, parts...)
}

// listPageSize returns the page size for height: every row left, down to
// one, so the page always fits and the selected row is never cut off
// (tui-dashboard-wireframe.md § Page Size, #450). Other screens keep
// FlexHeight's floor. Before the first window size arrives (height 0) the
// height is unknown, not small, so the page gets FlexHeight's floor until
// it does.
func (m DashboardModel) listPageSize(height int) int {
	if height <= 0 {
		return widgets.MinFlexHeight
	}
	return max(m.listRowsLeft(height), 1)
}

// TooSmall reports whether not even one package row fits, so the App
// shows the "terminal too small" line instead of the Dashboard
// (tui-dashboard-wireframe.md § 6, #450). It is false until the package
// table is shown and the height is known.
func (m DashboardModel) TooSmall() bool {
	return m.allData && m.lastHeight > 0 && m.listRowsLeft(m.lastHeight) < 1
}

// RecommendedSize returns the terminal size the "terminal too small" line
// recommends, given the terminal's height: lines at which a page holds
// widgets.MinFlexHeight rows, and columns at which the Dashboard's widest
// line is not cut (App.renderDashboardBox, widgets.WrapScreenBody).
func (m DashboardModel) RecommendedSize(terminalHeight int) (lines, cols int) {
	lines = terminalHeight + max(widgets.MinFlexHeight-m.listRowsLeft(m.lastHeight), 0)
	cols = max(lipgloss.Width(m.View()), theme.MinScreenWidth) + widgets.ScreenBodyMargin
	return lines, cols
}

// View renders the dashboard screen.
func (m DashboardModel) View() string {
	if m.loading {
		return "Loading packages...\n"
	}
	if m.loadErr != nil {
		return fmt.Sprintf("Error loading packages: %v\n", m.loadErr)
	}
	if len(m.packages) == 0 {
		return "No curated packages found.\n"
	}

	var b strings.Builder

	// Nav menu, summary cards, and toolbar rows
	if m.allData {
		for _, part := range m.fixedPartsAboveList() {
			b.WriteString(part)
			b.WriteString("\n")
		}
	}

	// Package list
	if m.allData {
		b.WriteString(m.pkgList.Render())
	} else {
		// Show basic list while loading review data
		for i, pkg := range m.packages {
			if i == 0 {
				b.WriteString("> ")
			} else {
				b.WriteString("  ")
			}
			b.WriteString(string(pkg))
			if lc, ok := m.lifecycle[pkg]; ok {
				b.WriteString("  (" + string(lc) + ")")
			} else {
				b.WriteString("  (loading...)")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString(m.renderActionBar())
	body := b.String()
	if m.gate.ModalOpen() {
		return overlay.Render(body, m.gate.View(), lipgloss.Width(body), lipgloss.Height(body))
	}
	return body
}

func (m *DashboardModel) renderSummaryCards() string {
	cards := make([]widgets.SummaryCard, len(m.cards))
	copy(cards, m.cards)
	for i := range cards {
		cards[i].SetSelected(i == m.cardSel)
	}
	return widgets.RenderCards(cards)
}

func (m *DashboardModel) buildSummary() map[CardID]widgets.Card {
	// Load all builds once for card-counting predicates that need them.
	builds := make(map[domain.PackageName]*domain.Build)
	if m.deps.BuildEngine != nil {
		bs := m.deps.BuildEngine.BuildStore()
		if bs != nil {
			for _, pkg := range m.packages {
				// Not logged: this runs on every summary rebuild, so a
				// lasting failure would be written again and again. #404
				// moves this read into the package load, which can log it
				// once (#462).
				b, err := bs.Get(pkg)
				if err == nil && b != nil {
					builds[pkg] = b
				}
			}
		}
	}

	// Count each card by applying its predicate over all packages.
	countCards := func(id CardID) int {
		count := 0
		for _, pkg := range m.packages {
			lc := m.lifecycle[pkg]
			build := builds[pkg]
			if m.cardFilterMap[id](pkg, lc, build) {
				count++
			}
		}
		return count
	}

	s := make(map[CardID]widgets.Card, len(dashboardCards))
	for _, def := range dashboardCards {
		card := widgets.Card{Label: def.Label, Count: countCards(def.ID)}
		if def.ID == CardAUR {
			card.Unknown = !m.aurChecked || m.aurCheckErr != nil
		}
		s[def.ID] = card
	}
	return s
}

func (m *DashboardModel) buildCards() {
	var cards []widgets.SummaryCard
	for _, id := range cardIDs() {
		cards = append(cards, widgets.NewSummaryCard(m.summary[id]))
	}
	m.cards = cards
}

func (m *DashboardModel) buildEntries(ctx context.Context, caller string) {
	trace.Log(ctx, "buildEntries", "from", caller)
	entries := make([]widgets.LifecycleEntry, len(m.packages))
	for i, pkg := range m.packages {
		// Not logged here: this runs on every redraw of the list and would
		// repeat the failure that the state derivation already logged
		// (logReadFailure, #462).
		lastSuccess, _ := m.getLastSuccessBuild(pkg)
		review, _ := m.reviewsByPkg[pkg]
		lc := m.localCommits[pkg]
		entries[i] = widgets.LifecycleEntry{
			Name:                pkg,
			Lifecycle:           m.lifecycle[pkg],
			Updates:             m.updates[pkg],
			NextAction:          nextActionFor(m.lifecycle[pkg]),
			Available:           m.availableFor(pkg),
			ConfirmedPublished:  m.formatConfirmedPublished(lastSuccess, m.reviewInputs[pkg].HeadCommit),
			Risk:                m.riskFor(review, m.lifecycle[pkg]),
			LocalCommits:        lc.n,
			LocalCommitsUnknown: lc.err != nil,
			Versions:            m.stateVersions[pkg],
		}
	}
	m.pkgList = widgets.NewLifecycleList(entries)
	m.pkgList.SetSort(m.toolbar.Sort())

	// Filter by the active card predicate.
	if m.cardFilter != nil {
		visibleEntries := m.pkgList.VisibleEntries()
		filtered := make([]widgets.LifecycleEntry, 0, len(visibleEntries))
		for _, e := range visibleEntries {
			latest, _ := m.buildRecord(e.Name)
			if m.cardFilter(e.Name, e.Lifecycle, latest) {
				filtered = append(filtered, e)
			}
		}
		m.pkgList = widgets.NewLifecycleList(filtered)
		m.pkgList.SetSort(m.toolbar.Sort())
		debug.Logf("dashboard", "buildEntries: cardFilter=%q → %d/%d entries filtered", m.cardID, len(filtered), len(visibleEntries))
	}

	// NewLifecycleList reset pageSize to its constructor default; restore the
	// height-derived value. Before the first WindowSizeMsg lastHeight is 0 and
	// listPageSize floors at 6.
	m.pkgList.SetPageSize(m.listPageSize(m.lastHeight))
}

// nextActionFor returns the human-readable next action label for a
// lifecycle state, following the mapping table from the dashboard wireframe.
func nextActionFor(state lifecycle.State) string {
	switch state {
	case lifecycle.NotReviewed:
		return "Fetch"
	case lifecycle.ReviewPending:
		return "Review"
	case lifecycle.Approved:
		return "Merge"
	case lifecycle.Merged:
		return "Build"
	case lifecycle.Built:
		return "Confirm"
	case lifecycle.Confirmed:
		return "Publish"
	case lifecycle.Published:
		// Final state: installing is done with pacman (#351).
		return "—"
	default:
		return "—"
	}
}

// lookupAUR returns the VersionPair for a package from the dashboard's
// cached AUR update results, or an empty pair if not found.
func (m *DashboardModel) lookupAUR(pkg domain.PackageName) (aurinfo.VersionPair, bool) {
	if m.aurUpdates == nil {
		return aurinfo.VersionPair{}, false
	}
	for _, p := range m.aurUpdates {
		if p.Pkg == pkg {
			return p, true
		}
	}
	return aurinfo.VersionPair{}, false
}

// availableFor returns the Available column value for pkg (#331).
func (m *DashboardModel) availableFor(pkg domain.PackageName) string {
	pair, found := m.lookupAUR(pkg)
	return formatAvailable(m.aurChecked, m.aurCheckErr, pair, found)
}

// formatAvailable returns the Available column value. Only a completed,
// successful comparison may read as "—" (nothing newer): a check that has
// not finished shows "…", one that failed or could not compare shows "?",
// and a package absent from the AUR response shows "not on AUR" (#331).
func formatAvailable(checked bool, checkErr error, pair aurinfo.VersionPair, found bool) string {
	switch {
	case !checked:
		return "…"
	case checkErr != nil, !found:
		return "?"
	case pair.AURUnknown:
		return "not on AUR"
	case pair.LocalUnknown, pair.AURVersion == "", pair.LocalVersion == "":
		return "?"
	case pair.AURStale:
		// Only the AUR's version: State shows the reviewed one (#428).
		// One too long for the column is cut with "…" when drawn.
		return pair.AURVersion
	default:
		return "—"
	}
}

// formatConfirmedPublished returns the Published column value, from the
// last success (workflow.md § State Transitions, decision R7):
// "Yes" when published and its ReviewedCommit is the current HEAD,
// "Older" when published but for an older commit (a previous version is
// in the package repository; the merged one is not built yet),
// "No" otherwise. Uses the cached HEAD commit — no git calls.
func (m *DashboardModel) formatConfirmedPublished(lastSuccess *domain.Build, cachedHeadCommit string) string {
	if lastSuccess == nil || lastSuccess.Status != domain.BuildSuccess {
		return "No"
	}
	if lastSuccess.Published && lastSuccess.ReviewedCommit == cachedHeadCommit {
		return "Yes"
	}
	if lastSuccess.Published {
		return "Older"
	}
	return "No"
}

// riskFor returns the Risk column value for a package. It evaluates cases
// in order: nil review → "No review"; not run → "Not Run"; ran with zero
// findings → "NONE"; decided (approved/rejected) with RiskAssessment → its
// HighestSeverity; otherwise (findings present) → highest severity from
// live findings.
func (m *DashboardModel) riskFor(review *domain.Review, lc lifecycle.State) string {
	if review == nil {
		return "No review"
	}
	if review.FindingsStatus == domain.FindingsNotRun {
		return "Not Run"
	}
	if review.FindingsStatus == domain.FindingsRun && len(review.Findings) == 0 {
		return "NONE"
	}
	if review.Status == domain.ReviewApproved || review.Status == domain.ReviewRejected {
		if review.RiskAssessment != nil {
			return strings.ToUpper(string(review.RiskAssessment.HighestSeverity))
		}
	}
	return m.highestSeverityForFindings(review.Findings)
}

// highestSeverityForFindings returns the highest severity found in the
// findings slice using the policy engine's category-to-severity mapping.
// Returns "HIGH" > "MEDIUM" > "LOW". Callers must pass at least one
// finding; callers with zero findings should return "NONE" instead.
func (m *DashboardModel) highestSeverityForFindings(findings []domain.Finding) string {
	var highest = domain.SeverityLow
	for _, f := range findings {
		s := m.deps.Policy.Severity(f.Category)
		if s == domain.SeverityHigh {
			return "HIGH"
		}
		if s == domain.SeverityMedium && highest != domain.SeverityHigh {
			highest = domain.SeverityMedium
		}
	}
	if highest == domain.SeverityMedium {
		return "MEDIUM"
	}
	return "LOW"
}

// buildRecordStatus returns the build status string for a build record,
// or "nil" if the record is empty.
func buildRecordStatus(b *domain.Build) string {
	if b == nil {
		return "nil"
	}
	return string(b.Status)
}

// buildRecord returns the package's latest build attempt (any status), or
// nil if none exists. Used only for build-status display and the Failed
// card — never for lifecycle (#309).
func (m *DashboardModel) buildRecord(pkg domain.PackageName) (*domain.Build, error) {
	if m.deps.BuildEngine == nil {
		return nil, nil
	}
	bs := m.deps.BuildEngine.BuildStore()
	if bs == nil {
		return nil, nil
	}
	return bs.Get(pkg)
}

// getLastSuccessBuild returns the last-success build record for a package, or nil if none exists.
func (m *DashboardModel) getLastSuccessBuild(pkg domain.PackageName) (*domain.Build, error) {
	if m.deps.BuildEngine == nil {
		return nil, nil
	}
	bs := m.deps.BuildEngine.BuildStore()
	if bs == nil {
		return nil, nil
	}
	return bs.GetLastSuccess(pkg)
}

func (m *DashboardModel) applyCardFilter() {
	m.cardID = cardIDFromIndex(m.cardSel)
	filter := m.cardFilterMap[m.cardID]
	if filter == nil {
		m.cardFilter = nil
		m.toolbar.SetFilter("")
		m.toolbar.SetSortEnabled(false)
		m.pkgList.SetFilter("")
		return
	}
	m.cardFilter = filter
	m.toolbar.SetFilter(cardFilterText(m.cardID))
	m.toolbar.SetSortEnabled(m.cardID == CardPackages)
	m.pkgList.SetFilter("")
	debug.Logf("dashboard", "applyCardFilter: cardSel=%d cardID=%q cardFilter=%v toolbarFilter=%q",
		m.cardSel, m.cardID, m.cardFilter != nil, m.toolbar.Filter())
}

func (m *DashboardModel) findPkgByOpID(opID string) (domain.PackageName, bool) {
	if m.lifecycle == nil {
		return "", false
	}
	if _, ok := m.lifecycle[domain.PackageName(opID)]; ok {
		return domain.PackageName(opID), true
	}
	return "", false
}

// UpdateFromReview applies an in-memory review to the dashboard state
// without re-fetching from disk. It checks whether the review's
// UpstreamCommit has been merged into master and updates the lifecycle
// entry accordingly, then rebuilds the summary and entry list.
// Returns a Cmd that may fire a toast if the previously selected
// package is no longer present after the update.
func (m *DashboardModel) UpdateFromReview(ctx context.Context, pkg domain.PackageName, review *domain.Review) (DashboardModel, tea.Cmd) {
	if m.lifecycle == nil {
		return *m, nil
	}
	isMerged := false
	if review.UpstreamCommit != "" && m.deps.Git != nil {
		var err error
		isMerged, err = m.deps.Git.IsMerged(ctx, pkg, review.UpstreamCommit)
		m.logReadFailure(ctx, pkg, "merge state", err)
	} else {
		trace.Log(ctx, "skipped", "check", "IsMerged", "reason", "no-upstream-commit-or-no-git")
	}
	trace.Log(ctx, "UpdateFromReview", "pkg", pkg, "isMerged", isMerged, "upstreamCommit", review.UpstreamCommit)
	build, buildErr := m.getLastSuccessBuild(pkg)
	m.logReadFailure(ctx, pkg, "last successful build", buildErr)
	var headCommit string
	if m.deps.Git != nil {
		hc, err := m.deps.Git.LocalHeadCommit(ctx, pkg)
		if err != nil {
			trace.Log(ctx, "UpdateFromReview", "check", "LocalHeadCommit", "pkg", pkg, "error", err.Error())
			m.logReadFailure(ctx, pkg, "master HEAD", err)
		} else {
			headCommit = hc
		}
	}
	m.lifecycle[pkg] = lifecycle.DeriveState(lifecycle.Inputs{
		ReviewStatus: review.Status,
		HasReview:    true,
		IsMerged:     isMerged,
		HeadCommit:   headCommit,
		LastSuccess:  build,
	})
	m.reviewInputs[pkg] = reviewInputs{
		Status:     review.Status,
		HasReview:  true,
		IsMerged:   isMerged,
		HeadCommit: headCommit,
	}
	m.reviewsByPkg[pkg] = review
	if m.stateVersions == nil {
		m.stateVersions = make(map[domain.PackageName]widgets.StateVersions)
	}
	m.stateVersions[pkg] = m.readStateVersions(ctx, pkg, review, isMerged, headCommit)
	cursor := m.pkgList.Cursor()
	var selectedPkg domain.PackageName
	visible := m.pkgList.VisibleEntries()
	if cursor >= 0 && cursor < len(visible) {
		selectedPkg = visible[cursor].Name
	}
	m.summary = m.buildSummary()
	m.buildCards()
	m.buildEntries(ctx, "UpdateFromReview")
	if selectedPkg != "" {
		entries := m.pkgList.VisibleEntries()
		found := false
		for i, e := range entries {
			if e.Name == selectedPkg {
				m.pkgList.SetCursor(i)
				found = true
				break
			}
		}
		if !found {
			m.pkgList.SetCursor(0)
			return *m, tea.Sequence(
				func() tea.Msg {
					return msgs.ToastMsg{Text: fmt.Sprintf("%s was removed from the workspace — selection reset", selectedPkg)}
				},
			)
		}
	}
	return *m, nil
}

// LoadReviewCmd loads review data for a single package.
func (m *DashboardModel) LoadReviewCmd(ctx context.Context, pkg domain.PackageName) tea.Cmd {
	return func() tea.Msg {
		if m.deps.Reviews == nil || m.deps.Git == nil {
			return ReviewDataMsg{Pkg: pkg, Has: false, TraceCtx: ctx}
		}
		review, err := m.deps.Reviews.GetReview(ctx, pkg)
		if err != nil {
			m.logReadFailure(ctx, pkg, "review", err)
			return ReviewDataMsg{Pkg: pkg, Has: false, TraceCtx: ctx}
		}
		status := review.Status
		has := true

		updates, commitsErr := m.deps.Git.CommitsBehind(ctx, pkg)
		if commitsErr != nil {
			trace.Log(ctx, "ReviewDataMsg", "check", "CommitsBehind", "pkg", pkg, "error", commitsErr.Error())
		}

		isMerged, isMergedErr := false, (error)(nil)
		if review.UpstreamCommit != "" {
			isMerged, isMergedErr = m.deps.Git.IsMerged(ctx, pkg, review.UpstreamCommit)
			if isMergedErr != nil {
				trace.Log(ctx, "ReviewDataMsg", "check", "IsMerged", "pkg", pkg, "commit", review.UpstreamCommit, "error", isMergedErr.Error())
				m.logReadFailure(ctx, pkg, "merge state", isMergedErr)
			}
		}

		var headCommit string
		if m.deps.Git != nil {
			hc, err := m.deps.Git.LocalHeadCommit(ctx, pkg)
			if err != nil {
				trace.Log(ctx, "ReviewDataMsg", "check", "LocalHeadCommit", "pkg", pkg, "error", err.Error())
				m.logReadFailure(ctx, pkg, "master HEAD", err)
			} else {
				headCommit = hc
			}
		}

		localCommits, localCommitsErr := m.deps.Git.LocalOnlyCommits(ctx, pkg)
		if localCommitsErr != nil {
			trace.Log(ctx, "ReviewDataMsg", "check", "LocalOnlyCommits", "pkg", pkg, "error", localCommitsErr.Error())
		}

		versions := m.readStateVersions(ctx, pkg, review, isMerged, headCommit)

		return ReviewDataMsg{
			Pkg:             pkg,
			Status:          status,
			Has:             has,
			Updates:         updates,
			IsMerged:        isMerged,
			Review:          review,
			HeadCommit:      headCommit,
			LocalCommits:    localCommits,
			LocalCommitsErr: localCommitsErr,
			Versions:        versions,
			TraceCtx:        ctx,
		}
	}
}

// logReadFailure writes a failed read that a package's lifecycle state is
// derived from to the error log (#462). The state is still derived as if
// the read had found nothing — fail-closed, as the operations re-check
// live — but the failure is never silent. A nil error and
// domain.ErrNotFound (no review, no build yet) are not failures.
func (m DashboardModel) logReadFailure(ctx context.Context, pkg domain.PackageName, what string, err error) {
	if err == nil || errors.Is(err, domain.ErrNotFound) {
		return
	}
	_ = errorlog.Logf(ctx, m.deps.Config.RootPath, "state", "read %s of %q: %v", what, pkg, err)
}

// readStateVersions reads the versions the State column shows for a
// package (tui-dashboard-wireframe.md § State, #428): before the merge,
// the review's reviewed version and, unless it is a first review, its base
// version; after the merge, the version at master HEAD. isMerged and
// headCommit are the caller's results, the ones the lifecycle state is
// derived from. Each read runs under its own git timeout (VersionAt).
//
// A failed git read is written to the error log and leaves that version
// "", as does a version the PKGBUILD computes or lacks, which is a
// property of the package and is not logged. The base is read only when
// the reviewed version was, since State never shows half a pair.
func (m DashboardModel) readStateVersions(ctx context.Context, pkg domain.PackageName, review *domain.Review, isMerged bool, headCommit string) widgets.StateVersions {
	var v widgets.StateVersions
	if review == nil || m.deps.Git == nil {
		return v
	}
	read := func(what, commit string) string {
		if commit == "" {
			return ""
		}
		ver, err := aurinfo.VersionAt(ctx, m.deps.Git, pkg, commit)
		if err != nil && !errors.Is(err, aurinfo.ErrVersionNotReadable) {
			trace.Log(ctx, "readStateVersions", "pkg", pkg, "version", what, "error", err.Error())
			_ = errorlog.Logf(ctx, m.deps.Config.RootPath, "version", "read %s version of %q: %v", what, pkg, err)
		}
		return ver
	}
	switch review.Status {
	case domain.ReviewApproved:
		if isMerged {
			v.Head = read("master HEAD", headCommit)
			return v
		}
		fallthrough
	case domain.ReviewPending:
		v.Reviewed = read("reviewed", review.UpstreamCommit)
		if review.BaseCommit == "" {
			v.FirstReview = true
		} else if v.Reviewed != "" {
			v.Base = read("base", review.BaseCommit)
		}
	case domain.ReviewRejected:
		v.Reviewed = read("reviewed", review.UpstreamCommit)
	}
	return v
}

// loadAURUpdatesCmd performs a bulk AUR update check for all currently
// loaded packages. The result is delivered as an AURUpdateMsg.
func (m DashboardModel) loadAURUpdatesCmd(ctx context.Context, packages []domain.PackageName) tea.Cmd {
	return func() tea.Msg {
		aur := m.deps.AUR
		if aur == nil || len(packages) == 0 {
			return msgs.AURUpdateMsg{Pairs: nil, TraceCtx: ctx}
		}
		trace.Log(ctx, "dashboard", "loadAURUpdatesCmd", "entered", "pkgCount", len(packages))
		// Wrap git-bound operations (resolveLocal in BulkCheck) with
		// a timeout to prevent indefinite hangs (#215).
		gitCtx, gitCancel := git.GitTimeoutCtx(ctx)
		defer gitCancel()
		result := aur.BulkCheck(gitCtx, packages)
		if result.Err != nil {
			trace.Log(ctx, "dashboard", "loadAURUpdatesCmd", "BulkCheck", "error", result.Err.Error())
		}
		if result.Err != nil {
			_ = errorlog.Logf(ctx, m.deps.Config.RootPath, "aur", "AUR update check failed: %v", result.Err)
		}
		return msgs.AURUpdateMsg{Pairs: result.Pairs, Err: result.Err, TraceCtx: ctx}
	}
}

// selectedEntry returns the row under the cursor, if any.
func (m DashboardModel) selectedEntry() (widgets.LifecycleEntry, bool) {
	entries := m.pkgList.VisibleEntries()
	if len(entries) == 0 || m.pkgList.Cursor() >= len(entries) {
		return widgets.LifecycleEntry{}, false
	}
	return entries[m.pkgList.Cursor()], true
}

// actions lists the Dashboard's actions in their fixed bar order, with
// availability for the selected row (tui-package-details.md § Action Bar
// Convention).
func (m DashboardModel) actions() []widgets.Action {
	entry, ok := m.selectedEntry()
	// rowReason is the reason a row action is unavailable: no row, or
	// the action's own prerequisite.
	rowReason := func(prereq string) string {
		if !ok {
			return reasonNoPackage
		}
		return prereq
	}

	mergeWhy := reasonNoPackage
	if ok {
		mergeWhy = mergeReason(m.lifecycle[entry.Name])
	}

	return []widgets.Action{
		{Key: "^PgDn/^PgUp", Label: "Cards", Enabled: true},
		{Key: "^O", Label: "Sort", Enabled: m.cardID == CardPackages, Reason: reasonPackagesCard},
		{Key: "ENTER", Label: "Open", Enabled: ok, Reason: reasonNoPackage},
		{Key: "M", Label: "Merge", Enabled: ok && m.isMergeEnabled(entry.Name), Reason: mergeWhy},
		{Key: "F", Label: "Fetch", Enabled: ok, Reason: reasonNoPackage},
		{Key: "R", Label: "Refresh", Enabled: true},
		{Key: "B", Label: "Build", Enabled: ok && isBuildAvailable(entry.Lifecycle), Reason: rowReason(prereqMergedOrLater)},
		{Key: "C", Label: "Confirm", Enabled: ok && m.isConfirmEnabled(entry.Name), Reason: rowReason(prereqBuilt)},
		{Key: "P", Label: "Publish", Enabled: ok && m.isPublishEnabled(entry.Name), Reason: rowReason(prereqConfirmed)},
	}
}

// renderActionBar renders the compact bar, or the full help while it is
// open. listPageSize measures it, so opening the help shrinks the list.
func (m DashboardModel) renderActionBar() string {
	if m.helpOpen {
		return "\n" + widgets.RenderActionHelp(m.actions(), max(m.width-4, 40))
	}
	return "\n" + widgets.RenderActionBar(m.actions())
}

func (m DashboardModel) isMergeEnabled(pkg domain.PackageName) bool {
	lc, ok := m.lifecycle[pkg]
	return ok && isMergeAvailable(lc)
}

func (m DashboardModel) isPublishEnabled(pkg domain.PackageName) bool {
	_, ok := m.lifecycle[pkg]
	return ok && m.lifecycle[pkg] == lifecycle.Confirmed
}

func (m DashboardModel) isConfirmEnabled(pkg domain.PackageName) bool {
	_, ok := m.lifecycle[pkg]
	return ok && m.lifecycle[pkg] == lifecycle.Built
}

func (m DashboardModel) renderNavMenu() string {
	return widgets.RenderNavMenu(widgets.DefaultNavItems())
}
