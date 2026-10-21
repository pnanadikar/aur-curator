package screens

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #343 — the Build Log tab never masks a failure, and End re-reads the
// log and jumps to its bottom.

// logStore is a BuildStore with a fixed latest record and log read
// result. Only Get and ReadBuildLog are implemented; anything else
// panics via the nil embedded interface.
type logStore struct {
	domain.BuildStore
	build   *domain.Build
	getErr  error
	log     string
	readErr error
	reads   int
}

func (s *logStore) Get(domain.PackageName) (*domain.Build, error) { return s.build, s.getErr }

func (s *logStore) ReadBuildLog(domain.BuildID, string) (string, error) {
	s.reads++
	return s.log, s.readErr
}

func detailWithLog(st *logStore) PackageDetailModel {
	m := detailAt(nil, "A")
	m.deps = testDepsWithRoot(deps.Dependencies{BuildEngine: build.NewEngine(nil, st, nil, nil, nil)})
	m.tab = TabBuildLog
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	return m
}

func runningBuild() *domain.Build {
	return &domain.Build{ID: "b1", Status: domain.BuildRunning, LogLocation: "/x/build.log"}
}

func finishedBuild() *domain.Build {
	return &domain.Build{ID: "b1", Status: domain.BuildFailed, LogLocation: "/x/build.log"}
}

func TestBuildLogLoadStates(t *testing.T) {
	// The JSON store reports a missing log file as domain.ErrNotFound;
	// a raw fs.ErrNotExist is accepted too.
	missing := domain.ErrNotFound
	rawMissing := fmt.Errorf("open /x/build.log: %w", fs.ErrNotExist)
	cases := []struct {
		name       string
		st         logStore
		wantNotice string
		wantErr    string
		wantLog    string
	}{
		{"never built", logStore{getErr: domain.ErrNotFound}, "No builds yet", "", ""},
		{"no log recorded", logStore{build: &domain.Build{ID: "b0", Status: domain.BuildSuccess}}, "No log recorded for this build", "", ""},
		{"running, log not created yet", logStore{build: runningBuild(), readErr: missing}, "Waiting for output…", "", ""},
		{"running, log empty", logStore{build: runningBuild()}, "Waiting for output…", "", ""},
		{"finished, log empty", logStore{build: finishedBuild()}, "Build log is empty", "", ""},
		{"running, raw not-exist", logStore{build: runningBuild(), readErr: rawMissing}, "Waiting for output…", "", ""},
		{"finished, log missing", logStore{build: finishedBuild(), readErr: missing}, "", "build log /x/build.log is missing", ""},
		{"finished, log unreadable", logStore{build: finishedBuild(), readErr: errors.New("permission denied")}, "", "permission denied", ""},
		{"store read fails", logStore{getErr: errors.New("disk on fire")}, "", "disk on fire", ""},
		{"log present", logStore{build: runningBuild(), log: "==> Making package"}, "", "", "==> Making package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.st
			m := detailWithLog(&st)
			msg := m.loadBuildLogCmd()().(buildLogLoadedMsg)
			if msg.pkg != "yay" {
				t.Fatalf("msg.pkg = %q", msg.pkg)
			}
			if msg.notice != tc.wantNotice {
				t.Errorf("notice = %q, want %q", msg.notice, tc.wantNotice)
			}
			if tc.wantErr == "" && msg.err != nil {
				t.Errorf("unexpected error %v", msg.err)
			}
			if tc.wantErr != "" && (msg.err == nil || !strings.Contains(msg.err.Error(), tc.wantErr)) {
				t.Errorf("err = %v, want it to mention %q", msg.err, tc.wantErr)
			}
			if msg.content != tc.wantLog {
				t.Errorf("content = %q, want %q", msg.content, tc.wantLog)
			}

			// What the tab shows: never the old catch-all for a failure.
			m, _ = m.Update(msg)
			view := m.contentPane.View()
			if tc.wantErr != "" && (!strings.Contains(view, "Could not load the build log") || strings.Contains(view, "No build log available")) {
				t.Errorf("failure not shown:\n%s", view)
			}
		})
	}
}

// TestBuildLogEndReloads: End on the Build Log tab re-reads the log and,
// once it arrives, shows its bottom.
func TestBuildLogEndReloads(t *testing.T) {
	st := &logStore{build: runningBuild(), log: manyLines(50)}
	m := detailWithLog(st)
	m, _ = m.Update(m.loadBuildLogCmd()())
	before := st.reads

	st.log = manyLines(300) // the build wrote more
	m, cmd := m.Update(endKeyMsg)
	if cmd == nil {
		t.Fatal("End on the Build Log tab did not re-read the log")
	}
	m, _ = m.Update(cmd())
	if st.reads != before+1 {
		t.Fatalf("reads = %d, want %d", st.reads, before+1)
	}
	if !strings.Contains(m.contentPane.View(), "line 300") {
		t.Fatalf("not at the bottom of the new log:\n%s", m.contentPane.View())
	}
	if m.followLog {
		t.Error("followLog not cleared after the reload arrived")
	}
}

// TestBuildLogEndOtherTabsUnchanged: on other tabs End only scrolls.
func TestBuildLogEndOtherTabsUnchanged(t *testing.T) {
	st := &logStore{build: runningBuild()}
	m := detailWithLog(st)
	m.tab = TabCommits
	if _, cmd := m.Update(endKeyMsg); cmd != nil {
		t.Fatal("End on another tab started a load")
	}
}

// TestBuildLogStaleLoadIgnored: a load for another package is dropped.
func TestBuildLogStaleLoadIgnored(t *testing.T) {
	m := detailWithLog(&logStore{build: runningBuild(), log: "mine"})
	m, _ = m.Update(m.loadBuildLogCmd()())
	m, _ = m.Update(buildLogLoadedMsg{pkg: "someone-else", content: "theirs"})
	if m.buildLogContent != "mine" {
		t.Fatalf("content = %q: a stale load replaced it", m.buildLogContent)
	}
}

// testDepsWithRoot gives d a workspace root, so failures can be written
// to the error log.
func testDepsWithRoot(d deps.Dependencies) deps.Dependencies {
	d.Config.RootPath = "/tmp/aur-curator-test"
	return d
}

// TestBuildRequestShowsBuildLogTab: once the confirmation flow requests
// the build, Package Details shows the Build Log tab straight away, with
// a notice rather than the previous build's log.
func TestBuildRequestShowsBuildLogTab(t *testing.T) {
	ofHead := &domain.Build{ReviewedCommit: "abc"}
	cases := []struct {
		name    string
		prepare func(PackageDetailModel) (PackageDetailModel, tea.Msg)
		want    bool
	}{
		{"no prompt: ordinary build", func(m PackageDetailModel) (PackageDetailModel, tea.Msg) {
			m.gate = gateAt("yay", buildGateRebuildChecking)
			return m, rebuilt(m.gate, buildGateRebuildMsg{headCommit: "abc"})
		}, true},
		{"rebuild confirmed", func(m PackageDetailModel) (PackageDetailModel, tea.Msg) {
			m.gate = gateAt("yay", buildGateRebuildChecking)
			m, _ = m.Update(rebuilt(m.gate, buildGateRebuildMsg{build: ofHead, headCommit: "abc", verified: true}))
			return m, keyRune("y")
		}, true},
		{"rebuild cancelled", func(m PackageDetailModel) (PackageDetailModel, tea.Msg) {
			m.gate = gateAt("yay", buildGateRebuildChecking)
			m, _ = m.Update(rebuilt(m.gate, buildGateRebuildMsg{build: ofHead, headCommit: "abc", verified: true}))
			return m, keyRune("n")
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := detailAt(nil, "A")
			m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
			m.tab = TabSummary
			m.buildLogContent = "previous build's log"
			m, msg := tc.prepare(m)
			m, _ = m.Update(msg)

			if got := m.tab == TabBuildLog; got != tc.want {
				t.Fatalf("on Build Log tab = %v, want %v", got, tc.want)
			}
			if !tc.want {
				return
			}
			if m.buildLogContent != "" || !strings.Contains(m.contentPane.View(), "Build queued") {
				t.Fatalf("tab shows stale content:\n%s", m.contentPane.View())
			}
			if m.gate.Requested() {
				t.Error("requested flag not consumed")
			}
		})
	}
}

// requestBuild drives m's build gate to the point where it requests the
// build, as pressing B on a never-built package with no prompts would.
func requestBuild(t *testing.T, m PackageDetailModel) PackageDetailModel {
	t.Helper()
	m.tab = TabSummary
	g := gateAt(m.pkgName, buildGateRebuildChecking)
	g.deps = m.deps
	m.gate = g
	m, cmd := m.Update(rebuilt(g, buildGateRebuildMsg{headCommit: "A"}))
	wantBuildRequested(t, cmd, m.pkgName)
	if m.tab != TabBuildLog {
		t.Fatalf("setup: tab = %v, want Build Log", m.tab)
	}
	return m
}

// TestBuildLogWaitsForRequestedBuild: until a record that started after
// the request exists, End shows "waiting", never the previous build's log.
func TestBuildLogWaitsForRequestedBuild(t *testing.T) {
	prev := finishedBuild()
	prev.StartTime = time.Now().Add(-time.Hour)
	st := &logStore{build: prev, log: "OLD LOG"}
	m := requestBuild(t, detailWithLog(st))
	if st.reads != 0 {
		t.Fatal("the previous build's log was read on request")
	}

	m, cmd := m.Update(endKeyMsg)
	m, _ = m.Update(cmd())
	if v := m.contentPane.View(); !strings.Contains(v, "Waiting for the build to start") || strings.Contains(v, "OLD LOG") {
		t.Fatalf("showed the wrong thing before the build started:\n%s", v)
	}

	// The requested build has started and written output.
	st.build = runningBuild()
	st.build.StartTime = time.Now().Add(time.Second)
	st.log = "NEW LOG"
	m, cmd = m.Update(endKeyMsg)
	m, _ = m.Update(cmd())
	if !strings.Contains(m.contentPane.View(), "NEW LOG") {
		t.Fatalf("new build's log not shown:\n%s", m.contentPane.View())
	}
}

// TestBuildRequestClearedOnFailureAndCancel: a build that never records
// itself (early failure, or cancelled) must not leave the tab waiting.
func TestBuildRequestClearedOnFailureAndCancel(t *testing.T) {
	for name, end := range map[string]tea.Msg{
		"failed":    msgs.OperationFailedMsg{OpID: "yay", Err: errors.New("clone failed")},
		"cancelled": msgs.OperationCancelledMsg{OpID: "yay"},
	} {
		t.Run(name, func(t *testing.T) {
			prev := finishedBuild()
			prev.StartTime = time.Now().Add(-time.Hour)
			m := requestBuild(t, detailWithLog(&logStore{build: prev, log: "OLD LOG"}))
			m, _ = m.Update(end)
			if !m.buildRequestedAt.IsZero() {
				t.Fatal("still waiting for a build that will never start")
			}
		})
	}
}
