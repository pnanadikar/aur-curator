package screens

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/lifecycle"
)

// #462 — a read the Dashboard derives a package's state from is written to
// the error log when it fails. It used to be traced or discarded, so a
// merged package could show Approved with nothing recorded anywhere.

// readErrorLog returns the workspace's errors.log, or "" if there is none.
func readErrorLog(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// TestLogReadFailure: a real error is logged with what was read and the
// package; nil and "not found" (no review, no build yet) are not failures.
func TestLogReadFailure(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantLog bool
	}{
		{"nil", nil, false},
		{"not found", domain.ErrNotFound, false},
		{"wrapped not found", fmt.Errorf("get review: %w", domain.ErrNotFound), false},
		{"real error", errors.New("disk on fire"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			m := DashboardModel{deps: deps.Dependencies{Config: domain.WorkspaceConfig{RootPath: root}}}

			m.logReadFailure(context.Background(), "yay", "merge state", tc.err)

			log := readErrorLog(t, root)
			if !tc.wantLog {
				if log != "" {
					t.Fatalf("expected no error log, got %q", log)
				}
				return
			}
			for _, want := range []string{"[state]", `read merge state of "yay"`, "disk on fire"} {
				if !strings.Contains(log, want) {
					t.Errorf("error log %q does not contain %q", log, want)
				}
			}
		})
	}
}

// failingGit462 is a mockGitProvider whose merge check and HEAD read fail.
type failingGit462 struct {
	*mockGitProvider
}

func (failingGit462) IsMerged(_ context.Context, _ domain.PackageName, _ string) (bool, error) {
	return false, errors.New("merge check broke")
}

func (failingGit462) LocalHeadCommit(_ context.Context, _ domain.PackageName) (string, error) {
	return "", errors.New("head read broke")
}

// TestUpdateFromReviewLogsFailedReads: after a review decision, a failed
// merge check and a failed HEAD read are logged; the state is still
// derived fail-closed (not merged, so Approved).
func TestUpdateFromReviewLogsFailedReads(t *testing.T) {
	root := t.TempDir()
	m := DashboardModel{
		lifecycle:    make(map[domain.PackageName]lifecycle.State),
		reviewInputs: make(map[domain.PackageName]reviewInputs),
		reviewsByPkg: make(map[domain.PackageName]*domain.Review),
		updates:      make(map[domain.PackageName]int),
		deps: deps.Dependencies{
			Git:    failingGit462{&mockGitProvider{}},
			Policy: testPolicyEngine(t),
			Config: domain.WorkspaceConfig{RootPath: root},
		},
	}
	review := &domain.Review{Status: domain.ReviewApproved, UpstreamCommit: "reviewed"}

	m, _ = m.UpdateFromReview(context.Background(), "yay", review)

	if got := m.lifecycle["yay"]; got != lifecycle.Approved {
		t.Errorf("lifecycle = %s, want %s (fail-closed)", got, lifecycle.Approved)
	}
	log := readErrorLog(t, root)
	for _, want := range []string{"merge check broke", "head read broke"} {
		if !strings.Contains(log, want) {
			t.Errorf("error log %q does not contain %q", log, want)
		}
	}
}
