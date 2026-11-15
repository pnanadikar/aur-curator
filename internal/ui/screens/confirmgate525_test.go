package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/inspect"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// confirmDeps is a workspace with a last success b2 of yay holding two
// artifacts (and an older b1 artifact, which Confirm ignores), and a fake
// inspector.
func confirmDeps(t *testing.T, fi *fakeInspector) deps.Dependencies {
	t.Helper()
	root := t.TempDir()
	paths := store.NewStorePaths(root)
	bs, as := jsonstore.NewBuildStore(paths), jsonstore.NewArtifactStore(paths)
	started := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	if err := bs.SaveLastSuccess("yay", &domain.Build{ID: "b2", PackageName: "yay", Status: domain.BuildSuccess, ReviewedCommit: "a1b2c3d4e5f6", StartTime: started}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []domain.Artifact{
		{ID: "a1", BuildID: "b2", PackageName: "yay", Filename: "yay-1-1-x86_64.pkg.tar.zst", Checksum: "c0ffee"},
		{ID: "a2", BuildID: "b2", PackageName: "yay", Filename: "yay-debug-1-1-x86_64.pkg.tar.zst"},
		{ID: "a0", BuildID: "b1", PackageName: "yay", Filename: "yay-0-1-x86_64.pkg.tar.zst"},
	} {
		if err := as.Save(a.ID, &a); err != nil {
			t.Fatal(err)
		}
	}
	return deps.Dependencies{
		BuildEngine: build.NewEngine(nil, bs, as, nil, paths),
		Inspector:   fi,
		Config:      domain.WorkspaceConfig{RootPath: root},
	}
}

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// startConfirm runs Start and delivers the inspection; it returns the
// gate with its modal open.
func startConfirm(t *testing.T, d deps.Dependencies) ConfirmGate {
	t.Helper()
	g := NewConfirmGate(d)
	g.SetWidth(200)
	g, cmd := g.Start("yay", false)
	if !g.ModalOpen() || !strings.Contains(ansi.Strip(g.View()), "Inspecting yay…") {
		t.Fatalf("no inspecting notice: %q", ansi.Strip(g.View()))
	}
	msg := cmd()
	g, _, handled := g.Update(msg)
	if !handled || !g.ModalOpen() || g.stage != confirmGateModal {
		t.Fatalf("modal not open after inspection: %+v", g)
	}
	return g
}

func TestConfirmGateModal(t *testing.T) {
	fi := &fakeInspector{reports: map[string]inspect.Report{"yay-1-1-x86_64.pkg.tar.zst": setidReport()}}
	g := startConfirm(t, confirmDeps(t, fi))
	view := ansi.Strip(g.View())
	for _, want := range []string{
		"Confirm yay?",
		"Build a1b2c3d4 · 15 Nov 2026 10:00",
		"yay-1-1-x86_64.pkg.tar.zst",
		"sha256 c0ffee",
		"setuid / setgid (1)",
		"-rwsr-xr-x root root  opt/x/sandbox",
		"yay-debug-1-1-x86_64.pkg.tar.zst",
		"Nothing with special power.",
		"Has an install script (.INSTALL): v to read it.",
		"Full list: a to go to the Artifacts tab.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("modal lacks %q:\n%s", want, view)
		}
	}
	// "Nothing with special power." is set apart under its own artifact.
	if !strings.Contains(modalText(view), "yay-debug-1-1-x86_64.pkg.tar.zst\n\nNothing with special power.") {
		t.Errorf("no blank line before the nothing line:\n%s", modalText(view))
	}
	if strings.Contains(view, "yay-0-1") {
		t.Errorf("an older build's artifact in the modal:\n%s", view)
	}
	// Only the build's own artifacts were inspected.
	if strings.Join(fi.calls, ",") != "b2/yay-1-1-x86_64.pkg.tar.zst,b2/yay-debug-1-1-x86_64.pkg.tar.zst" {
		t.Errorf("inspected %q", fi.calls)
	}
}

// TestConfirmGateModalPaths: 8 paths in all, across artifacts, then
// "… and N more".
func TestConfirmGateModalPaths(t *testing.T) {
	var many []inspect.Entry
	for i := 0; i < 6; i++ {
		many = append(many, inspect.Entry{Path: "etc/f" + string(rune('a'+i)), Type: "file", Mode: 0o644})
	}
	r := inspect.Classify(many)
	fi := &fakeInspector{reports: map[string]inspect.Report{"yay-1-1-x86_64.pkg.tar.zst": r, "yay-debug-1-1-x86_64.pkg.tar.zst": r}}
	view := ansi.Strip(startConfirm(t, confirmDeps(t, fi)).View())
	if n := strings.Count(view, "etc/f"); n != 8 {
		t.Errorf("%d paths shown, want 8:\n%s", n, view)
	}
	if !strings.Contains(view, "… and 4 more") {
		t.Errorf("no more line:\n%s", view)
	}
}

func TestConfirmGateKeys(t *testing.T) {
	fi := &fakeInspector{reports: map[string]inspect.Report{"yay-1-1-x86_64.pkg.tar.zst": setidReport()}, script: "post_install() { printf '\x1b[2J'; }\n"}
	d := confirmDeps(t, fi)

	t.Run("y confirms the inspected build", func(t *testing.T) {
		g, cmd, _ := startConfirm(t, d).Update(runeKey("y"))
		if g.ModalOpen() || cmd == nil {
			t.Fatal("y did not end the flow")
		}
		msg, ok := cmd().(msgs.ConfirmRequestedMsg)
		if !ok || msg.Pkg != "yay" || msg.BuildID != "b2" || msg.TraceCtx == nil {
			t.Fatalf("y → %+v", msg)
		}
	})
	for _, k := range []tea.KeyMsg{runeKey("n"), {Type: tea.KeyEsc}} {
		g, cmd, _ := startConfirm(t, d).Update(k)
		if g.ModalOpen() {
			t.Errorf("%s left the modal open", k)
		}
		if cmd != nil {
			if _, ok := cmd().(msgs.ConfirmRequestedMsg); ok {
				t.Errorf("%s confirmed", k)
			}
		}
	}
	t.Run("a cancels and asks for the Artifacts tab", func(t *testing.T) {
		g, cmd, _ := startConfirm(t, d).Update(runeKey("a"))
		if pkg, wanted := g.ArtifactsWanted(); !wanted || pkg != "yay" || g.ModalOpen() || cmd != nil {
			t.Fatalf("a → wanted %v %q, open %v, cmd %v", wanted, pkg, g.ModalOpen(), cmd != nil)
		}
	})
	t.Run("v reads the install script, escaped, and keeps the modal", func(t *testing.T) {
		g, cmd, _ := startConfirm(t, d).Update(runeKey("v"))
		if !g.ModalOpen() || cmd == nil {
			t.Fatal("v closed the modal or did nothing")
		}
		m, ok := cmd().(confirmGateInstallMsg)
		if !ok || !strings.Contains(m.content, "== yay-1-1-x86_64.pkg.tar.zst: .INSTALL ==") || !strings.Contains(m.content, `printf '\x1b[2J'`) || strings.Contains(m.content, "\x1b") {
			t.Fatalf("install content %q", m.content)
		}
		if fi.calls[len(fi.calls)-1] != "install b2/yay-1-1-x86_64.pkg.tar.zst" {
			t.Errorf("calls %q", fi.calls)
		}
	})
	t.Run("v without an install script does nothing", func(t *testing.T) {
		g := startConfirm(t, confirmDeps(t, &fakeInspector{}))
		g, cmd, _ := g.Update(runeKey("v"))
		if !g.ModalOpen() || cmd != nil {
			t.Fatal("v acted without an install script")
		}
	})
	t.Run("Esc while inspecting abandons the run", func(t *testing.T) {
		g := NewConfirmGate(d)
		g, cmd := g.Start("yay", false)
		g, _, _ = g.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if g.ModalOpen() {
			t.Fatal("still open")
		}
		g, _, handled := g.Update(cmd())
		if !handled || g.ModalOpen() {
			t.Fatal("a stale inspection reopened the modal")
		}
	})
}

// TestConfirmGateInspectionFails: a failed inspection is shown and y
// still confirms (information, not a gate).
func TestConfirmGateInspectionFails(t *testing.T) {
	fi := &fakeInspector{errs: map[string]error{"yay-1-1-x86_64.pkg.tar.zst": errFake("bsdtar failed: exit status 1")}}
	g := startConfirm(t, confirmDeps(t, fi))
	if !strings.Contains(ansi.Strip(g.View()), "⚠ cannot inspect: bsdtar failed: exit status 1") {
		t.Errorf("modal:\n%s", ansi.Strip(g.View()))
	}
	_, cmd, _ := g.Update(runeKey("y"))
	if msg, ok := cmd().(msgs.ConfirmRequestedMsg); !ok || msg.BuildID != "b2" {
		t.Fatalf("y after a failed inspection → %+v", msg)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

// TestDashboardConfirmArtifacts: `a` on the Dashboard opens Package
// Details on the Artifacts tab.
func TestDashboardConfirmArtifacts(t *testing.T) {
	m := DashboardModel{confirmGate: startConfirm(t, confirmDeps(t, &fakeInspector{}))}
	_, cmd := m.Update(runeKey("a"))
	if cmd == nil {
		t.Fatal("no command")
	}
	var nav msgs.NavigateMsg
	found := false
	for _, msg := range flatten(cmd) {
		if n, ok := msg.(msgs.NavigateMsg); ok {
			nav, found = n, true
		}
	}
	if !found || nav.To != msgs.ScreenPackageDetail || nav.Package != "yay" || !nav.ShowArtifacts {
		t.Fatalf("navigate %+v (found %v)", nav, found)
	}
}

// TestDetailShowArtifacts: PackageSelectedMsg.ShowArtifacts switches to
// the Artifacts tab once the package has loaded; `a` on Package Details
// switches straight away.
func TestDetailShowArtifacts(t *testing.T) {
	m := NewPackageDetailModel(deps.Dependencies{})
	m, _ = m.Update(PackageSelectedMsg{Pkg: "yay", ShowArtifacts: true})
	if m.tab != TabSummary {
		t.Fatalf("tab %v before the load", m.tab)
	}
	m, cmd := m.Update(detailLoadedMsg{})
	if m.tab != TabArtifactHistory || !m.artifactsLoading || cmd == nil {
		t.Fatalf("after load: tab %v loading %v cmd %v", m.tab, m.artifactsLoading, cmd != nil)
	}

	d := detailAt(&domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}, "A")
	d.confirmGate = startConfirm(t, confirmDeps(t, &fakeInspector{}))
	d, cmd = d.Update(runeKey("a"))
	if d.tab != TabArtifactHistory || d.confirmGate.ModalOpen() || cmd == nil {
		t.Fatalf("a: tab %v open %v", d.tab, d.confirmGate.ModalOpen())
	}
}

// flatten runs cmd and any tea.Batch inside it.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flatten(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// modalText is a rendered modal's text, one line per line, without the
// border and padding.
func modalText(view string) string {
	var out []string
	for _, l := range strings.Split(view, "\n") {
		l = strings.TrimSpace(l)
		l = strings.TrimSpace(strings.Trim(l, "│╭╮╰╯─"))
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// TestConfirmOnArtifactsTab: Confirm pressed on the Artifacts tab, which
// already shows the full list, offers no `a`; elsewhere it does.
func TestConfirmOnArtifactsTab(t *testing.T) {
	d := confirmDeps(t, &fakeInspector{})
	for _, tc := range []struct {
		tab    DetailTab
		offers bool
	}{{TabArtifactHistory, false}, {TabSummary, true}} {
		m := detailAt(&domain.Build{Status: domain.BuildSuccess, ReviewedCommit: "A"}, "A")
		m.deps = d
		m.confirmGate = NewConfirmGate(d)
		m.confirmGate.SetWidth(200)
		m.tab = tc.tab
		m, cmd := m.Update(runeKey("c"))
		m, _ = m.Update(cmd())
		if got := strings.Contains(ansi.Strip(m.confirmGate.View()), "Full list: a to go to the Artifacts tab."); got != tc.offers {
			t.Errorf("tab %d: a line shown %v, want %v", tc.tab, got, tc.offers)
		}
		m, _ = m.Update(runeKey("a"))
		if open := m.confirmGate.ModalOpen(); open == tc.offers {
			t.Errorf("tab %d: after a, modal open %v", tc.tab, open)
		}
	}
}
