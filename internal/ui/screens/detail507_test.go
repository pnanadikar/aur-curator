package screens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/theme"
)

// #507 — Build Log and Artifacts name their build, and say when an update
// fetched or merged since is not built (tui-package-details.md § Build
// Log, § Artifacts, § Update Note).

const (
	built507    = "aaaaaaaa11112222"
	head507     = "bbbbbbbb33334444"
	upstream507 = "cccccccc55556666"
)

var start507 = time.Date(2026, 11, 6, 20, 13, 0, 0, time.Local)

// header507 is the expected header line for a build of built507.
func header507(status string) string {
	s := "aaaaaaaa · " + start507.Format(buildHeaderTime)
	if status != "" {
		s += " · " + status
	}
	return s
}

const (
	mergedNote507  = "An update merged since (bbbbbbbb) is not built yet."
	fetchedNote507 = "An update fetched since (cccccccc) is not built yet."
)

// noteCase507 sets the commits Package Details knows and the note they
// should produce.
type noteCase507 struct {
	name       string
	head       string
	review     *domain.Review
	mergeKnown bool
	isMerged   bool
	want       string
}

func noteCases507() []noteCase507 {
	approved := &domain.Review{Status: domain.ReviewApproved, UpstreamCommit: upstream507}
	rejected := &domain.Review{Status: domain.ReviewRejected, UpstreamCommit: upstream507}
	return []noteCase507{
		{"current", built507, approved, true, true, ""},
		{"merged since", head507, approved, true, true, mergedNote507},
		{"fetched since", built507, approved, true, false, fetchedNote507},
		{"fetched since, pending review", built507, &domain.Review{Status: domain.ReviewPending, UpstreamCommit: upstream507}, true, false, fetchedNote507},
		{"fetched since, rejected", built507, rejected, true, false, ""},
		{"merge state unknown", built507, approved, false, false, ""},
		{"head unknown, merged", "", approved, true, true, ""},
		{"head unknown, fetched since", "", approved, true, false, fetchedNote507},
		{"no review", built507, nil, false, false, ""},
	}
}

func (c noteCase507) apply(m PackageDetailModel) PackageDetailModel {
	m.headCommit, m.review, m.mergeKnown, m.isMerged = c.head, c.review, c.mergeKnown, c.isMerged
	return m
}

// checkHeader507 checks that content starts with header, then note (if
// any), then a blank line, and has no other note.
func checkHeader507(t *testing.T, content, header, note string) {
	t.Helper()
	lines := strings.Split(content, "\n")
	want := []string{header}
	if note != "" {
		want = append(want, note)
	}
	want = append(want, "")
	if len(lines) < len(want) {
		t.Fatalf("content too short:\n%s", content)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q\ncontent:\n%s", i, lines[i], w, content)
		}
	}
	for _, other := range []string{mergedNote507, fetchedNote507} {
		if other != note && strings.Contains(content, other) {
			t.Errorf("content has %q, want no such note:\n%s", other, content)
		}
	}
}

func build507(status domain.BuildStatus) *domain.Build {
	return &domain.Build{ID: "b1", Status: status, ReviewedCommit: built507, StartTime: start507, LogLocation: "/x/build.log"}
}

// loadLog507 loads the Build Log tab through loadBuildLogCmd.
func loadLog507(m PackageDetailModel) PackageDetailModel {
	m, _ = m.Update(m.loadBuildLogCmd()())
	return m
}

func TestBuildLogHeader507(t *testing.T) {
	t.Parallel()
	for _, c := range noteCases507() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := c.apply(detailWithLog(&logStore{build: build507(domain.BuildSuccess), log: "==> Making package: yay\n"}))
			m = loadLog507(m)
			content := m.contentPane.Content()
			checkHeader507(t, content, header507("success"), c.want)
			if !strings.Contains(content, "==> Making package: yay") {
				t.Errorf("log missing:\n%s", content)
			}
			if strings.Contains(content, "Build Log") {
				t.Errorf("the Build Log heading should be gone:\n%s", content)
			}
		})
	}
}

// TestBuildLogHeaderStatus507: the status is the loaded record's, read
// with the log at every load (End), not the screen's separate m.build.
func TestBuildLogHeaderStatus507(t *testing.T) {
	t.Parallel()
	st := &logStore{build: build507(domain.BuildRunning), log: "==> Starting build()...\n"}
	m := detailWithLog(st)
	m.headCommit = built507
	m.build = &domain.Build{ID: "old", Status: domain.BuildFailed, ReviewedCommit: "ffffffff", StartTime: start507.Add(-time.Hour)}

	m = loadLog507(m)
	checkHeader507(t, m.contentPane.Content(), header507("running"), "")

	st.build = build507(domain.BuildSuccess)
	m = loadLog507(m)
	checkHeader507(t, m.contentPane.Content(), header507("success"), "")
}

// TestBuildLogNoticesHaveNoHeader507: no build, or a notice instead of a
// log, is shown as before, with no header.
func TestBuildLogNoticesHaveNoHeader507(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		st   *logStore
		want string
	}{
		{"no builds", &logStore{getErr: domain.ErrNotFound}, "No builds yet"},
		{"waiting for output", &logStore{build: build507(domain.BuildRunning), readErr: domain.ErrNotFound}, "Waiting for output…"},
		{"empty log", &logStore{build: build507(domain.BuildSuccess), log: " \n"}, "Build log is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := detailWithLog(tt.st)
			m.headCommit = head507
			m = loadLog507(m)
			if got := m.contentPane.Content(); got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildLogNoteFollowsDetailReload507: a detail reload (e.g. after a
// Fetch) updates the note without reloading the log.
func TestBuildLogNoteFollowsDetailReload507(t *testing.T) {
	t.Parallel()
	m := detailWithLog(&logStore{build: build507(domain.BuildSuccess), log: "log\n"})
	m.headCommit = built507
	m = loadLog507(m)
	checkHeader507(t, m.contentPane.Content(), header507("success"), "")

	m, _ = m.Update(detailLoadedMsg{
		review:     &domain.Review{Status: domain.ReviewPending, UpstreamCommit: upstream507},
		mergeKnown: true,
		headCommit: built507,
	})
	checkHeader507(t, m.contentPane.Content(), header507("success"), fetchedNote507)
}

// detailWithArtifacts507 is Package Details on the Artifacts tab, with
// artifacts arts and a build store whose last success is ls.
func detailWithArtifacts507(arts []domain.Artifact, bs domain.BuildStore) PackageDetailModel {
	m := detailAt(nil, "A")
	m.deps = testDepsWithRoot(deps.Dependencies{
		BuildEngine: build.NewEngine(nil, bs, &fakeArtifactStore{arts: arts}, nil, nil),
	})
	m.tab = TabArtifactHistory
	m, _ = m.Update(tea.WindowSizeMsg{Width: theme.MinScreenWidth, Height: 40})
	return m
}

func lastSuccess507(ls *domain.Build) *mockBuildStore {
	return &mockBuildStore{lastSuccess: map[domain.PackageName]*domain.Build{"yay": ls}}
}

func loadArtifacts507(m PackageDetailModel) PackageDetailModel {
	m, _ = m.Update(m.loadArtifactHistoryCmd()())
	return m
}

func arts507(buildIDs ...domain.BuildID) []domain.Artifact {
	var out []domain.Artifact
	for i, id := range buildIDs {
		out = append(out, domain.Artifact{BuildID: id, Filename: "yay-" + string(rune('a'+i)) + ".pkg.tar.zst", Architecture: "x86_64", Version: "12.5.0-1"})
	}
	return out
}

func TestArtifactsHeader507(t *testing.T) {
	t.Parallel()
	for _, c := range noteCases507() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := c.apply(detailWithArtifacts507(arts507("b1", "b1"), lastSuccess507(build507(domain.BuildSuccess))))
			m = loadArtifacts507(m)
			content := m.contentPane.Content()
			checkHeader507(t, content, header507(""), c.want)
			if !strings.Contains(content, "yay-a.pkg.tar.zst") || !strings.Contains(content, "yay-b.pkg.tar.zst") {
				t.Errorf("artifacts missing:\n%s", content)
			}
		})
	}
}

// TestArtifactsFromAnotherBuild507: artifacts whose build is not the last
// successful one get a ⚠ line naming it and no header.
func TestArtifactsFromAnotherBuild507(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		arts []domain.Artifact
		ls   *domain.Build
		want string
	}{
		{"other build", arts507("b0", "b0"), build507(domain.BuildSuccess), "⚠ These artifacts are from build b0, not the last successful build (b1)."},
		{"mixed builds", arts507("b1", "b0", "b9"), build507(domain.BuildSuccess), "from build b0, b9, not the last successful build (b1)."},
		{"no last success", arts507("b1"), nil, "from build b1, not the last successful build (none recorded)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := &mockBuildStore{}
			if tt.ls != nil {
				bs = lastSuccess507(tt.ls)
			}
			m := detailWithArtifacts507(tt.arts, bs)
			m.headCommit = head507
			m = loadArtifacts507(m)
			content := m.contentPane.Content()
			if !strings.Contains(content, tt.want) || !strings.Contains(content, "please file a bug report") {
				t.Errorf("content lacks %q and the bug-report request:\n%s", tt.want, content)
			}
			if strings.Contains(content, "aaaaaaaa · ") || strings.Contains(content, mergedNote507) {
				t.Errorf("content has a header or note for a build it cannot name:\n%s", content)
			}
			if !strings.Contains(content, "yay-a.pkg.tar.zst") {
				t.Errorf("artifacts missing:\n%s", content)
			}
		})
	}
}

// failingLastSuccess507 is a build store whose GetLastSuccess fails.
type failingLastSuccess507 struct{ mockBuildStore }

func (failingLastSuccess507) GetLastSuccess(domain.PackageName) (*domain.Build, error) {
	return nil, errors.New("read last-success build: permission denied")
}

func TestArtifactsLastSuccessUnreadable507(t *testing.T) {
	t.Parallel()
	m := loadArtifacts507(detailWithArtifacts507(arts507("b1"), &failingLastSuccess507{}))
	content := m.contentPane.Content()
	if !strings.Contains(content, "⚠ Could not read the last successful build: read last-success build: permission denied") {
		t.Errorf("content lacks the read error:\n%s", content)
	}
	if !strings.Contains(content, "yay-a.pkg.tar.zst") {
		t.Errorf("artifacts missing:\n%s", content)
	}
}

func TestArtifactsNoneUnchanged507(t *testing.T) {
	t.Parallel()
	m := detailWithArtifacts507(nil, lastSuccess507(build507(domain.BuildSuccess)))
	m.headCommit = head507
	m = loadArtifacts507(m)
	if got, want := m.contentPane.Content(), "No artifacts recorded for this package"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestUpdateNoteEmptyBuildCommit507(t *testing.T) {
	t.Parallel()
	if got := updateNote("", head507, nil, false, false); got != "" {
		t.Errorf("updateNote with no build commit = %q, want \"\"", got)
	}
}

// mergeGit507 is a fakeGitProvider whose IsMerged answers merged, err.
type mergeGit507 struct {
	fakeGitProvider
	merged bool
	err    error
}

func (g mergeGit507) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return g.merged, g.err
}

// TestDetailLoadMergeKnown507: the detail load says whether the merge
// state was read, so a failed IsMerged never reads as "not merged".
func TestDetailLoadMergeKnown507(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		g                    mergeGit507
		review               *domain.Review
		wantKnown, wantMerge bool
	}{
		{"merged", mergeGit507{merged: true}, &domain.Review{ID: "r", UpstreamCommit: upstream507}, true, true},
		{"not merged", mergeGit507{}, &domain.Review{ID: "r", UpstreamCommit: upstream507}, true, false},
		{"IsMerged fails", mergeGit507{err: errors.New("git: broken")}, &domain.Review{ID: "r", UpstreamCommit: upstream507}, false, false},
		{"no upstream commit", mergeGit507{merged: true}, &domain.Review{ID: "r"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewPackageDetailModel(deps.Dependencies{
				Git:     tt.g,
				Reviews: review.NewEngine(tt.g, &fakeReviewStore{review: tt.review}, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{})),
			})
			m.pkgName = "yay"
			msg, ok := m.loadDetailCmd()().(detailLoadedMsg)
			if !ok || msg.err != nil {
				t.Fatalf("loadDetailCmd = %#v", msg)
			}
			if msg.mergeKnown != tt.wantKnown || msg.isMerged != tt.wantMerge {
				t.Errorf("mergeKnown, isMerged = %v, %v; want %v, %v", msg.mergeKnown, msg.isMerged, tt.wantKnown, tt.wantMerge)
			}
		})
	}
}

// TestBuildIdentityFormat507 pins the header's text, year included.
func TestBuildIdentityFormat507(t *testing.T) {
	t.Parallel()
	b := build507(domain.BuildSuccess)
	if got, want := buildIdentity(b, true), "aaaaaaaa · 6 Nov 2026 20:13 · success"; got != want {
		t.Errorf("buildIdentity = %q, want %q", got, want)
	}
	if got, want := buildIdentity(b, false), "aaaaaaaa · 6 Nov 2026 20:13"; got != want {
		t.Errorf("buildIdentity without status = %q, want %q", got, want)
	}
}
