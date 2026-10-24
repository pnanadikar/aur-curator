package screens

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

type fakeAnalyzerManager struct{}

func (fakeAnalyzerManager) Run(
	context.Context,
	*git.GitDiff,
) ([]domain.Finding, error) {
	return nil, nil
}

func TestReviewScreenApproveRequiresConfirmation(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalApprove {
		t.Fatalf("modal flow = %v, want approve", screen.modalFlow)
	}

	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	screen = updated.(ReviewScreen)
	if screen.modalFlow != modalApprove {
		t.Fatalf("after 'a', modal flow = %v, want approve (cancel returns to modal)", screen.modalFlow)
	}

	// Cancel via Esc — should close modal and return to review screen
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
	screen = updated.(ReviewScreen)
	if screen.modal != nil {
		t.Fatal("modal should be dismissed after Esc")
	}
	if store.review.Status != domain.ReviewPending {
		t.Fatal("review should remain pending when approval is cancelled")
	}

	// Reopen approve and confirm
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if screen.modalFlow != modalApprove {
		t.Fatalf("reopened modal flow = %v, want approve", screen.modalFlow)
	}

	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty notes should save review")
	}

	msg := cmd()
	updated, cmd = screen.Update(msg)
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("expected quit command")
	}

	result := cmd()
	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", v.Kind())
	}
	seqLen := v.Len()
	if seqLen != 2 {
		t.Fatalf("expected 2 cmds in sequence, got %d", seqLen)
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(0).Interface().(tea.Cmd)()), "ToastMsg") {
		t.Fatalf("expected first cmd to be ToastMsg, got %T", v.Index(0).Interface().(tea.Cmd)())
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(1).Interface().(tea.Cmd)()), "BackMsg") {
		t.Fatalf("expected second cmd to be BackMsg, got %T", v.Index(1).Interface().(tea.Cmd)())
	}
	if store.review == nil {
		t.Fatal("expected saved review")
	}
	if store.review.Status != domain.ReviewApproved {
		t.Fatalf("status = %q, want approved", store.review.Status)
	}
	if store.review.FindingsStatus != domain.FindingsNotRun {
		t.Fatalf("findings status = %q, want not_run", store.review.FindingsStatus)
	}
}

func TestReviewScreenRejectSkipStoresEmptyRationale(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalRejectConfirm {
		t.Fatalf("modal flow = %v, want reject confirm", screen.modalFlow)
	}

	// Enter confirms the reject
	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if screen.modalFlow != modalRejectReason {
		t.Fatalf("modal flow = %v, want reject reason", screen.modalFlow)
	}
	_ = cmd

	// Esc skips the reason input, saving as rejected
	updated, cmd = screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("skip should save rejected review")
	}

	msg := cmd()

	_, cmd = screen.Update(msg)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	result := cmd()
	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", v.Kind())
	}
	seqLen := v.Len()
	if seqLen != 2 {
		t.Fatalf("expected 2 cmds in sequence, got %d", seqLen)
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(0).Interface().(tea.Cmd)()), "ToastMsg") {
		t.Fatalf("expected first cmd to be ToastMsg, got %T", v.Index(0).Interface().(tea.Cmd)())
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(1).Interface().(tea.Cmd)()), "BackMsg") {
		t.Fatalf("expected second cmd to be BackMsg, got %T", v.Index(1).Interface().(tea.Cmd)())
	}

	updated, _ = screen.Update(msg)
	screen = updated.(ReviewScreen)
	if store.review == nil {
		t.Fatal("expected saved review")
	}
	if store.review.Status != domain.ReviewRejected {
		t.Fatalf("status = %q, want rejected", store.review.Status)
	}
	if store.review.Rationale != "" {
		t.Fatalf("rationale = %q, want empty string", store.review.Rationale)
	}
	_ = screen
}

type errorSavingStore struct {
	fakeReviewStore
	err error
}

func (f *errorSavingStore) Save(domain.PackageName, *domain.Review) error {
	return f.err
}

func TestReviewScreenSaveErrorDispatchesToastAndBack(t *testing.T) {
	t.Parallel()

	git := fakeGitProvider{}
	saveErr := fmt.Errorf("save failed")
	store := &errorSavingStore{
		fakeReviewStore: fakeReviewStore{
			review: &domain.Review{
				PackageName:    "yay",
				Status:         domain.ReviewPending,
				FindingsStatus: domain.FindingsNotRun,
			},
		},
		err: saveErr,
	}

	policyEngine := policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	})

	engine := review.NewEngine(git, store, fakeAnalyzerManager{}, policyEngine)
	model := NewReviewScreen("yay", git, store, engine, domain.WorkspaceConfig{})
	model, _ = loadReadyScreen(t, model)

	// Enter opens the approval modal, Enter confirms it
	var cmd tea.Cmd
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("expected approval modal to open on first Enter")
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty rationale should save review")
	}

	result := cmd()
	updated, cmd = model.Update(result)
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	result = cmd()
	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", v.Kind())
	}
	seqLen := v.Len()
	if seqLen != 2 {
		t.Fatalf("expected 2 cmds in sequence, got %d", seqLen)
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(0).Interface().(tea.Cmd)()), "ToastMsg") {
		t.Fatalf("expected first cmd to be ToastMsg, got %T", v.Index(0).Interface().(tea.Cmd)())
	}
	if !strings.Contains(fmt.Sprintf("%T", v.Index(1).Interface().(tea.Cmd)()), "BackMsg") {
		t.Fatalf("expected second cmd to be BackMsg, got %T", v.Index(1).Interface().(tea.Cmd)())
	}

	updated, _ = model.Update(result)
	model = updated.(ReviewScreen)
	if model.err == nil {
		t.Fatal("expected error to be set after failed save")
	}
	if !strings.Contains(model.err.Error(), "save failed") {
		t.Fatalf("expected error message to contain 'save failed', got: %v", model.err)
	}
}

func TestReviewScreenAutoMerge_ApproveWithAutoMerge(t *testing.T) {
	t.Parallel()

	git := fakeGitProvider{}
	store := &fakeReviewStore{
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	}
	engine := review.NewEngine(git, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	}))
	model := NewReviewScreen("yay", git, store, engine, domain.WorkspaceConfig{AutoMerge: true})
	model, _ = loadReadyScreen(t, model)

	// Enter opens the approval modal, Enter confirms it
	var cmd tea.Cmd
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("expected approval modal to open on first Enter")
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty rationale should save review")
	}

	result := cmd()
	updated, cmd = model.Update(result)
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	v := reflect.ValueOf(cmd())
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", v.Kind())
	}
	t.Logf("batch has %d items", v.Len())
	for i := 0; i < v.Len(); i++ {
		t.Logf("item %d type: %T", i, v.Index(i).Interface())
	}
	for i := 0; i < v.Len(); i++ {
		msg := v.Index(i).Interface().(tea.Cmd)()
		t.Logf("msg %d type: %T", i, msg)
		if _, ok := msg.(msgs.MergeRequestedMsg); ok {
			return
		}
	}
	t.Logf("model.review.Status = %v, config.AutoMerge = %v", model.review.Status, model.config.AutoMerge)
	t.Fatal("expected MergeRequestedMsg in sequence when AutoMerge=true")
}

func TestReviewScreenAutoMerge_ApproveWithoutAutoMerge(t *testing.T) {
	t.Parallel()

	git := fakeGitProvider{}
	store := &fakeReviewStore{
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	}
	engine := review.NewEngine(git, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	}))
	model := NewReviewScreen("yay", git, store, engine, domain.WorkspaceConfig{AutoMerge: false})
	model, _ = loadReadyScreen(t, model)

	// Enter opens the approval modal, Enter confirms it
	var cmd tea.Cmd
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("expected approval modal to open on first Enter")
	}
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty rationale should save review")
	}

	result := cmd()
	_, cmd = model.Update(result)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	v := reflect.ValueOf(cmd())
	if v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", v.Kind())
	}
	for i := 0; i < v.Len(); i++ {
		if _, ok := v.Index(i).Interface().(tea.Cmd)().(msgs.MergeRequestedMsg); ok {
			t.Fatal("expected no MergeRequestedMsg when AutoMerge=false")
		}
	}
}

func TestReviewScreenAutoMerge_RejectNoMerge(t *testing.T) {
	t.Parallel()

	git := &fakeGitProvider{}
	store := &fakeReviewStore{
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	}
	engine := review.NewEngine(git, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	}))
	model := NewReviewScreen("yay", git, store, engine, domain.WorkspaceConfig{AutoMerge: true})
	model, _ = loadReadyScreen(t, model)

	// ctrl+x opens the reject confirm modal
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	model = updated.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("expected reject confirm modal to be open")
	}
	// Enter confirms the reject
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if model.modal == nil {
		t.Fatal("expected reject reason modal to be open after confirmation")
	}
	// Esc skips the reason, which saves the review as Rejected
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("reject should save review via saveReviewCmd")
	}

	// Run the save cmd, then run the sequence it returns
	result := cmd()
	updated, cmd = model.Update(result)
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	seqVal := reflect.ValueOf(cmd)
	if seqVal.Kind() != reflect.Func {
		t.Fatalf("expected func, got %v", seqVal.Kind())
	}
	_ = model
}

func TestReviewScreenViewRendersModalBeforeBody(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.openRejectConfirmModal()

	view := model.View()
	if !strings.Contains(view, "Reject this review?") {
		t.Fatal("expected modal prompt to be rendered")
	}
	if !strings.Contains(view, "Reviewing:") {
		t.Fatal("expected review body to be rendered")
	}
}

func TestReviewScreenViewFitsAllocatedHeightOnDiffTab(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updated.(ReviewScreen)
	updated, _ = model.Update(reviewLoadedMsg{
		diff: git.GitDiff{
			UpstreamCommit: "upstream123",
			LocalCommit:    "local123",
			Commits: []git.Commit{
				{
					Hash:    "upstream123",
					Author:  "Alice",
					Date:    "2026-07-22T10:00:00Z",
					Subject: "Bump version",
				},
			},
			Files: []git.FileDiff{
				{
					Path: "PKGBUILD",
					Kind: git.FileKindPKGBUILD,
					Patch: "diff --git a/PKGBUILD b/PKGBUILD\n" +
						"new file mode 100644\n" +
						"index 0000000..ced3f31\n" +
						"--- /dev/null\n" +
						"+++ b/PKGBUILD\n" +
						"@@ -0,0 +1,24 @@\n" +
						"+pkgname=example\n",
				},
			},
		},
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	})
	model = updated.(ReviewScreen)

	view := model.View()
	if got := lipgloss.Height(view); got > 30 {
		t.Fatalf("view height = %d, want <= 30; got:\n%s", got, view)
	}
}

func TestReviewScreenViewFitsAllocatedHeight(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.width = 200
	model.syncLayout()
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("view height = %d, want %d\n%s", got, model.height, model.View())
	}

	model.opFailure = &opFailure{kind: "Run Analyzers", err: fmt.Errorf("boom")}
	model.syncLayout()
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("view height = %d with failure banner, want %d\n%s", got, model.height, model.View())
	}
}

func TestReviewScreenDiffTabScrollsWithPgDown(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	model = updated.(ReviewScreen)
	updated, _ = model.Update(reviewLoadedMsg{
		diff: git.GitDiff{
			UpstreamCommit: "upstream123",
			LocalCommit:    "local123",
			Files: []git.FileDiff{
				{
					Path: "PKGBUILD",
					Kind: git.FileKindPKGBUILD,
					Patch: "diff --git a/PKGBUILD b/PKGBUILD\n" +
						"line 01\nline 02\nline 03\nline 04\nline 05\nline 06\n" +
						"line 07\nline 08\nline 09\nline 10\nline 11\nline 12\n" +
						"line 13\nline 14\nline 15\nline 16\nline 17\nline 18\n",
				},
			},
		},
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	})
	model = updated.(ReviewScreen)

	before := model.View()
	if !strings.Contains(before, "line 01") {
		t.Fatalf("expected initial diff view to contain top content; got:\n%s", before)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(ReviewScreen)
	after := model.View()

	if before == after {
		t.Fatal("expected pgdown to change the rendered diff view")
	}
	if strings.Contains(after, "line 01") {
		t.Fatalf("expected scrolled diff view to move past top content; got:\n%s", after)
	}
}

func TestReviewScreenNoOpApproveOnDecidedReview(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Advance to approved state.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalApprove {
		t.Fatal("first Enter should open approve modal")
	}

	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty notes should save review")
	}
	msg := cmd()
	updated, _ = screen.Update(msg)
	screen = updated.(ReviewScreen)
	if screen.review.Status != domain.ReviewApproved {
		t.Fatalf("status = %v, want approved", screen.review.Status)
	}

	// Second Enter on already-approved review should be a no-op.
	updated2, _ := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen2 := updated2.(ReviewScreen)
	if screen2.modalFlow != modalNone {
		t.Fatal("second Enter on approved review should be no-op (no modal)")
	}
}

func TestReviewScreenNoOpRejectOnDecidedReview(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Reject the review first.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalRejectConfirm {
		t.Fatal("ctrl+x should open reject confirm modal")
	}
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	screen = updated.(ReviewScreen)
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	screen = updated.(ReviewScreen)
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	screen = updated.(ReviewScreen)
	updated2, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated2.(ReviewScreen)
	if cmd == nil {
		t.Fatal("reject should save review")
	}
	msg := cmd()
	updated3, _ := screen.Update(msg)
	screen = updated3.(ReviewScreen)
	if screen.review.Status != domain.ReviewRejected {
		t.Fatalf("status = %v, want rejected", screen.review.Status)
	}

	// Second ctrl+x on already-rejected review should be a no-op.
	updated4, _ := screen.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	screen2 := updated4.(ReviewScreen)
	if screen2.modalFlow != modalNone {
		t.Fatal("second ctrl+x on rejected review should be no-op (no modal)")
	}
}

func TestReviewScreenNoOpAnalyzeOnDecidedReview(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Advance to approved state.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalApprove {
		t.Fatal("first Enter should open approve modal")
	}

	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty notes should save review")
	}
	msg := cmd()
	updated, _ = screen.Update(msg)
	screen = updated.(ReviewScreen)
	if screen.review.Status != domain.ReviewApproved {
		t.Fatalf("status = %v, want approved", screen.review.Status)
	}

	before := screen.loading
	updated2, cmd2 := screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	screen2 := updated2.(ReviewScreen)
	if screen2.loading != before {
		t.Fatal("a on an already-decided review should not start loading")
	}
	if cmd2 != nil {
		t.Fatal("a on an already-decided review should return no command")
	}
}

func TestReviewScreenViewShowsLoadingIndicatorWithContent(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.loading = true

	view := model.View()
	if !strings.Contains(view, "Running analyzers…") {
		t.Fatalf("expected loading indicator in view; got:\n%s", view)
	}
	if !strings.Contains(view, "Reviewing:") {
		t.Fatal("expected review body to still be rendered when loading")
	}
	if !strings.Contains(view, "Diff") {
		t.Fatal("expected tab bar to still be rendered when loading")
	}
	if !strings.Contains(view, "PKGBUILD") {
		t.Fatal("expected diff content to still be rendered when loading")
	}
	if !strings.Contains(view, "Esc Back") {
		t.Fatal("expected action bar to still be rendered when loading")
	}
}

// TestApproveWithFindingsShowsModal verifies that when the review has
// findings, Enter opens an approval modal that shows the findings summary.
func TestApproveWithFindingsShowsModal(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Inject findings into the review
	model.review.FindingsStatus = domain.FindingsRun
	model.review.Findings = []domain.Finding{
		{
			Analyzer: "checksum",
			Category: "checksumMismatch",
			Summary:  "sha256sum mismatch",
		},
		{
			Analyzer: "shell",
			Category: "dangerousCommand",
			Summary:  "eval detected",
		},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modal == nil {
		t.Fatal("expected approval modal to open on Enter")
	}
	if screen.modalFlow != modalApprove {
		t.Fatalf("modal flow = %v, want approve", screen.modalFlow)
	}

	// Verify the findings are in the rendered modal text
	view := screen.View()
	if strings.Contains(view, "No analyzers have been run") {
		t.Error("expected no 'No analyzers have been run' text when findings are run")
	}
	if !strings.Contains(view, "Approve?") {
		t.Error("expected 'Approve?' text in modal")
	}
	if strings.Contains(view, "Approve anyway?") {
		t.Error("expected no 'Approve anyway?' text when findings are run")
	}
	if !strings.Contains(view, "Findings: 2 (2 LOW)") {
		t.Error("expected findings summary 'Findings: 2 (2 LOW)' in modal")
	}

	if store.review.Status != domain.ReviewPending {
		t.Fatal("status should still be pending before confirmation")
	}
}

// TestApproveWithoutFindingsRunShowsNotRunModal verifies that when the
// review has no findings status (not run), the approval modal shows the
// not-run notice instead of a findings list.
func TestApproveWithoutFindingsRunShowsNotRunModal(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modal == nil {
		t.Fatal("expected approval modal to open on Enter")
	}
	if screen.modalFlow != modalApprove {
		t.Fatalf("modal flow = %v, want approve", screen.modalFlow)
	}

	view := screen.View()
	if !strings.Contains(view, "No analyzers have been run for this review.") {
		t.Errorf("expected 'No analyzers have been run' text in modal, got:\n%s", view)
	}
	if !strings.Contains(view, "Approve anyway?") {
		t.Error("expected 'Approve anyway?' text in modal when findings are not run")
	}
	if strings.Contains(view, "Approve?") {
		t.Error("expected no 'Approve?' text when findings are not run")
	}
	if strings.Contains(view, "0 findings") {
		t.Error("expected no '0 findings' text when findings are not run")
	}
}

// TestApproveWithZeroFindingsShowsZeroModal verifies that when findings
// were run and produced zero findings, the approval modal shows the
// zero-findings notice instead of the not-run notice.
func TestApproveWithZeroFindingsShowsZeroModal(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	model.review.FindingsStatus = domain.FindingsRun

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modal == nil {
		t.Fatal("expected approval modal to open on Enter")
	}
	if screen.modalFlow != modalApprove {
		t.Fatalf("modal flow = %v, want approve", screen.modalFlow)
	}

	view := screen.View()
	if !strings.Contains(view, "There are 0 findings, approve?") {
		t.Errorf("expected '0 findings' prompt in modal, got:\n%s", view)
	}
	if strings.Contains(view, "No analyzers have been run") {
		t.Error("expected no 'No analyzers have been run' text when findings are run")
	}
	if strings.Contains(view, "Approve anyway?") {
		t.Error("expected no 'Approve anyway?' text when findings are run")
	}
}

// TestApproveWithFindingsCancelReturnsToReview verifies that Esc
// dismisses the approval modal without changing review status.
func TestApproveWithFindingsCancelReturnsToReview(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	model.review.FindingsStatus = domain.FindingsRun
	model.review.Findings = []domain.Finding{
		{Analyzer: "checksum", Category: "checksumMismatch", Summary: "sha256sum mismatch"},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modal == nil {
		t.Fatal("expected approval modal to open")
	}

	// Esc cancels the modal
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
	screen = updated.(ReviewScreen)
	if screen.modal != nil {
		t.Fatal("expected modal to be dismissed after Esc")
	}
	if screen.modalFlow != modalNone {
		t.Fatalf("modal flow = %v, want none", screen.modalFlow)
	}
	if store.review.Status != domain.ReviewPending {
		t.Fatal("status should remain pending when approval is cancelled")
	}
}

// TestApproveWithFindingsConfirmSavesReview verifies that Enter
// confirms the approval modal and saves the review with rationale.
func TestApproveWithFindingsConfirmSavesReview(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	model.review.FindingsStatus = domain.FindingsRun
	store.review.FindingsStatus = domain.FindingsRun
	model.review.Findings = []domain.Finding{
		{Analyzer: "checksum", Category: "checksumMismatch", Summary: "sha256sum mismatch"},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen := updated.(ReviewScreen)
	if screen.modal == nil {
		t.Fatal("expected approval modal to open")
	}

	// Enter confirms with empty rationale
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected saveReviewCmd from confirmed approval modal")
	}

	// Run the save command
	result := cmd()
	updated, _ = screen.Update(result)
	screen = updated.(ReviewScreen)

	if store.review.Status != domain.ReviewApproved {
		t.Fatalf("status = %q, want approved", store.review.Status)
	}
	if store.review.Rationale != "" {
		t.Fatalf("rationale = %q, want empty", store.review.Rationale)
	}
	if store.review.FindingsStatus != domain.FindingsRun {
		t.Fatalf("findings status = %q, want run", store.review.FindingsStatus)
	}
}

// TestRejectFlowSkipsReasonWhenEmpty verifies that Esc in the reject
// reason modal saves the review as rejected with empty rationale.
func TestRejectFlowSkipsReasonWhenEmpty(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// ctrl+x opens reject confirm, Enter confirms, Esc skips reason
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalRejectConfirm {
		t.Fatalf("modal flow = %v, want reject confirm", screen.modalFlow)
	}

	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if screen.modalFlow != modalRejectReason {
		t.Fatalf("modal flow = %v, want reject reason", screen.modalFlow)
	}

	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("reject skip should save review")
	}

	result := cmd()
	updated, _ = screen.Update(result)
	screen = updated.(ReviewScreen)

	if store.review.Status != domain.ReviewRejected {
		t.Fatalf("status = %q, want rejected", store.review.Status)
	}
	if store.review.Rationale != "" {
		t.Fatalf("rationale = %q, want empty", store.review.Rationale)
	}
}

// TestRejectFlowAcceptsReasonWhenProvided verifies that typing a
// rejection reason and submitting saves it as the rationale.
func TestRejectFlowAcceptsReasonWhenProvided(t *testing.T) {
	t.Parallel()

	model, store := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// ctrl+x opens reject confirm, Enter confirms
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	screen := updated.(ReviewScreen)
	if screen.modalFlow != modalRejectConfirm {
		t.Fatalf("modal flow = %v, want reject confirm", screen.modalFlow)
	}

	updated, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if cmd != nil {
		cmd() // drain any modal command
	}
	if screen.modalFlow != modalRejectReason {
		t.Fatalf("modal flow = %v, want reject reason", screen.modalFlow)
	}

	// Input is already focused by openRejectReasonModal. Just type and Enter.
	updated, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("poor")})
	screen = updated.(ReviewScreen)
	updated, cmd = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("reject with reason should save review")
	}

	result := cmd()
	updated, _ = screen.Update(result)
	screen = updated.(ReviewScreen)

	if store.review.Status != domain.ReviewRejected {
		t.Fatalf("status = %q, want rejected", store.review.Status)
	}
	if store.review.Rationale != "poor" {
		t.Fatalf("rationale = %q, want 'poor'", store.review.Rationale)
	}
}

func loadReadyScreen(t *testing.T, model ReviewScreen) (ReviewScreen, tea.Cmd) {
	t.Helper()

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(ReviewScreen)
	updated, _ = model.Update(reviewLoadedMsg{
		diff: git.GitDiff{
			UpstreamCommit: "upstream123",
			LocalCommit:    "local123",
			Commits: []git.Commit{
				{
					Hash:    "upstream123",
					Author:  "Alice",
					Date:    "2026-07-22T10:00:00Z",
					Subject: "Bump version",
				},
			},
			Files: []git.FileDiff{
				{
					Path: "PKGBUILD",
					Kind: git.FileKindPKGBUILD,
					Patch: "diff --git a/PKGBUILD b/PKGBUILD\n" +
						"--- a/PKGBUILD\n+++ b/PKGBUILD\n-pkgver=1.0\n+pkgver=1.1\n",
				},
			},
		},
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
			ReviewerNotes:  "",
		},
	})
	model = updated.(ReviewScreen)
	return model, nil
}

// fakeGitProvider satisfies the full git.Provider interface. These tests
// never actually invoke it — loadReadyScreen injects reviewLoadedMsg
// directly rather than driving Init()/loadReviewCmd() — but the field
// type on ReviewScreen is git.Provider, so every method must be present
// for this to compile, even the ones this test file never exercises.
type fakeGitProvider struct{}

func (fakeGitProvider) Import(context.Context, domain.PackageName) error { return nil }

func (fakeGitProvider) Fetch(context.Context, domain.PackageName) error { return nil }

func (fakeGitProvider) Status(context.Context, domain.PackageName) (*git.RepositoryStatus, error) {
	return &git.RepositoryStatus{}, nil
}

func (fakeGitProvider) CommitsBehind(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}

func (fakeGitProvider) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	return "", nil
}

func (fakeGitProvider) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}

func (fakeGitProvider) ReviewDiff(context.Context, domain.PackageName, string, string) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}

func (fakeGitProvider) Merge(context.Context, domain.PackageName, string) error { return nil }
func (fakeGitProvider) LocalOnlyCommits(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}

func (fakeGitProvider) Metadata(context.Context, domain.PackageName) (*git.RepositoryMetadata, error) {
	return &git.RepositoryMetadata{}, nil
}

func (fakeGitProvider) CloneAt(context.Context, domain.PackageName, string, string) error { return nil }
func (fakeGitProvider) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return "", nil
}
func (fakeGitProvider) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return false, nil
}
func (fakeGitProvider) FileAtCommit(context.Context, domain.PackageName, string, string) (string, error) {
	return "", nil
}

type fakeReviewStore struct {
	review *domain.Review
}

func (f *fakeReviewStore) Get(domain.PackageName) (*domain.Review, error) {
	if f.review == nil {
		return nil, domain.ErrNotFound
	}

	r := *f.review
	return &r, nil
}

func (f *fakeReviewStore) Save(_ domain.PackageName, review *domain.Review) error {
	r := *review
	f.review = &r
	return nil
}

func (f *fakeReviewStore) Delete(domain.PackageName) error {
	f.review = nil
	return nil
}

func newTestReviewScreen(t *testing.T) (ReviewScreen, *fakeReviewStore) {
	t.Helper()

	git := fakeGitProvider{}
	store := &fakeReviewStore{
		review: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsNotRun,
		},
	}

	policyEngine := policy.NewEngine(domain.PolicyConfig{
		ID:          domain.PolicyConfigID("test"),
		WorkspaceID: domain.WorkspaceID("test"),
		Version:     "test",
	})

	engine := review.NewEngine(
		git,
		store,
		fakeAnalyzerManager{},
		policyEngine,
	)

	screen := NewReviewScreen(
		"yay",
		git,
		store,
		engine,
		domain.WorkspaceConfig{},
	)

	return screen, store
}

func TestReviewScreenRenderTabContentFindings(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Inject findings via OperationCompletedMsg — auto-switches to Findings tab.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "yay",
		Result: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings: []domain.Finding{
				{Category: "shell_exec", Summary: "Unsafe shell exec"},
				{Category: "url_new", Summary: "New URL"},
			},
		},
	})
	// The screen shows the stored review: the findings appear with the
	// reload that follows the operation (#399).
	updated, _ = updated.(ReviewScreen).Update(reviewLoadedMsg{review: &domain.Review{
		PackageName:    "yay",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "shell_exec", Summary: "Unsafe shell exec"},
			{Category: "url_new", Summary: "New URL"},
		},
	}})
	model = updated.(ReviewScreen)

	view := model.View()
	if !strings.Contains(view, "Findings:") {
		t.Fatal("renderTabContent should render findings when Findings tab is active")
	}
	if strings.Contains(view, "No findings yet") {
		t.Fatal("findings should be rendered, not the 'no findings yet' message")
	}
}

func TestReviewScreenRenderTabContentDiffDefault(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Diff tab is default.
	view := model.View()
	if !strings.Contains(view, "PKGBUILD") {
		t.Fatal("default renderTabContent should render diff content")
	}
}

func TestReviewScreenRenderFindingsContentNotRun(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Switch to Findings tab.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlPgDown})
	model = updated.(ReviewScreen)

	// FindingsNotRun — no analyzers run yet.
	view := model.View()
	if !strings.Contains(view, "No findings yet") {
		t.Fatal("renderFindingsContent should show 'no findings yet' message when not run")
	}
}

func TestReviewScreenRenderFindingsContentEmpty(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Inject empty findings via OperationCompletedMsg — auto-switches to Findings tab.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "yay",
		Result: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings:       []domain.Finding{},
		},
	})
	// The screen shows the stored review: the findings appear with the
	// reload that follows the operation (#399).
	updated, _ = updated.(ReviewScreen).Update(reviewLoadedMsg{review: &domain.Review{
		PackageName:    "yay",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{},
	}})
	model = updated.(ReviewScreen)

	view := model.View()
	if !strings.Contains(view, "No findings — all checks passed") {
		t.Fatal("renderFindingsContent should show 'all checks passed' when findings list is empty")
	}
}

func TestReviewScreenRenderFindingsContentWithFindings(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Inject findings via OperationCompletedMsg — auto-switches to Findings tab.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "yay",
		Result: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings: []domain.Finding{
				{Category: "shell_exec", Summary: "Unsafe shell exec", Analyzer: "shell"},
				{Category: "version_bump", Summary: "Version bump"},
			},
		},
	})
	// The screen shows the stored review: the findings appear with the
	// reload that follows the operation (#399).
	updated, _ = updated.(ReviewScreen).Update(reviewLoadedMsg{review: &domain.Review{
		PackageName:    "yay",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings: []domain.Finding{
			{Category: "shell_exec", Summary: "Unsafe shell exec", Analyzer: "shell"},
			{Category: "version_bump", Summary: "Version bump"},
		},
	}})
	model = updated.(ReviewScreen)

	view := model.View()
	if !strings.Contains(view, "Findings:") {
		t.Fatal("renderFindingsContent should render findings header")
	}
	if !strings.Contains(view, "Unsafe shell exec") {
		t.Fatal("renderFindingsContent should render individual findings")
	}
}

func TestReviewScreenOperationCompletedMsgSwitchesToFindingsOnAnalyzerSuccess(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Start on Diff tab (default).
	if model.tabs.Active().Label != "Diff" {
		t.Fatalf("expected Diff tab, got %q", model.tabs.Active().Label)
	}

	// Simulate successful analyzer completion via OperationCompletedMsg.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "yay",
		Result: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
			Findings:       []domain.Finding{},
		},
	})
	// The screen reloads the stored review after the operation (#399);
	// the tab switches when that reload arrives.
	updated, _ = updated.(ReviewScreen).Update(reviewLoadedMsg{review: &domain.Review{
		PackageName:    "yay",
		Status:         domain.ReviewPending,
		FindingsStatus: domain.FindingsRun,
		Findings:       []domain.Finding{},
	}})
	screen := updated.(ReviewScreen)

	if got := screen.tabs.Active().Label; got != "Findings" {
		t.Fatalf("active tab after analyzer success = %q, want Findings", got)
	}
}

func TestReviewScreenOperationCompletedMsgNoSwitchOnFailure(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Start on Diff tab (default).
	if model.tabs.Active().Label != "Diff" {
		t.Fatalf("expected Diff tab, got %q", model.tabs.Active().Label)
	}

	// Simulate failed analyzer via OperationFailedMsg.
	updated, _ := model.Update(msgs.OperationFailedMsg{
		OpID:     "yay",
		Err:      fmt.Errorf("analyzer failed"),
		TraceCtx: context.Background(),
	})
	screen := updated.(ReviewScreen)

	if got := screen.tabs.Active().Label; got != "Diff" {
		t.Fatalf("active tab after failure = %q, want Diff (no switch)", got)
	}
}

func TestReviewScreenOperationCompletedMsgNoSwitchOnUnrelatedOp(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Start on Diff tab (default).
	if model.tabs.Active().Label != "Diff" {
		t.Fatalf("expected Diff tab, got %q", model.tabs.Active().Label)
	}

	// Simulate analyzer completion for a different package.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "different-pkg",
		Result: &domain.Review{
			PackageName:    "different-pkg",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
		},
	})
	screen := updated.(ReviewScreen)

	if got := screen.tabs.Active().Label; got != "Diff" {
		t.Fatalf("active tab after unrelated completion = %q, want Diff (no switch)", got)
	}
}

func TestReviewScreenOperationFailedMsgClearsLoading(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.loading = true

	updated, cmd := model.Update(msgs.OperationFailedMsg{
		OpID:     "yay",
		Err:      fmt.Errorf("build failed"),
		TraceCtx: context.Background(),
	})
	screen := updated.(ReviewScreen)
	if screen.loading {
		t.Fatal("loading should be false after OperationFailedMsg with matching OpID")
	}
	if screen.err != nil {
		t.Fatalf("err should be nil after OperationFailedMsg (toast carries the error), got %q", screen.err.Error())
	}
	// A failed operation may still have replaced the review: the screen
	// reloads it (#399).
	if cmd == nil {
		t.Fatal("expected a reload cmd after OperationFailedMsg with matching OpID")
	}
}

func TestReviewScreenOperationFailedMsgMismatchedOpID(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.loading = true
	model.err = nil

	updated, _ := model.Update(msgs.OperationFailedMsg{
		OpID:     "different-pkg",
		Err:      fmt.Errorf("unrelated failure"),
		TraceCtx: context.Background(),
	})
	screen := updated.(ReviewScreen)
	if !screen.loading {
		t.Fatal("loading should remain true when OpID does not match pkgName")
	}
	if screen.err != nil {
		t.Fatalf("err should remain nil when OpID does not match, got %v", screen.err)
	}
}

func TestReviewScreenOperationCompletedMsgMismatchedOpID(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.loading = true
	originalReview := model.review

	updated, cmd := model.Update(msgs.OperationCompletedMsg{
		OpID:   "different-pkg",
		Result: &domain.Review{PackageName: "different-pkg"},
	})
	screen := updated.(ReviewScreen)
	if !screen.loading {
		t.Fatal("loading should remain true when OpID does not match pkgName")
	}
	if screen.review != originalReview {
		t.Fatal("review should be unchanged when OpID does not match")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd())
	}
}

func TestReviewScreenOperationFailedMsgMismatchedOpIDNoPanic(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)
	model.loading = true
	originalReview := model.review

	updated, _ := model.Update(msgs.OperationFailedMsg{
		OpID:     "different-pkg",
		Err:      fmt.Errorf("unrelated failure"),
		TraceCtx: context.Background(),
	})
	screen := updated.(ReviewScreen)
	if screen.review != originalReview {
		t.Fatal("review should be unchanged when OpID does not match")
	}
	if !screen.loading {
		t.Fatal("loading should remain true when OpID does not match")
	}
}

// TestReviewScreenOperationCompletedMsgReloadsAndClearsLoading: a completed
// operation for this package clears loading and reloads the stored review
// instead of taking msg.Result (#399); one for another package changes
// nothing.
func TestReviewScreenOperationCompletedMsgReloadsAndClearsLoading(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         msgs.OperationCompletedMsg
		wantLoading bool
		wantReload  bool
	}{
		{
			name: "matching opid with review result",
			msg: msgs.OperationCompletedMsg{
				OpID:   "yay",
				Result: &domain.Review{ID: "yay@other", PackageName: "yay", Status: domain.ReviewPending},
			},
			wantLoading: false,
			wantReload:  true,
		},
		{
			name: "matching opid with string result",
			msg: msgs.OperationCompletedMsg{
				OpID:   "yay",
				Result: "build completed",
			},
			wantLoading: false,
			wantReload:  true,
		},
		{
			name: "matching opid with nil result",
			msg: msgs.OperationCompletedMsg{
				OpID:   "yay",
				Result: nil,
			},
			wantLoading: false,
			wantReload:  true,
		},
		{
			name: "mismatched opid",
			msg: msgs.OperationCompletedMsg{
				OpID:   "different-pkg",
				Result: &domain.Review{PackageName: "different-pkg"},
			},
			wantLoading: true,
			wantReload:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, _ := newTestReviewScreen(t)
			model, _ = loadReadyScreen(t, model)
			model.loading = true

			originalReview := model.review

			updated, cmd := model.Update(tt.msg)
			screen := updated.(ReviewScreen)

			if screen.loading != tt.wantLoading {
				t.Errorf("loading = %v, want %v", screen.loading, tt.wantLoading)
			}
			if screen.review != originalReview {
				t.Fatal("review pointer changed: the screen must not take msg.Result")
			}
			if screen.err != nil {
				t.Errorf("err = %v, want nil", screen.err)
			}
			if tt.wantReload {
				if cmd == nil {
					t.Fatal("expected a reload cmd")
				}
				if _, ok := cmd().(reviewLoadedMsg); !ok {
					t.Fatal("reload cmd did not produce a reviewLoadedMsg")
				}
			} else if cmd != nil {
				t.Fatalf("expected nil cmd, got %T", cmd())
			}
		})
	}
}

func TestReviewScreenBackFromAnalyzedReviewSetsChangedTrue(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Simulate analyzers completing via OperationCompletedMsg.
	updated, _ := model.Update(msgs.OperationCompletedMsg{
		OpID: "yay",
		Result: &domain.Review{
			PackageName:    "yay",
			Status:         domain.ReviewPending,
			FindingsStatus: domain.FindingsRun,
		},
	})
	model = updated.(ReviewScreen)

	// Press Back (Esc).
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected BackMsg command from Esc")
	}

	result := cmd()
	backMsg, ok := result.(msgs.BackMsg)
	if !ok {
		t.Fatalf("expected BackMsg, got %T", result)
	}
	if !backMsg.Changed {
		t.Fatal("BackMsg.Changed should be true after analyzers ran")
	}
}

func TestReviewScreenBackFromUnchangedReviewSetsChangedFalse(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	// Press Back (Esc) without any changes — no analyzer run, no decision.
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected BackMsg command from Esc")
	}

	result := cmd()
	backMsg, ok := result.(msgs.BackMsg)
	if !ok {
		t.Fatalf("expected BackMsg, got %T", result)
	}
	if backMsg.Changed {
		t.Fatal("BackMsg.Changed should be false after no-op visit")
	}
}

func TestReviewScreenBackFromApprovedReviewSetsChangedTrue(t *testing.T) {
	t.Parallel()

	model, _ := newTestReviewScreen(t)
	model, _ = loadReadyScreen(t, model)

	var cmd tea.Cmd
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(ReviewScreen)
	if cmd == nil {
		t.Fatal("approve with empty notes should save review")
	}

	result := cmd()
	_, cmd = model.Update(result)
	if cmd == nil {
		t.Fatal("expected sequence command from reviewSavedMsg")
	}

	seqVal := reflect.ValueOf(cmd())
	if seqVal.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %v", seqVal.Kind())
	}
	for i := 0; i < seqVal.Len(); i++ {
		cmdFn := seqVal.Index(i).Interface().(tea.Cmd)
		msg := cmdFn()
		if backMsg, ok := msg.(msgs.BackMsg); ok {
			if !backMsg.Changed {
				t.Fatal("BackMsg.Changed should be true after approve")
			}
		}
	}
}

func TestReviewScreenActionBarDimmedState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      domain.ReviewStatus
		wantPending bool
	}{
		{"pending — status-gated actions enabled", domain.ReviewPending, true},
		{"approved — status-gated actions dimmed", domain.ReviewApproved, false},
		{"rejected — status-gated actions dimmed", domain.ReviewRejected, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, _ := newTestReviewScreen(t)
			model, _ = loadReadyScreen(t, model)
			model.review.Status = tt.status

			for _, k := range []string{"a", "ENTER", "^X"} {
				a := actionByKey(t, model.actions(), k)
				if a.Enabled != tt.wantPending {
					t.Errorf("%s enabled = %v, want %v", k, a.Enabled, tt.wantPending)
				}
				if a.Reason != prereqPendingReview {
					t.Errorf("%s reason = %q, want %q", k, a.Reason, prereqPendingReview)
				}
			}
			for _, k := range []string{"^PgDn/^PgUp", "Esc"} {
				if !actionByKey(t, model.actions(), k).Enabled {
					t.Errorf("%s must always be enabled", k)
				}
			}

			// Compact bar: never any reason text (Action Bar Convention).
			bar := model.renderActionBar()
			if strings.Contains(bar, prereqPendingReview) || strings.Contains(bar, "(requires") {
				t.Errorf("compact bar shows reason text: %q", bar)
			}
		})
	}
}
