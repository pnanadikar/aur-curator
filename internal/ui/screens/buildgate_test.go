package screens

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// #320 — the Build confirmation flow (workflow.md § Build Confirmation),
// shared by Package Details and the Dashboard. Replaces
// localcommits316_test.go.

// gateGit is a git.Provider with fixed Status/LocalOnlyCommits/
// LocalHeadCommit results; everything else comes from mockGitProvider.
type gateGit struct {
	*mockGitProvider
	status    *git.RepositoryStatus
	statusErr error
	n         int
	nErr      error
	head      string
}

func (g gateGit) Status(context.Context, domain.PackageName) (*git.RepositoryStatus, error) {
	return g.status, g.statusErr
}
func (g gateGit) LocalOnlyCommits(context.Context, domain.PackageName) (int, error) {
	return g.n, g.nErr
}
func (g gateGit) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return g.head, nil
}

func keyRune(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

// gateAt returns a gate for pkg waiting at stage, as Start (or a
// previous stage) would have left it.
func gateAt(pkg domain.PackageName, stage buildGateStage) BuildGate {
	return BuildGate{
		width: 100,
		pkg:   pkg,
		run:   nextBuildGateRun(),
		stage: stage,
		ctx:   context.Background(),
	}
}

func checked(g BuildGate, m buildGateCheckedMsg) buildGateCheckedMsg {
	m.pkg, m.run = g.pkg, g.run
	return m
}

func rebuilt(g BuildGate, m buildGateRebuildMsg) buildGateRebuildMsg {
	m.pkg, m.run = g.pkg, g.run
	return m
}

func wantBuildRequested(t *testing.T, cmd tea.Cmd, pkg domain.PackageName) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a BuildRequestedMsg command, got nil")
	}
	got, ok := cmd().(msgs.BuildRequestedMsg)
	if !ok {
		t.Fatalf("expected BuildRequestedMsg, got %T", cmd())
	}
	if got.Pkg != pkg {
		t.Fatalf("BuildRequestedMsg.Pkg = %q, want %q", got.Pkg, pkg)
	}
}

// TestBuildGateCheckedStage covers the flow after the live status /
// local-commits check returns (formerly TestBuildFlowLocalCommitsStage).
func TestBuildGateCheckedStage(t *testing.T) {
	checkErr := errors.New("git broke")
	clean := git.RepositoryStatus{Clean: true}
	cases := []struct {
		name      string
		msg       buildGateCheckedMsg
		wantStage buildGateStage
		wantText  string
	}{
		{"clean, none: straight to rebuild check", buildGateCheckedMsg{status: clean}, buildGateRebuildChecking, ""},
		{"clean, local commits: ask", buildGateCheckedMsg{status: clean, localCommits: 2}, buildGateLocalCommits, "2 local commit(s)"},
		{"clean, check failed: ask", buildGateCheckedMsg{status: clean, localCommitsErr: checkErr}, buildGateLocalCommits, "git broke"},
		{"status error does not skip local commits", buildGateCheckedMsg{statusErr: checkErr, localCommits: 1}, buildGateLocalCommits, "1 local commit(s)"},
		{"dirty tree asks about uncommitted changes first", buildGateCheckedMsg{status: git.RepositoryStatus{HasModified: true}, localCommits: 1}, buildGateUncommitted, "uncommitted changes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := gateAt("yay", buildGateChecking)
			got, cmd, handled := g.Update(checked(g, tc.msg))
			if !handled {
				t.Fatal("gate message not handled")
			}
			if got.stage != tc.wantStage {
				t.Fatalf("stage = %v, want %v", got.stage, tc.wantStage)
			}
			if tc.wantText == "" {
				if got.ModalOpen() {
					t.Fatal("no modal expected")
				}
				if cmd == nil {
					t.Fatal("expected the rebuild-check command")
				}
				return
			}
			if !got.ModalOpen() {
				t.Fatal("expected a modal")
			}
			if view := got.View(); !strings.Contains(view, tc.wantText) {
				t.Fatalf("modal does not mention %q:\n%s", tc.wantText, view)
			}
		})
	}
}

// TestBuildGateUncommittedThenLocalCommits: the live count survives the
// uncommitted-changes modal and drives the next stage.
func TestBuildGateUncommittedThenLocalCommits(t *testing.T) {
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{HasModified: true}, localCommits: 3}))
	if g.stage != buildGateUncommitted {
		t.Fatalf("stage = %v, want uncommitted", g.stage)
	}
	g, _, _ = g.Update(keyRune("y"))
	if g.stage != buildGateLocalCommits {
		t.Fatalf("stage = %v, want local-commits", g.stage)
	}
	if !strings.Contains(g.View(), "3 local commit(s)") {
		t.Fatalf("modal lost the count:\n%s", g.View())
	}
}

// TestBuildGateRebuildStage covers the rebuild-check result and the
// rebuild modal's confirm path.
func TestBuildGateRebuildStage(t *testing.T) {
	ofHead := &domain.Build{ReviewedCommit: "abc"}
	cases := []struct {
		name      string
		msg       buildGateRebuildMsg
		wantBuild bool // BuildRequestedMsg without a prompt
		wantText  string
	}{
		{"no last success: build", buildGateRebuildMsg{headCommit: "abc"}, true, ""},
		{"last success of an older commit: build", buildGateRebuildMsg{build: &domain.Build{ReviewedCommit: "old"}, headCommit: "abc"}, true, ""},
		{"verified build of HEAD: ask", buildGateRebuildMsg{build: ofHead, headCommit: "abc", verified: true}, false, "verified build already exists"},
		{"corrupted build of HEAD: ask", buildGateRebuildMsg{build: ofHead, headCommit: "abc"}, false, "appears corrupted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := gateAt("yay", buildGateRebuildChecking)
			got, cmd, handled := g.Update(rebuilt(g, tc.msg))
			if !handled {
				t.Fatal("gate message not handled")
			}
			if tc.wantBuild {
				wantBuildRequested(t, cmd, "yay")
				if got.stage != buildGateIdle || got.ModalOpen() {
					t.Fatalf("gate not reset after requesting: stage %v", got.stage)
				}
				return
			}
			if got.stage != buildGateRebuild || !strings.Contains(got.View(), tc.wantText) {
				t.Fatalf("stage %v, modal:\n%s\nwant %q", got.stage, got.View(), tc.wantText)
			}
			got, cmd, _ = got.Update(keyRune("y"))
			wantBuildRequested(t, cmd, "yay")
			if got.stage != buildGateIdle || got.ModalOpen() {
				t.Fatalf("gate not reset after confirm: stage %v", got.stage)
			}
		})
	}
}

// TestBuildGateLocalCommitsConfirmGoesToRebuildCheck: confirming local
// commits never builds directly; it runs the rebuild check.
func TestBuildGateLocalCommitsConfirmGoesToRebuildCheck(t *testing.T) {
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{Clean: true}, localCommits: 1}))
	g, cmd, _ := g.Update(keyRune("y"))
	if g.stage != buildGateRebuildChecking || g.ModalOpen() {
		t.Fatalf("stage = %v (modal %v), want rebuild-checking", g.stage, g.ModalOpen())
	}
	if cmd == nil {
		t.Fatal("expected the rebuild-check command")
	}
	if _, ok := cmd().(buildGateRebuildMsg); !ok {
		t.Fatalf("expected buildGateRebuildMsg, got %T", cmd())
	}
}

// TestBuildGateCancelAtEveryModal: no (or Esc) at any stage ends the run
// with nothing built.
func TestBuildGateCancelAtEveryModal(t *testing.T) {
	ofHead := &domain.Build{ReviewedCommit: "abc"}
	opens := map[string]func() BuildGate{
		"uncommitted": func() BuildGate {
			g := gateAt("yay", buildGateChecking)
			g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{HasModified: true}}))
			return g
		},
		"local commits": func() BuildGate {
			g := gateAt("yay", buildGateChecking)
			g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{Clean: true}, localCommits: 1}))
			return g
		},
		"rebuild": func() BuildGate {
			g := gateAt("yay", buildGateRebuildChecking)
			g, _, _ = g.Update(rebuilt(g, buildGateRebuildMsg{build: ofHead, headCommit: "abc", verified: true}))
			return g
		},
	}
	for name, open := range opens {
		for _, k := range []tea.KeyMsg{keyRune("n"), {Type: tea.KeyEsc}} {
			t.Run(name+"/"+k.String(), func(t *testing.T) {
				g := open()
				if !g.ModalOpen() {
					t.Fatal("setup: expected a modal")
				}
				g, cmd, handled := g.Update(k)
				if !handled {
					t.Fatal("cancel key not handled")
				}
				if g.stage != buildGateIdle || g.ModalOpen() || g.run != 0 {
					t.Fatalf("gate not reset: stage %v, run %d", g.stage, g.run)
				}
				if cmd != nil {
					if _, built := cmd().(msgs.BuildRequestedMsg); built {
						t.Fatal("cancel requested a build")
					}
				}
			})
		}
	}
}

// TestBuildGateDropsForeignResults: a result is accepted only by the run,
// package and stage that are waiting for it (#320 — App delivers private
// screen messages to whichever screen is active when they arrive).
func TestBuildGateDropsForeignResults(t *testing.T) {
	waiting := gateAt("yay", buildGateChecking)
	cases := []struct {
		name string
		g    BuildGate
		msg  tea.Msg
	}{
		{"other run", waiting, buildGateCheckedMsg{pkg: "yay", run: waiting.run + 1000, status: git.RepositoryStatus{Clean: true}}},
		{"other package", waiting, buildGateCheckedMsg{pkg: "bat", run: waiting.run, status: git.RepositoryStatus{Clean: true}}},
		{"wrong stage", waiting, buildGateRebuildMsg{pkg: "yay", run: waiting.run}},
		{"idle gate", BuildGate{}, buildGateCheckedMsg{pkg: "yay", run: waiting.run, status: git.RepositoryStatus{Clean: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cmd, handled := tc.g.Update(tc.msg)
			if !handled {
				t.Fatal("foreign gate message must be consumed, not passed to the screen")
			}
			if cmd != nil {
				t.Fatal("foreign result produced a command")
			}
			if got.stage != tc.g.stage || got.run != tc.g.run || got.ModalOpen() {
				t.Fatalf("gate changed: stage %v→%v run %d→%d", tc.g.stage, got.stage, tc.g.run, got.run)
			}
		})
	}
}

// TestBuildGateModalOwnsKeys: while a modal is open every key goes to it;
// other messages still reach the screen.
func TestBuildGateModalOwnsKeys(t *testing.T) {
	g := gateAt("yay", buildGateChecking)
	g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{Clean: true}, localCommits: 1}))
	g, _, handled := g.Update(keyRune("f"))
	if !handled || !g.ModalOpen() || g.stage != buildGateLocalCommits {
		t.Fatalf("unbound key escaped the modal: handled %v, modal %v", handled, g.ModalOpen())
	}
	_, _, handled = g.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled {
		t.Fatal("non-key message swallowed by the gate")
	}
}

// TestBuildGateStart: Start runs the live checks under a fresh run, and
// refuses without git.
func TestBuildGateStart(t *testing.T) {
	g, cmd := NewBuildGate(deps.Dependencies{}).Start("yay")
	if cmd != nil || g.stage != buildGateIdle {
		t.Fatalf("no git: stage %v, cmd %v", g.stage, cmd != nil)
	}

	gp := gateGit{mockGitProvider: &mockGitProvider{}, status: &git.RepositoryStatus{Clean: true}, n: 4}
	g, cmd = NewBuildGate(deps.Dependencies{Git: gp}).Start("yay")
	if g.stage != buildGateChecking || cmd == nil {
		t.Fatalf("stage %v, cmd %v", g.stage, cmd != nil)
	}
	msg, ok := cmd().(buildGateCheckedMsg)
	if !ok {
		t.Fatalf("expected buildGateCheckedMsg, got %T", cmd())
	}
	if msg.run != g.run || msg.pkg != "yay" || msg.localCommits != 4 || !msg.status.Clean {
		t.Fatalf("unexpected check result %+v (run %d)", msg, g.run)
	}

	again, _ := g.Start("yay")
	if again.run == g.run {
		t.Fatal("a new Start must use a new run")
	}
}

// TestBuildGateStatusNilIsAnError: a nil status with no error is treated
// as a failed check, as before #320.
func TestBuildGateStatusNilIsAnError(t *testing.T) {
	gp := gateGit{mockGitProvider: &mockGitProvider{}}
	g, cmd := NewBuildGate(deps.Dependencies{Git: gp}).Start("yay")
	msg := cmd().(buildGateCheckedMsg)
	if msg.statusErr == nil {
		t.Fatal("nil status must be reported as an error")
	}
	g, _, _ = g.Update(msg)
	if g.stage != buildGateRebuildChecking {
		t.Fatalf("status error must skip only the uncommitted stage; stage = %v", g.stage)
	}
}

// TestBuildGateRebuildCheckCmd covers the live rebuild check's lookup of
// the last success and HEAD.
func TestBuildGateRebuildCheckCmd(t *testing.T) {
	cases := []struct {
		name        string
		lastSuccess map[domain.PackageName]*domain.Build
		wantBuild   bool
		wantVerif   bool
	}{
		{"never built", nil, false, false},
		{"last success of HEAD, no artifact store entries", map[domain.PackageName]*domain.Build{"yay": {ReviewedCommit: "abc"}}, true, true},
		{"last success of an older commit", map[domain.PackageName]*domain.Build{"yay": {ReviewedCommit: "old"}}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bs := &mockBuildStore{lastSuccess: tc.lastSuccess}
			gp := gateGit{mockGitProvider: &mockGitProvider{}, head: "abc"}
			eng := build.NewEngine(&mockBuilder{}, bs, &mockArtStore{}, gp, nil)
			g := gateAt("yay", buildGateRebuildChecking)
			g.deps = deps.Dependencies{Git: gp, BuildEngine: eng}
			msg := g.rebuildCheckCmd()().(buildGateRebuildMsg)
			if (msg.build != nil) != tc.wantBuild || msg.verified != tc.wantVerif {
				t.Fatalf("build %v verified %v, want %v %v", msg.build != nil, msg.verified, tc.wantBuild, tc.wantVerif)
			}
			if msg.run != g.run || msg.pkg != "yay" {
				t.Fatal("result not tagged with the run")
			}
		})
	}
}

// TestDashboardResultNotAcceptedByDetail: a check started on the
// Dashboard and delivered to Package Details (the user navigated) is
// dropped, never opening a modal for Detail's package.
func TestDashboardResultNotAcceptedByDetail(t *testing.T) {
	gp := gateGit{mockGitProvider: &mockGitProvider{}, status: &git.RepositoryStatus{Clean: true}, n: 2}
	dash := NewBuildGate(deps.Dependencies{Git: gp})
	dash, cmd := dash.Start("yay")
	result := cmd()

	// Worst case: Package Details has its own run for the same package
	// waiting at the same stage.
	detailGate, _ := NewBuildGate(deps.Dependencies{Git: gp}).Start("yay")
	detail := PackageDetailModel{pkgName: "yay", width: 100, height: 40, gate: detailGate}
	detail, dcmd := detail.Update(result)
	if detail.gate.ModalOpen() || dcmd != nil {
		t.Fatal("Package Details acted on the Dashboard's check")
	}
	_, _, handled := dash.Update(result)
	if !handled {
		t.Fatal("the Dashboard's own gate must still accept it")
	}
}

// TestDashboardBuildAvailability: Build follows the state table
// (workflow.md § Dashboard / Package Details), and local commits never
// dim it — the gate asks instead.
func TestDashboardBuildAvailability(t *testing.T) {
	cases := []struct {
		lc      lifecycle.State
		enabled bool
	}{
		{lifecycle.Merged, true},
		{lifecycle.Built, true},
		{lifecycle.Confirmed, true},
		{lifecycle.Published, true},
		{lifecycle.Approved, false},
		{lifecycle.ReviewPending, false},
		{lifecycle.Rejected, false},
		{lifecycle.NotReviewed, false},
	}
	for _, tc := range cases {
		for _, local := range []widgets.LifecycleEntry{{}, {LocalCommits: 2}, {LocalCommitsUnknown: true}} {
			entry := local
			entry.Name, entry.Lifecycle = "yay", tc.lc
			m := DashboardModel{}
			m.pkgList = widgets.NewLifecycleList([]widgets.LifecycleEntry{entry})
			got := actionByKey(t, m.actions(), "B")
			if got.Enabled != tc.enabled {
				t.Fatalf("%v (local %d, unknown %v): Build enabled = %v, want %v", tc.lc, entry.LocalCommits, entry.LocalCommitsUnknown, got.Enabled, tc.enabled)
			}
		}
	}
}

// TestDashboardBuildKeyStartsGate: b on an enabled row starts the gate,
// including rebuild states (T15); on a disabled row it does nothing.
func TestDashboardBuildKeyStartsGate(t *testing.T) {
	gp := gateGit{mockGitProvider: &mockGitProvider{}, status: &git.RepositoryStatus{Clean: true}}
	for _, tc := range []struct {
		lc   lifecycle.State
		want bool
	}{{lifecycle.Published, true}, {lifecycle.Merged, true}, {lifecycle.Approved, false}} {
		m := NewDashboardModel(deps.Dependencies{Git: gp})
		m.pkgList = widgets.NewLifecycleList([]widgets.LifecycleEntry{{Name: "yay", Lifecycle: tc.lc}})
		m, cmd := m.Update(keyRune("b"))
		started := m.gate.stage == buildGateChecking
		if started != tc.want || (cmd != nil) != tc.want {
			t.Fatalf("%v: started %v (cmd %v), want %v", tc.lc, started, cmd != nil, tc.want)
		}
		if tc.want {
			if _, ok := cmd().(buildGateCheckedMsg); !ok {
				t.Fatalf("%v: b must run the gate's checks, not build directly", tc.lc)
			}
		}
	}
}

// TestScreensModalBlocksOtherKeys: with the gate's modal open, a screen
// key (f Fetch) must not reach the screen. Before #320 Package Details
// passed keys the modal ignored through to its own handlers.
func TestScreensModalBlocksOtherKeys(t *testing.T) {
	open := func() BuildGate {
		g := gateAt("yay", buildGateChecking)
		g.deps = deps.Dependencies{Git: gateGit{mockGitProvider: &mockGitProvider{}}}
		g, _, _ = g.Update(checked(g, buildGateCheckedMsg{status: git.RepositoryStatus{Clean: true}, localCommits: 1}))
		return g
	}

	d := PackageDetailModel{pkgName: "yay", width: 100, height: 40, gate: open()}
	d.deps.Git = gateGit{mockGitProvider: &mockGitProvider{}}
	d, cmd := d.Update(keyRune("f"))
	if !d.gate.ModalOpen() {
		t.Fatal("Package Details: modal closed by f")
	}
	if cmd != nil {
		if _, fetched := cmd().(msgs.FetchRequestedMsg); fetched {
			t.Fatal("Package Details: f reached the screen while the modal was open")
		}
	}

	m := NewDashboardModel(deps.Dependencies{})
	m.pkgList = widgets.NewLifecycleList([]widgets.LifecycleEntry{{Name: "yay", Lifecycle: lifecycle.Merged}})
	m.gate = open()
	m, cmd = m.Update(keyRune("f"))
	if !m.gate.ModalOpen() {
		t.Fatal("Dashboard: modal closed by f")
	}
	if cmd != nil {
		if _, fetched := cmd().(msgs.FetchRequestedMsg); fetched {
			t.Fatal("Dashboard: f reached the screen while the modal was open")
		}
	}
}
