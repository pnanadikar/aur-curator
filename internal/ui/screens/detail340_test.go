package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #340 — Package Details offers Confirm and Publish with the same
// availability as the Dashboard (workflow.md § Dashboard / Package
// Details): Confirm in Built, Publish in Confirmed.

// detailAt returns a loaded Package Details for an approved, merged
// package whose last success is ls and whose HEAD is head.
// It starts from the constructor's state, so key bindings are real.
func detailAt(ls *domain.Build, head string) PackageDetailModel {
	m := NewPackageDetailModel(deps.Dependencies{})
	m.pkgName = "yay"
	m.width, m.height = 200, 40
	m.review = &domain.Review{Status: domain.ReviewApproved}
	m.isMerged = true
	m.headCommit = head
	m.lastSuccessBuild = ls
	return m
}

func TestDetailConfirmPublishAvailability(t *testing.T) {
	built := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}
	confirmed := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A", Confirmed: true}
	published := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A", Confirmed: true, Published: true}
	cases := []struct {
		name             string
		ls               *domain.Build
		head             string
		confirm, publish bool
	}{
		{"merged, never built", nil, "A", false, false},
		{"built at HEAD", built, "A", true, false},
		{"confirmed at HEAD", confirmed, "A", false, true},
		{"published at HEAD", published, "A", false, false},
		{"confirmed, HEAD moved", confirmed, "B", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := detailAt(tc.ls, tc.head)
			if got := m.isConfirmEnabled(); got != tc.confirm {
				t.Errorf("Confirm enabled = %v, want %v", got, tc.confirm)
			}
			if got := m.isPublishEnabled(); got != tc.publish {
				t.Errorf("Publish enabled = %v, want %v", got, tc.publish)
			}
			if got := actionByKey(t, m.actions(), "C").Enabled; got != tc.confirm {
				t.Errorf("action bar Confirm enabled = %v, want %v", got, tc.confirm)
			}
			if got := actionByKey(t, m.actions(), "P").Enabled; got != tc.publish {
				t.Errorf("action bar Publish enabled = %v, want %v", got, tc.publish)
			}
		})
	}
}

func TestDetailConfirmPublishKeys(t *testing.T) {
	built := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}
	confirmed := &domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A", Confirmed: true}
	cases := []struct {
		name string
		ls   *domain.Build
		key  string
		want string // "confirm", "publish", or "" for nothing
	}{
		{"c in Built confirms", built, "c", "confirm"},
		{"p in Built does nothing", built, "p", ""},
		{"p in Confirmed publishes", confirmed, "p", "publish"},
		{"c in Confirmed does nothing", confirmed, "c", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := detailAt(tc.ls, "A")
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			var got string
			if cmd != nil {
				switch msg := cmd().(type) {
				case msgs.ConfirmRequestedMsg:
					got = "confirm"
					if msg.Pkg != "yay" || msg.TraceCtx == nil {
						t.Fatalf("ConfirmRequestedMsg = %+v", msg)
					}
				case msgs.PublishRequestedMsg:
					got = "publish"
					if msg.Pkg != "yay" || msg.TraceCtx == nil {
						t.Fatalf("PublishRequestedMsg = %+v", msg)
					}
				}
			}
			if got != tc.want {
				t.Fatalf("key %q → %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}
