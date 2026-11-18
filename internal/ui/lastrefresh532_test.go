package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/ui/screens"
)

// refreshShown returns what the header shows after "Last Refresh: ".
func refreshShown(t *testing.T, a App) string {
	t.Helper()
	h := ansi.Strip(a.viewHeader())
	_, after, ok := strings.Cut(h, "Last Refresh: ")
	if !ok {
		t.Fatalf("header has no Last Refresh:\n%s", h)
	}
	return strings.TrimSpace(after)
}

func newApp532(t *testing.T) App {
	t.Helper()
	d := Dependencies{}
	d.Config.RootPath = t.TempDir()
	return NewApp(d)
}

func update532(t *testing.T, a App, msg tea.Msg) App {
	t.Helper()
	m, _ := a.Update(msg)
	return m.(App)
}

// The header's Last Refresh is the Dashboard's last successful load
// (#532): "—" before one, unchanged by navigation, by R alone and by a
// failed load.
func TestLastRefreshNoneBeforeFirstLoad532(t *testing.T) {
	if got := refreshShown(t, newApp532(t)); got != "—" {
		t.Fatalf("before any load: got %q, want —", got)
	}
}

func TestLastRefreshNotSetByNavigation532(t *testing.T) {
	a := newApp532(t)
	a = update532(t, a, msgs.NavigateMsg{To: msgs.ScreenRepositoryStatus, FromMenu: true})
	a = update532(t, a, msgs.NavigateMsg{To: msgs.ScreenDashboard, FromMenu: true})
	if got := refreshShown(t, a); got != "—" {
		t.Fatalf("after navigating to the Dashboard, no load: got %q, want —", got)
	}
}

func TestLastRefreshNotSetByRAlone532(t *testing.T) {
	a := newApp532(t)
	// The binding is r; the action bar labels it R.
	a = update532(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if got := refreshShown(t, a); got != "—" {
		t.Fatalf("after R, before its load returns: got %q, want —", got)
	}
}

func TestLastRefreshNotSetByFailedLoad532(t *testing.T) {
	a := newApp532(t)
	a = update532(t, a, screens.PackagesLoadedMsg{Err: errors.New("catalog unreadable"), TraceCtx: context.Background()})
	if got := refreshShown(t, a); got != "—" {
		t.Fatalf("after a failed load: got %q, want —", got)
	}
}

func TestLastRefreshSetBySuccessfulLoad532(t *testing.T) {
	a := newApp532(t)
	before := time.Now()
	a = update532(t, a, screens.PackagesLoadedMsg{Packages: []domain.PackageName{"pkg-a"}, TraceCtx: context.Background()})
	after := time.Now()
	got := refreshShown(t, a)
	if got != before.Format("15:04") && got != after.Format("15:04") {
		t.Fatalf("after a successful load: got %q, want %s", got, before.Format("15:04"))
	}
	// Navigating away and back keeps it.
	a = update532(t, a, msgs.NavigateMsg{To: msgs.ScreenHelp, FromMenu: true})
	a = update532(t, a, msgs.NavigateMsg{To: msgs.ScreenDashboard, FromMenu: true})
	if again := refreshShown(t, a); again != got {
		t.Fatalf("after navigating back: got %q, want %q", again, got)
	}
}
