package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/sanitize"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// confirmGateRunSeq numbers ConfirmGate runs process-wide, as BuildGate's
// do: a result is accepted only by the run that started it.
var confirmGateRunSeq uint64

func nextConfirmGateRun() uint64 { return atomic.AddUint64(&confirmGateRunSeq, 1) }

// confirmModalPaths is how many paths the Confirm modal shows in all
// (workflow.md § Confirm Confirmation).
const confirmModalPaths = 8

type confirmGateStage int

const (
	confirmGateIdle       confirmGateStage = iota
	confirmGateInspecting                  // inspection in flight
	confirmGateModal                       // the Confirm modal is open
)

func (s confirmGateStage) String() string {
	switch s {
	case confirmGateIdle:
		return "idle"
	case confirmGateInspecting:
		return "inspecting"
	case confirmGateModal:
		return "modal"
	}
	return "unknown"
}

// confirmGateInspectedMsg carries the last success, its artifacts and
// their inspections, read in one tea.Cmd. buildErr says why build is nil.
type confirmGateInspectedMsg struct {
	pkg         domain.PackageName
	run         uint64
	build       *domain.Build
	buildErr    error
	artifacts   []domain.Artifact
	artifactErr error
	inspections []artifactInspection
	TraceCtx    context.Context
}

// confirmGateInstallMsg carries the install scripts read for `v`.
type confirmGateInstallMsg struct {
	pkg     domain.PackageName
	run     uint64
	content string
}

// ConfirmGate is the Confirm flow (workflow.md § Confirm Confirmation,
// #525): inspect the last success's artifacts, show what they install
// with special power, then ConfirmRequestedMsg on `y`. Package Details
// and the Dashboard each embed one. Like BuildGate, a screen calls Update
// first for every message; while the gate shows anything, every key is
// its own.
type ConfirmGate struct {
	deps  deps.Dependencies
	width int

	pkg   domain.PackageName
	run   uint64
	stage confirmGateStage
	ctx   context.Context

	build       *domain.Build
	artifacts   []domain.Artifact
	inspections []artifactInspection

	modal *widgets.ConfirmModal

	// artifactsWanted is set when the run ended with `a`: the screen
	// shows the Artifacts tab. Cleared by Start and Reset.
	artifactsWanted bool
	// onArtifacts is set when Confirm was pressed on the Artifacts tab:
	// no `a` line, and `a` does nothing.
	onArtifacts bool
}

// NewConfirmGate constructs an idle ConfirmGate.
func NewConfirmGate(d deps.Dependencies) ConfirmGate { return ConfirmGate{deps: d} }

// ArtifactsWanted reports whether the last run ended with `a`, and for
// which package.
func (g ConfirmGate) ArtifactsWanted() (domain.PackageName, bool) {
	return g.pkg, g.artifactsWanted
}

// ModalOpen reports whether the gate shows anything (the inspection
// notice or the Confirm modal).
func (g ConfirmGate) ModalOpen() bool { return g.modal != nil }

// View renders what the gate shows, or "".
func (g ConfirmGate) View() string {
	if g.modal == nil {
		return ""
	}
	return g.modal.View()
}

// SetWidth records the screen width used to size the modal.
func (g *ConfirmGate) SetWidth(width int) {
	g.width = width
	if g.modal != nil {
		g.modal.SetWidth(g.modalWidth())
	}
}

func (g ConfirmGate) modalWidth() int { return max(g.width-8, 48) }

// Reset abandons any run in progress; a result still in flight is
// dropped when it arrives.
func (g ConfirmGate) Reset() ConfirmGate { return ConfirmGate{deps: g.deps, width: g.width} }

// Start begins a run for pkg: the inspection, shown as a notice until it
// arrives. onArtifacts says Confirm was pressed on the Artifacts tab,
// which already shows the full list: the modal then offers no `a`.
func (g ConfirmGate) Start(pkg domain.PackageName, onArtifacts bool) (ConfirmGate, tea.Cmd) {
	g = g.Reset()
	g.onArtifacts = onArtifacts
	g.ctx = trace.Start(context.Background(), "confirm")
	g.pkg = pkg
	g.run = nextConfirmGateRun()
	g.stage = confirmGateInspecting
	trace.Log(g.ctx, "key", "action", "pressed", "op", "confirm", "pkg", pkg)
	modal := widgets.NewNoticeModal("Inspecting " + displayPath(string(pkg)) + "…")
	modal.SetWidth(g.modalWidth())
	g.modal = &modal
	return g, g.inspectCmd()
}

// Update advances the flow. handled reports whether msg was consumed.
func (g ConfirmGate) Update(msg tea.Msg) (ConfirmGate, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case confirmGateInspectedMsg:
		if g.run == 0 || msg.run != g.run || msg.pkg != g.pkg || g.stage != confirmGateInspecting {
			trace.Log(msg.TraceCtx, "confirmGate", "msg", "inspected", "pkg", msg.pkg, "result", "dropped-stale")
			return g, nil, true
		}
		g.build, g.artifacts, g.inspections = msg.build, msg.artifacts, msg.inspections
		modal := widgets.NewChoiceModal(g.modalLines(msg)...)
		modal.SetWidth(g.modalWidth())
		g.modal = &modal
		g.stage = confirmGateModal
		return g, g.modal.Focus(), true
	case confirmGateInstallMsg:
		if msg.run != g.run || msg.pkg != g.pkg || g.stage != confirmGateModal {
			return g, nil, true
		}
		return g, openPager(g.pkg, "install.txt", msg.content), true
	}

	if g.modal == nil {
		return g, nil, false
	}
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return g, nil, false
	}

	if g.stage == confirmGateInspecting {
		// Enter or Esc on the notice abandons the run.
		if result, _ := g.modal.Update(keyMsg); result.Confirmed || result.Cancelled {
			trace.Log(g.ctx, "key", "action", "cancelled", "op", "confirm", "pkg", g.pkg, "stage", g.stage.String())
			return g.Reset(), nil, true
		}
		return g, nil, true
	}

	switch keyMsg.String() {
	case "a", "A":
		if g.onArtifacts {
			return g, nil, true
		}
		trace.Log(g.ctx, "key", "action", "cancelled", "op", "confirm", "pkg", g.pkg, "then", "artifacts")
		pkg := g.pkg
		g = g.Reset()
		g.pkg = pkg
		g.artifactsWanted = true
		return g, nil, true
	case "v", "V":
		if !hasInstall(g.inspections) {
			return g, nil, true
		}
		return g, g.installCmd(), true
	}

	result, cmd := g.modal.Update(keyMsg)
	switch {
	case result.Confirmed:
		ctx, pkg := g.ctx, g.pkg
		var id domain.BuildID
		if g.build != nil {
			id = g.build.ID
		}
		trace.Log(ctx, "key", "action", "fired", "op", "confirm", "pkg", pkg, "build", id)
		return g.Reset(), func() tea.Msg {
			return msgs.ConfirmRequestedMsg{Pkg: pkg, BuildID: id, TraceCtx: ctx}
		}, true
	case result.Cancelled:
		trace.Log(g.ctx, "key", "action", "cancelled", "op", "confirm", "pkg", g.pkg, "stage", g.stage.String())
		return g.Reset(), cmd, true
	}
	return g, cmd, true
}

// modalLines is the modal's text (workflow.md § Confirm Confirmation),
// each line cut to the modal's width.
func (g ConfirmGate) modalLines(msg confirmGateInspectedMsg) []string {
	lines := []string{"Confirm " + displayPath(string(msg.pkg)) + "?"}
	switch {
	case msg.buildErr != nil:
		lines = append(lines, "", cannotInspect(fmt.Errorf("read the last successful build: %w", msg.buildErr)))
	case msg.build != nil:
		lines = append(lines, "Build "+buildIdentity(msg.build, false))
	}
	if msg.artifactErr != nil {
		lines = append(lines, "", cannotInspect(fmt.Errorf("read the artifacts: %w", msg.artifactErr)))
	}
	budget, more := confirmModalPaths, 0
	for i, a := range msg.artifacts {
		lines = append(lines, "", displayPath(a.Filename))
		if a.Checksum != "" {
			lines = append(lines, "sha256 "+a.Checksum)
		}
		var ins artifactInspection
		if i < len(msg.inspections) {
			ins = msg.inspections[i]
		} else {
			ins = artifactInspection{err: errors.New("not inspected")}
		}
		sum, shown, m := inspectionSummary(ins, budget)
		if ins.err == nil && ins.report.Empty() {
			// Set apart, so it reads as this artifact's, not as a
			// verdict on the last one (Dan, 15 Nov).
			lines = append(lines, "")
		}
		lines = append(lines, sum...)
		budget -= shown
		more += m
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf("… and %d more", more))
	}
	lines = append(lines, "")
	if hasInstall(msg.inspections) {
		lines = append(lines, "Has an install script (.INSTALL): v to read it.")
	}
	if !g.onArtifacts {
		lines = append(lines, "Full list: a to go to the Artifacts tab.")
	}
	w := g.modalWidth() - 4
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lines
}

// inspectCmd reads the last success, its artifacts and their
// inspections in one tea.Cmd.
func (g ConfirmGate) inspectCmd() tea.Cmd {
	d, pkg, run, ctx := g.deps, g.pkg, g.run, g.ctx
	return func() tea.Msg {
		out := confirmGateInspectedMsg{pkg: pkg, run: run, TraceCtx: ctx}
		if d.BuildEngine == nil || d.BuildEngine.BuildStore() == nil || d.BuildEngine.ArtifactStore() == nil {
			out.buildErr = errors.New("build store not configured")
			return out
		}
		b, err := d.BuildEngine.BuildStore().GetLastSuccess(pkg)
		if err != nil {
			out.buildErr = err
			_ = errorlog.Log(ctx, d.Config.RootPath, "inspect", fmt.Sprintf("%s: read last successful build: %v", pkg, err))
			return out
		}
		out.build = b
		arts, err := d.BuildEngine.ArtifactStore().GetArtifactsByPackage(pkg)
		if err != nil {
			out.artifactErr = err
			_ = errorlog.Log(ctx, d.Config.RootPath, "inspect", fmt.Sprintf("%s: read artifacts: %v", pkg, err))
		}
		// Only the artifacts of the build being confirmed.
		for _, a := range arts {
			if a.BuildID == b.ID {
				out.artifacts = append(out.artifacts, a)
			}
		}
		out.inspections = inspectArtifacts(ctx, d, pkg, out.artifacts)
		return out
	}
}

// installCmd reads every artifact's install script, escaped (#491), one
// after the other under its file name.
func (g ConfirmGate) installCmd() tea.Cmd {
	d, pkg, run, ctx := g.deps, g.pkg, g.run, g.ctx
	var arts []domain.Artifact
	for i, a := range g.artifacts {
		if i < len(g.inspections) && g.inspections[i].err == nil && g.inspections[i].report.HasInstall() {
			arts = append(arts, a)
		}
	}
	return func() tea.Msg {
		var b strings.Builder
		for _, a := range arts {
			fmt.Fprintf(&b, "== %s: .INSTALL ==\n\n", sanitize.Escape(a.Filename))
			s, err := d.Inspector.InstallScript(ctx, a.BuildID, a.Filename)
			if err != nil {
				fmt.Fprintf(&b, "%s\n\n", cannotInspect(err))
				_ = errorlog.Log(ctx, d.Config.RootPath, "inspect", fmt.Sprintf("%s: %s: install script: %v", pkg, a.Filename, err))
				continue
			}
			b.WriteString(sanitize.Escape(s))
			if !strings.HasSuffix(s, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		return confirmGateInstallMsg{pkg: pkg, run: run, content: b.String()}
	}
}
