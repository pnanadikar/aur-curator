package screens

import (
	"context"
	"errors"
	"testing"

	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// #331 — the Available column and the AUR Update card show "—"/a count
// only for a completed, successful check; every other state says so.

func TestFormatAvailable(t *testing.T) {
	same := aurinfo.VersionPair{Pkg: "yay", AURVersion: "1.1-1", LocalVersion: "1.1-1"}
	newer := aurinfo.VersionPair{Pkg: "yay", AURVersion: "1.2-1", LocalVersion: "1.1-1", AURStale: true}
	cases := []struct {
		name    string
		checked bool
		err     error
		pair    aurinfo.VersionPair
		found   bool
		want    string
	}{
		{"not loaded yet", false, nil, aurinfo.VersionPair{}, false, "…"},
		{"whole check failed", true, errors.New("timeout"), newer, true, "?"},
		{"package missing from results", true, nil, aurinfo.VersionPair{}, false, "?"},
		{"not on AUR", true, nil, aurinfo.VersionPair{Pkg: "yay", LocalVersion: "1.0-1", AURUnknown: true}, true, "not on AUR"},
		{"local version unknown", true, nil, aurinfo.VersionPair{Pkg: "yay", AURVersion: "1.0-1", LocalUnknown: true}, true, "?"},
		{"newer on AUR: only the AUR version (#428)", true, nil, newer, true, "1.2-1"},
		{"nothing newer", true, nil, same, true, "—"},
		{"newer, long version: whole, the column cuts it", true, nil,
			aurinfo.VersionPair{Pkg: "edge", AURVersion: "155.0.4283.21-1", LocalVersion: "155.0.4283.13-1", AURStale: true},
			true, "155.0.4283.21-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatAvailable(tc.checked, tc.err, tc.pair, tc.found); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAURUpdateMsgStates drives the handler: a failed check makes every
// row and the card unknown and drops the previous check's staleness; a
// later successful check restores real values.
func TestAURUpdateMsgStates(t *testing.T) {
	ctx := context.Background()
	newer := aurinfo.VersionPair{Pkg: "yay", AURVersion: "1.2-1", LocalVersion: "1.1-1", AURStale: true}

	// Same initial map the constructor creates, so the test checks the
	// reset itself rather than tripping over a nil map.
	m := DashboardModel{aurStaleByPkg: make(map[domain.PackageName]bool)}
	if got := m.availableFor("yay"); got != "…" {
		t.Fatalf("before any check: %q, want …", got)
	}

	m, _ = m.Update(msgs.AURUpdateMsg{Pairs: []aurinfo.VersionPair{newer}, TraceCtx: ctx})
	if got := m.availableFor("yay"); got != "1.2-1" {
		t.Fatalf("after success: %q", got)
	}
	if !m.aurStaleByPkg["yay"] || m.summary[CardAUR].Unknown {
		t.Fatalf("after success: stale=%v cardUnknown=%v", m.aurStaleByPkg["yay"], m.summary[CardAUR].Unknown)
	}

	m, _ = m.Update(msgs.AURUpdateMsg{Err: errors.New("timeout"), TraceCtx: ctx})
	if got := m.availableFor("yay"); got != "?" {
		t.Fatalf("after failure: %q, want ?", got)
	}
	if m.aurStaleByPkg["yay"] {
		t.Fatal("after failure: the previous check's staleness must be dropped")
	}
	if !m.summary[CardAUR].Unknown {
		t.Fatal("after failure: the AUR Update card must be unknown")
	}

	m, _ = m.Update(msgs.AURUpdateMsg{Pairs: []aurinfo.VersionPair{newer}, TraceCtx: ctx})
	if got := m.availableFor("yay"); got != "1.2-1" || m.summary[CardAUR].Unknown {
		t.Fatalf("after recovery: %q, cardUnknown=%v", got, m.summary[CardAUR].Unknown)
	}
}
