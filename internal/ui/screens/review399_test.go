package screens

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// Reviews used by the #399 tests: A is the one on screen, B replaced it.
func review399A() *domain.Review {
	return &domain.Review{ID: "yay@A", PackageName: "yay", UpstreamCommit: "1111111aaaa",
		Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun}
}

func review399B() *domain.Review {
	return &domain.Review{ID: "yay@B", PackageName: "yay", UpstreamCommit: "2222222bbbb",
		Status: domain.ReviewPending, FindingsStatus: domain.FindingsNotRun}
}

// pkgbuildGit399 is a fakeGitProvider whose FileAtCommit returns a PKGBUILD
// and counts calls.
type pkgbuildGit399 struct {
	fakeGitProvider
	fileCalls *int
}

func (g pkgbuildGit399) FileAtCommit(context.Context, domain.PackageName, string, string) (string, error) {
	*g.fileCalls++
	return "pkgname=yay\npkgver=12.5\npkgrel=1\n", nil
}

// newScreen399 builds a Review screen for "yay" whose store holds stored,
// and shows shown (delivered as a first load).
func newScreen399(t *testing.T, g git.Provider, stored, shown *domain.Review, cfg domain.WorkspaceConfig) (ReviewScreen, *fakeReviewStore) {
	t.Helper()
	store := &fakeReviewStore{review: stored}
	engine := review.NewEngine(g, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{}))
	m := NewReviewScreen("yay", g, store, engine, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(ReviewScreen)
	updated, _ = m.Update(reviewLoadedMsg{review: shown})
	return updated.(ReviewScreen), store
}

func TestReplacedNoticeText399(t *testing.T) {
	got := replacedNoticeText("1111111aaaa", "2222222bbbb")
	want := "A new upstream commit arrived (1111111 → 2222222). This review was replaced; review the new changes."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestReviewChangedText399(t *testing.T) {
	tests := []struct{ version, commit, want string }{
		{"12.5-1", "2222222bbbb", "The review changed while you were viewing it (now 12.5-1, 2222222). Nothing was recorded. The screen has been reloaded — review the new changes and decide again."},
		{"", "2222222bbbb", "The review changed while you were viewing it (now 2222222). Nothing was recorded. The screen has been reloaded — review the new changes and decide again."},
	}
	for _, tt := range tests {
		if got := reviewChangedText(tt.version, tt.commit); got != tt.want {
			t.Errorf("reviewChangedText(%q, %q)\n got  %q\n want %q", tt.version, tt.commit, got, tt.want)
		}
	}
}

// TestSaveRefusalIsLogged399: a decision refused because the stored review
// changed records nothing and writes one activity-log and one error-log
// entry, like a refused Confirm/Publish.
func TestSaveRefusalIsLogged399(t *testing.T) {
	root := t.TempDir()
	actDir := t.TempDir()
	cfg := domain.WorkspaceConfig{RootPath: root, ActivityLogPath: filepath.Join(actDir, "activity.log")}
	m, store := newScreen399(t, fakeGitProvider{}, review399B(), review399A(), cfg)

	msg := m.saveReviewCmd(&domain.Review{Status: domain.ReviewApproved})()

	saved, ok := msg.(reviewSavedMsg)
	if !ok {
		t.Fatalf("got %T, want reviewSavedMsg", msg)
	}
	var rce *review.ReviewChangedError
	if saved.event != "approve" || !errors.As(saved.err, &rce) {
		t.Fatalf("event/err = %q/%v, want approve/*review.ReviewChangedError", saved.event, saved.err)
	}
	if store.review.Status != domain.ReviewPending {
		t.Fatalf("stored status = %q, want pending (nothing recorded)", store.review.Status)
	}

	const text = "review for yay changed while it was being viewed"
	act, err := os.ReadFile(cfg.ActivityLogPath)
	if err != nil || !strings.Contains(string(act), text) || !strings.Contains(string(act), "approve") {
		t.Fatalf("activity log = %q (err %v), want an approve entry containing %q", act, err, text)
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(b), text) {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("no error-log entry under %s containing %q", root, text)
	}
}

// TestSaveSuccessLogsNoRefusal399: an accepted decision is not logged as a
// refusal.
func TestSaveSuccessLogsNoRefusal399(t *testing.T) {
	root := t.TempDir()
	cfg := domain.WorkspaceConfig{RootPath: root, ActivityLogPath: filepath.Join(t.TempDir(), "activity.log")}
	m, store := newScreen399(t, fakeGitProvider{}, review399A(), review399A(), cfg)

	msg := m.saveReviewCmd(&domain.Review{Status: domain.ReviewRejected, Rationale: "no"})()

	saved, ok := msg.(reviewSavedMsg)
	if !ok || saved.err != nil || saved.event != "reject" {
		t.Fatalf("got %#v, want reviewSavedMsg with event reject and no error", msg)
	}
	if store.review.Status != domain.ReviewRejected {
		t.Fatalf("stored status = %q, want rejected", store.review.Status)
	}
	if act, _ := os.ReadFile(cfg.ActivityLogPath); strings.Contains(string(act), "changed while it was being viewed") {
		t.Fatalf("activity log has a refusal entry: %q", act)
	}
}

// TestLoadReadsVersionOnlyAfterRefusal399: the upstream version is read
// for the refusal banner only, never on an ordinary load.
func TestLoadReadsVersionOnlyAfterRefusal399(t *testing.T) {
	calls := 0
	g := pkgbuildGit399{fileCalls: &calls}
	m, _ := newScreen399(t, g, review399B(), review399A(), domain.WorkspaceConfig{})

	msg, ok := m.loadReviewCmd()().(reviewLoadedMsg)
	if !ok || msg.err != nil || msg.upstreamVersion != "" || calls != 0 {
		t.Fatalf("ordinary load: version=%q calls=%d err=%v; want \"\", 0, nil", msg.upstreamVersion, calls, msg.err)
	}

	m.pendingRefusal = "approve"
	msg, ok = m.loadReviewCmd()().(reviewLoadedMsg)
	if !ok || msg.err != nil || msg.upstreamVersion != "12.5-1" || calls != 1 {
		t.Fatalf("refusal load: version=%q calls=%d err=%v; want 12.5-1, 1, nil", msg.upstreamVersion, calls, msg.err)
	}
}

// update399 delivers msg and returns the screen and cmd.
func update399(t *testing.T, m ReviewScreen, msg tea.Msg) (ReviewScreen, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(ReviewScreen), cmd
}

// TestReloadsAfterOwnOperation399: a completed operation for this package
// does not take msg.Result; it reloads what is stored.
func TestReloadsAfterOwnOperation399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399B(), review399A(), domain.WorkspaceConfig{})

	m, cmd := update399(t, m, msgs.OperationCompletedMsg{OpID: "yay", Result: review399B()})

	if m.review.ID != "yay@A" {
		t.Fatalf("review.ID = %q right after the operation, want yay@A (not msg.Result)", m.review.ID)
	}
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	loaded, ok := cmd().(reviewLoadedMsg)
	if !ok || loaded.err != nil || loaded.review.ID != "yay@B" {
		t.Fatalf("reload produced %#v, want the stored review yay@B", loaded)
	}
}

func TestIgnoresOtherPackageOperation399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399A(), review399A(), domain.WorkspaceConfig{})

	for _, msg := range []tea.Msg{
		msgs.OperationCompletedMsg{OpID: "other", Result: review399B()},
		msgs.OperationFailedMsg{OpID: "other", Kind: "analyze", Err: &review.ReviewChangedError{Package: "other"}},
	} {
		got, cmd := update399(t, m, msg)
		if cmd != nil || got.pendingRefusal != "" || got.review.ID != "yay@A" {
			t.Fatalf("%T for another package: cmd=%v pendingRefusal=%q review=%q; want nil, \"\", yay@A",
				msg, cmd != nil, got.pendingRefusal, got.review.ID)
		}
	}
}

// TestReloadReplacedShowsToast399: a reload that brings a different review
// says so, and keeps the changed flag Back reports.
func TestReloadReplacedShowsToast399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399B(), review399A(), domain.WorkspaceConfig{})
	m.changed = true

	m, cmd := update399(t, m, reviewLoadedMsg{review: review399B()})

	if m.review.ID != "yay@B" || !m.changed || m.opFailure != nil {
		t.Fatalf("review=%q changed=%v opFailure=%v; want yay@B, true, nil", m.review.ID, m.changed, m.opFailure)
	}
	if cmd == nil {
		t.Fatal("expected a toast cmd")
	}
	toast, ok := cmd().(msgs.ToastMsg)
	want := "A new upstream commit arrived (1111111 → 2222222). This review was replaced; review the new changes."
	if !ok || toast.Text != want {
		t.Fatalf("got %#v\nwant ToastMsg %q", toast, want)
	}
}

func TestReloadSameReviewNoToast399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399A(), review399A(), domain.WorkspaceConfig{})

	m, cmd := update399(t, m, reviewLoadedMsg{review: review399A()})

	if cmd != nil || m.opFailure != nil {
		t.Fatalf("cmd=%v opFailure=%v; want nil, nil", cmd != nil, m.opFailure)
	}
}

func TestFirstLoadNoToast399(t *testing.T) {
	store := &fakeReviewStore{review: review399B()}
	engine := review.NewEngine(fakeGitProvider{}, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{}))
	m := NewReviewScreen("yay", fakeGitProvider{}, store, engine, domain.WorkspaceConfig{})

	m, cmd := update399(t, m, reviewLoadedMsg{review: review399B()})

	if cmd != nil || m.changed || m.review.ID != "yay@B" {
		t.Fatalf("cmd=%v changed=%v review=%q; want nil, false, yay@B", cmd != nil, m.changed, m.review.ID)
	}
}

// TestRefusalBanner399: an Analyze refused because the review changed
// reloads, then shows the banner naming the new version and commit.
func TestRefusalBanner399(t *testing.T) {
	tests := []struct{ version, want string }{
		{"12.5-1", "The review changed while you were viewing it (now 12.5-1, 2222222). Nothing was recorded. The screen has been reloaded — review the new changes and decide again."},
		{"", "The review changed while you were viewing it (now 2222222). Nothing was recorded. The screen has been reloaded — review the new changes and decide again."},
	}
	for _, tt := range tests {
		m, _ := newScreen399(t, fakeGitProvider{}, review399B(), review399A(), domain.WorkspaceConfig{})
		m.loading = true

		m, cmd := update399(t, m, msgs.OperationFailedMsg{OpID: "yay", Kind: "analyze",
			Err: &review.ReviewChangedError{Package: "yay", Expected: "yay@A", Current: *review399B()}})
		if m.pendingRefusal != "analyze" || cmd == nil || m.loading {
			t.Fatalf("after refusal: pendingRefusal=%q cmd=%v loading=%v; want analyze, reload, false",
				m.pendingRefusal, cmd != nil, m.loading)
		}

		m, cmd = update399(t, m, reviewLoadedMsg{review: review399B(), upstreamVersion: tt.version})

		if m.opFailure == nil || m.opFailure.kind != "analyze" || m.opFailure.err.Error() != tt.want {
			t.Fatalf("banner = %#v\nwant kind analyze, text %q", m.opFailure, tt.want)
		}
		if cmd != nil {
			t.Fatalf("got a cmd (%T); want no toast with the banner", cmd())
		}
		if m.pendingRefusal != "" || m.review.ID != "yay@B" {
			t.Fatalf("pendingRefusal=%q review=%q; want \"\", yay@B", m.pendingRefusal, m.review.ID)
		}
	}
}

// TestDecisionRefusedStays399: a refused decision does not leave the
// screen; it reloads the stored review.
func TestDecisionRefusedStays399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399B(), review399A(), domain.WorkspaceConfig{})

	m, cmd := update399(t, m, reviewSavedMsg{event: "approve",
		err: &review.ReviewChangedError{Package: "yay", Expected: "yay@A", Current: *review399B()}})

	if m.err != nil || m.modal != nil || m.pendingRefusal != "approve" || !m.changed {
		t.Fatalf("err=%v modal=%v pendingRefusal=%q changed=%v; want nil, nil, approve, true",
			m.err, m.modal != nil, m.pendingRefusal, m.changed)
	}
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	switch got := cmd().(type) {
	case reviewLoadedMsg:
		if got.review.ID != "yay@B" || got.upstreamVersion != "" {
			t.Fatalf("reload = %q / version %q; want yay@B / \"\" (fake PKGBUILD is empty)", got.review.ID, got.upstreamVersion)
		}
	default:
		t.Fatalf("cmd produced %T, want reviewLoadedMsg (no toast, no back)", got)
	}
}

// TestOtherSaveErrorStillLeaves399: any other save error keeps the old
// behaviour (toast and back).
func TestOtherSaveErrorStillLeaves399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399A(), review399A(), domain.WorkspaceConfig{})

	m, cmd := update399(t, m, reviewSavedMsg{event: "approve", err: errors.New("save failed")})

	if m.err == nil || m.pendingRefusal != "" || cmd == nil {
		t.Fatalf("err=%v pendingRefusal=%q cmd=%v; want an error, \"\", a cmd", m.err, m.pendingRefusal, cmd != nil)
	}
}

// TestAnalyzeReloadSwitchesToFindings399: the Findings tab opens on the
// reload after a completed Analyze, not after other operations.
func TestAnalyzeReloadSwitchesToFindings399(t *testing.T) {
	analysed := review399A()
	analysed.FindingsStatus = domain.FindingsRun

	for _, tc := range []struct {
		result  any
		wantTab string
	}{
		{review399A(), "Findings"},
		{"Merge completed", "Diff"},
	} {
		m, _ := newScreen399(t, fakeGitProvider{}, analysed, review399A(), domain.WorkspaceConfig{})
		m, _ = update399(t, m, msgs.OperationCompletedMsg{OpID: "yay", Result: tc.result})
		m, _ = update399(t, m, reviewLoadedMsg{review: analysed})
		if got := m.tabs.Active().Label; got != tc.wantTab {
			t.Errorf("Result %T: active tab = %q, want %q", tc.result, got, tc.wantTab)
		}
	}
}

// TestBackAfterReloadReportsChanged399: Back after an analysis and its
// reload still tells Package Details to refresh.
func TestBackAfterReloadReportsChanged399(t *testing.T) {
	m, _ := newScreen399(t, fakeGitProvider{}, review399A(), review399A(), domain.WorkspaceConfig{})

	m, _ = update399(t, m, msgs.OperationCompletedMsg{OpID: "yay", Result: review399A()})
	m, _ = update399(t, m, reviewLoadedMsg{review: review399A()})
	_, cmd := update399(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cmd == nil {
		t.Fatal("expected BackMsg cmd from Esc")
	}
	back, ok := cmd().(msgs.BackMsg)
	if !ok || !back.Changed {
		t.Fatalf("got %#v, want BackMsg{Changed: true}", back)
	}
}

// TestBackKeyIsEscOnly399: ctrl+c is App's quit key and never reaches a
// screen, so the Review screen's Back binding does not list it.
func TestBackKeyIsEscOnly399(t *testing.T) {
	m := NewReviewScreen("yay", nil, nil, nil, domain.WorkspaceConfig{})
	got := m.backKey.Keys()
	if len(got) != 1 || got[0] != "esc" {
		t.Fatalf("backKey keys = %v, want [esc]", got)
	}
}
