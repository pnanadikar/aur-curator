package screens

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/store"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// buildGateRunSeq numbers BuildGate runs process-wide. Every gate message
// carries the run that started it, so a result can only ever be accepted
// by that run — never by another screen's gate, and never by a later run
// on the same screen (#320).
var buildGateRunSeq uint64

func nextBuildGateRun() uint64 { return atomic.AddUint64(&buildGateRunSeq, 1) }

// buildGateStage is where a BuildGate run currently is.
type buildGateStage int

const (
	buildGateIdle            buildGateStage = iota
	buildGateChecking                       // status + local-commits check in flight
	buildGateUncommitted                    // uncommitted-changes modal open
	buildGateLocalCommits                   // local-commits modal open
	buildGateRebuildChecking                // rebuild check in flight
	buildGateRebuild                        // rebuild modal open
)

// String names the stage for trace output.
func (s buildGateStage) String() string {
	switch s {
	case buildGateIdle:
		return "idle"
	case buildGateChecking:
		return "checking"
	case buildGateUncommitted:
		return "uncommitted"
	case buildGateLocalCommits:
		return "local-commits"
	case buildGateRebuildChecking:
		return "rebuild-checking"
	case buildGateRebuild:
		return "rebuild"
	}
	return "unknown"
}

// buildGateCheckedMsg carries the live git.Status and LocalOnlyCommits
// results taken at the Build key press.
type buildGateCheckedMsg struct {
	pkg             domain.PackageName
	run             uint64
	status          git.RepositoryStatus
	statusErr       error
	localCommits    int
	localCommitsErr error
	TraceCtx        context.Context
}

// buildGateRebuildMsg carries the result of the live rebuild-integrity
// check (workflow.md § Rebuild Confirmation). build is the last success,
// nil when there is none or it could not be read.
type buildGateRebuildMsg struct {
	pkg        domain.PackageName
	run        uint64
	build      *domain.Build
	headCommit string
	verified   bool // all artifact checksums match
	TraceCtx   context.Context
}

// BuildGate is the Build confirmation flow (workflow.md § Build
// Confirmation): uncommitted changes, then local commits not on upstream,
// then the rebuild check, then BuildRequestedMsg. Package Details and the
// Dashboard each embed one; neither implements any part of the flow
// itself.
//
// A screen calls Update first for every message. Update consumes the
// gate's own messages and, while a modal is open, every key; everything
// else is left to the screen (handled == false).
type BuildGate struct {
	deps  deps.Dependencies
	width int

	pkg   domain.PackageName
	run   uint64
	stage buildGateStage
	ctx   context.Context

	localCommits    int
	localCommitsErr error

	modal *widgets.ConfirmModal

	// requested is set when the run ends by requesting the build, so the
	// embedding screen can react (Package Details shows the Build Log
	// tab, #343). Cleared by Start and Reset.
	requested bool
}

// NewBuildGate constructs an idle BuildGate.
func NewBuildGate(d deps.Dependencies) BuildGate {
	return BuildGate{deps: d}
}

// Requested reports whether the last run ended by requesting the build.
func (g BuildGate) Requested() bool { return g.requested }

// ModalOpen reports whether the gate is showing a confirmation modal.
func (g BuildGate) ModalOpen() bool { return g.modal != nil }

// View renders the open modal, or "" when none is open.
func (g BuildGate) View() string {
	if g.modal == nil {
		return ""
	}
	return g.modal.View()
}

// SetWidth records the screen width used to size the modal.
func (g *BuildGate) SetWidth(width int) {
	g.width = width
	if g.modal != nil {
		g.modal.SetWidth(g.modalWidth())
	}
}

func (g BuildGate) modalWidth() int { return max(g.width-8, 48) }

// Reset abandons any run in progress. A result still in flight for it is
// dropped when it arrives.
func (g BuildGate) Reset() BuildGate {
	return BuildGate{deps: g.deps, width: g.width}
}

// Start begins a new run for pkg, abandoning any previous one. A run
// whose result was delivered to another screen and dropped never blocks
// the next key press.
func (g BuildGate) Start(pkg domain.PackageName) (BuildGate, tea.Cmd) {
	g = g.Reset()
	ctx := trace.Start(context.Background(), "build")
	if g.deps.Git == nil {
		trace.Log(ctx, "key", "action", "refused", "op", "build", "pkg", pkg, "reason", "no-git")
		return g, nil
	}
	g.pkg = pkg
	g.run = nextBuildGateRun()
	g.stage = buildGateChecking
	g.ctx = ctx
	return g, g.checkCmd()
}

// Update advances the flow. handled reports whether msg was consumed.
func (g BuildGate) Update(msg tea.Msg) (BuildGate, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case buildGateCheckedMsg:
		if !g.owns(msg.pkg, msg.run, buildGateChecking) {
			trace.Log(msg.TraceCtx, "buildGate", "msg", "checked", "pkg", msg.pkg, "result", "dropped-stale")
			return g, nil, true
		}
		g.localCommits = msg.localCommits
		g.localCommitsErr = msg.localCommitsErr
		if msg.statusErr == nil && !msg.status.Clean {
			g, cmd := g.openModal(buildGateUncommitted,
				"You have uncommitted changes that won't be included",
				"in this build — Build always uses the last committed",
				"version. Continue?",
			)
			return g, cmd, true
		}
		// A failed status check skips only the uncommitted-changes
		// stage (Build uses the committed tree either way); the
		// local-commits and rebuild stages still run.
		g, cmd := g.localCommitsStage()
		return g, cmd, true

	case buildGateRebuildMsg:
		if !g.owns(msg.pkg, msg.run, buildGateRebuildChecking) {
			trace.Log(msg.TraceCtx, "buildGate", "msg", "rebuild", "pkg", msg.pkg, "result", "dropped-stale")
			return g, nil, true
		}
		if msg.build == nil || msg.build.ReviewedCommit != msg.headCommit {
			// No last success, or it is of an older commit: an
			// ordinary build, no prompt.
			g, cmd := g.request(false)
			return g, cmd, true
		}
		if msg.verified {
			g, cmd := g.openModal(buildGateRebuild,
				"A verified build already exists,",
				"rebuild anyway?",
			)
			return g, cmd, true
		}
		g, cmd := g.openModal(buildGateRebuild,
			"A build already exists but",
			"appears corrupted. Rebuild",
			"recommended. Proceed?",
		)
		return g, cmd, true
	}

	if g.modal == nil {
		return g, nil, false
	}
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		return g, nil, false
	}

	result, cmd := g.modal.Update(msg)
	switch {
	case result.Confirmed:
		stage := g.stage
		g.modal = nil
		switch stage {
		case buildGateUncommitted:
			return withHandled(g.localCommitsStage())
		case buildGateLocalCommits:
			trace.Log(g.ctx, "key", "action", "fired", "op", "build", "pkg", g.pkg, "localCommits", g.localCommitsTrace())
			return withHandled(g.rebuildStage())
		case buildGateRebuild:
			return withHandled(g.request(true))
		}
		// A modal in an unexpected stage: abandon rather than build.
		trace.Log(g.ctx, "buildGate", "confirm", "unexpected-stage", "pkg", g.pkg, "stage", stage.String())
		return g.Reset(), nil, true
	case result.Cancelled:
		trace.Log(g.ctx, "key", "action", "cancelled", "op", "build", "pkg", g.pkg, "stage", g.stage.String())
		return g.Reset(), cmd, true
	}
	// Every key belongs to the modal while it is open.
	return g, cmd, true
}

func withHandled(g BuildGate, cmd tea.Cmd) (BuildGate, tea.Cmd, bool) {
	return g, cmd, true
}

// owns reports whether a result belongs to this gate's current run and
// arrives at the stage that is waiting for it.
func (g BuildGate) owns(pkg domain.PackageName, run uint64, stage buildGateStage) bool {
	return g.run != 0 && g.run == run && g.pkg == pkg && g.stage == stage
}

func (g BuildGate) openModal(stage buildGateStage, lines ...string) (BuildGate, tea.Cmd) {
	modal := widgets.NewChoiceModal(lines...)
	modal.SetWidth(g.modalWidth())
	g.modal = &modal
	g.stage = stage
	return g, g.modal.Focus()
}

// localCommitsStage asks before building when local master has commits
// not on upstream, or when that could not be checked (workflow.md § Local
// Commits Not on Upstream); otherwise it goes on to the rebuild check.
func (g BuildGate) localCommitsStage() (BuildGate, tea.Cmd) {
	switch {
	case g.localCommitsErr != nil:
		return g.openModal(buildGateLocalCommits,
			"Could not check for local commits not on upstream:",
			strings.ReplaceAll(g.localCommitsErr.Error(), "\n", " "),
			"Build anyway?",
		)
	case g.localCommits > 0:
		return g.openModal(buildGateLocalCommits,
			fmt.Sprintf("This build includes %d local commit(s)", g.localCommits),
			"not on upstream. Build anyway?",
		)
	}
	trace.Log(g.ctx, "key", "action", "fired", "op", "build", "pkg", g.pkg)
	return g.rebuildStage()
}

func (g BuildGate) rebuildStage() (BuildGate, tea.Cmd) {
	g.stage = buildGateRebuildChecking
	return g, g.rebuildCheckCmd()
}

// request ends the run and requests the build.
func (g BuildGate) request(logFired bool) (BuildGate, tea.Cmd) {
	ctx, pkg := g.ctx, g.pkg
	if logFired {
		trace.Log(ctx, "key", "action", "fired", "op", "build", "pkg", pkg)
	}
	g = g.Reset()
	g.requested = true
	return g, func() tea.Msg {
		return msgs.BuildRequestedMsg{Pkg: pkg, TraceCtx: ctx}
	}
}

func (g BuildGate) localCommitsTrace() any {
	if g.localCommitsErr != nil {
		return "unknown"
	}
	return g.localCommits
}

// checkCmd runs the live status and local-commits checks.
func (g BuildGate) checkCmd() tea.Cmd {
	gp, pkg, run, ctx := g.deps.Git, g.pkg, g.run, g.ctx
	return func() tea.Msg {
		checkCtx, cancel := git.GitTimeoutCtx(ctx)
		defer cancel() // checkCtx never escapes; ctx does
		out := buildGateCheckedMsg{pkg: pkg, run: run, TraceCtx: ctx}
		status, err := gp.Status(checkCtx, pkg)
		switch {
		case err != nil:
			out.statusErr = err
		case status == nil:
			out.statusErr = errors.New("git status returned no result")
		default:
			out.status = *status
		}
		if out.statusErr != nil {
			trace.Log(ctx, "buildCheck", "check", "Status", "pkg", pkg, "error", out.statusErr.Error())
		}
		out.localCommits, out.localCommitsErr = gp.LocalOnlyCommits(checkCtx, pkg)
		if out.localCommitsErr != nil {
			trace.Log(ctx, "buildCheck", "check", "LocalOnlyCommits", "pkg", pkg, "error", out.localCommitsErr.Error())
		}
		return out
	}
}

// rebuildCheckCmd verifies that the last success's artifacts still match
// their stored checksums and whether it is the build of the current HEAD.
// Every case where that cannot be established falls through to an
// ordinary build, as before #320; each is now traced.
func (g BuildGate) rebuildCheckCmd() tea.Cmd {
	d, pkg, run, ctx := g.deps, g.pkg, g.run, g.ctx
	return func() tea.Msg {
		out := buildGateRebuildMsg{pkg: pkg, run: run, TraceCtx: ctx}
		if d.BuildEngine == nil {
			trace.Log(ctx, "buildCheck", "check", "rebuild", "pkg", pkg, "result", "skipped", "reason", "no-build-engine")
			return out
		}
		bs := d.BuildEngine.BuildStore()
		if bs == nil {
			trace.Log(ctx, "buildCheck", "check", "rebuild", "pkg", pkg, "result", "skipped", "reason", "no-build-store")
			return out
		}

		// R6: the last success, not the latest attempt.
		build, err := bs.GetLastSuccess(pkg)
		switch {
		case errors.Is(err, domain.ErrNotFound), err == nil && build == nil:
			return out // never built successfully: an ordinary build
		case err != nil:
			trace.Log(ctx, "buildCheck", "check", "GetLastSuccess", "pkg", pkg, "error", err.Error())
			return out
		}

		gitCtx, gitCancel := git.GitTimeoutCtx(ctx)
		defer gitCancel() // gitCtx never escapes; ctx does
		headCommit, err := d.Git.LocalHeadCommit(gitCtx, pkg)
		if err != nil {
			trace.Log(ctx, "buildCheck", "check", "LocalHeadCommit", "pkg", pkg, "error", err.Error())
			return out
		}
		out.build = build
		out.headCommit = headCommit
		if build.ReviewedCommit != headCommit {
			return out
		}

		artStore := d.BuildEngine.ArtifactStore()
		if artStore == nil || len(build.ArtifactIDs) == 0 {
			out.verified = true
			return out
		}
		out.verified = true
		for _, artID := range build.ArtifactIDs {
			artifact, err := artStore.Get(artID)
			if err != nil || artifact == nil {
				out.verified = false
				break
			}
			artifactFile := filepath.Join(
				store.NewStorePaths(d.Config.RootPath).BuildArtifactsDir(artifact.BuildID),
				artifact.Filename,
			)
			if sha256Hash(artifactFile) != artifact.Checksum {
				out.verified = false
				break
			}
		}
		return out
	}
}

// sha256Hash computes the lowercase hex SHA-256 digest of the file at
// path, or "" if it cannot be read. Same algorithm as build artifact
// recording in internal/build/engine.go.
func sha256Hash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
