package screens

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/inspect"
)

// fakeInspector returns a report or an error per file name, and the
// install script; it records the calls.
type fakeInspector struct {
	reports map[string]inspect.Report
	errs    map[string]error
	script  string
	calls   []string
}

func (f *fakeInspector) Inspect(_ context.Context, id domain.BuildID, file string) (inspect.Report, error) {
	f.calls = append(f.calls, string(id)+"/"+file)
	return f.reports[file], f.errs[file]
}

func (f *fakeInspector) InstallScript(_ context.Context, id domain.BuildID, file string) (string, error) {
	f.calls = append(f.calls, "install "+string(id)+"/"+file)
	return f.script, f.errs["install"]
}

func setidReport() inspect.Report {
	return inspect.Classify([]inspect.Entry{
		{Path: ".INSTALL", Type: "file", Mode: 0o644},
		{Path: "opt/x/sandbox", Type: "file", Mode: 0o4755, Uname: "root", Gname: "root"},
		{Path: "etc/x.conf", Type: "file", Mode: 0o644},
		{Path: "usr/lib/systemd/system/x.service", Type: "link", Mode: 0o777, Link: "/dev/null"},
		{Path: "usr/share/x", Type: "dir", Mode: 0o755, UID: 1000, GID: 1000},
	})
}

func TestModeString(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		mode uint32
		want string
	}{
		{"file", 0o4755, "-rwsr-xr-x"},
		{"file", 0o2755, "-rwxr-sr-x"},
		{"file", 0o4644, "-rwSr--r--"},
		{"file", 0o440, "-r--r-----"},
		{"dir", 0o1777, "drwxrwxrwt"},
		{"dir", 0o1776, "drwxrwxrwT"},
		{"link", 0o777, "lrwxrwxrwx"},
		{"char", 0o600, "crw-------"},
	} {
		if got := modeString(inspect.Entry{Type: tc.typ, Mode: tc.mode}); got != tc.want {
			t.Errorf("%s %o = %s, want %s", tc.typ, tc.mode, got, tc.want)
		}
	}
}

func TestEntryLine(t *testing.T) {
	if got := entryLine(inspect.CatSetID, inspect.Entry{Path: "opt/x/sandbox", Type: "file", Mode: 0o4755, Uname: "root", Gname: "root"}); got != "-rwsr-xr-x root root  opt/x/sandbox" {
		t.Errorf("setuid line %q", got)
	}
	if got := entryLine(inspect.CatNotRoot, inspect.Entry{Path: "usr/share/x", Type: "dir", Mode: 0o755, UID: 1000, GID: 50}); got != "drwxr-xr-x 1000 50  usr/share/x" {
		t.Errorf("not-root line %q", got)
	}
	if got := entryLine(inspect.CatUnit, inspect.Entry{Path: "etc/systemd/system/a.wants/x.service", Type: "link", Link: "/usr/lib/systemd/system/x.service"}); got != "etc/systemd/system/a.wants/x.service -> /usr/lib/systemd/system/x.service" {
		t.Errorf("link line %q", got)
	}
	// Control characters and newlines are shown, not acted on (#491).
	if got := entryLine(inspect.CatEtc, inspect.Entry{Path: "etc/a\x1b[2Jb\nc", Type: "file"}); got != `etc/a\x1b[2Jb\nc` {
		t.Errorf("escaped line %q", got)
	}
}

func TestInspectionLines(t *testing.T) {
	got := ansi.Strip(strings.Join(inspectionLines(artifactInspection{report: setidReport()}, "     "), "\n"))
	want := strings.Join([]string{
		"     setuid / setgid (1)",
		"       -rwsr-xr-x root root  opt/x/sandbox",
		"     not owned by root (1)",
		"       drwxr-xr-x 1000 1000  usr/share/x",
		"     install script (1)",
		"       .INSTALL",
		"     systemd unit (1)",
		"       usr/lib/systemd/system/x.service -> /dev/null",
		"     under /etc (1)",
		"       etc/x.conf",
	}, "\n")
	if got != want {
		t.Errorf("lines:\n%s\nwant:\n%s", got, want)
	}
	if got := ansi.Strip(strings.Join(inspectionLines(artifactInspection{}, "  "), "")); got != "  Nothing with special power." {
		t.Errorf("empty %q", got)
	}
	if got := ansi.Strip(strings.Join(inspectionLines(artifactInspection{err: errors.New("bsdtar failed: x")}, "  "), "")); got != "  ⚠ cannot inspect: bsdtar failed: x" {
		t.Errorf("error %q", got)
	}
}

func TestInspectionSummary(t *testing.T) {
	lines, shown, more := inspectionSummary(artifactInspection{report: setidReport()}, 3)
	want := []string{
		"setuid / setgid (1)",
		"  -rwsr-xr-x root root  opt/x/sandbox",
		"not owned by root (1)",
		"  drwxr-xr-x 1000 1000  usr/share/x",
		"install script (1)",
		"  .INSTALL",
		"systemd unit (1)",
		"under /etc (1)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") || shown != 3 || more != 2 {
		t.Errorf("summary (more %d):\n%s", more, strings.Join(lines, "\n"))
	}
	if l, s, m := inspectionSummary(artifactInspection{}, 8); len(l) != 1 || l[0] != "Nothing with special power." || s != 0 || m != 0 {
		t.Errorf("empty %q %d %d", l, s, m)
	}
}

// TestInspectArtifacts: every artifact inspected with its own build ID,
// a failure kept with its artifact and logged, the rest still inspected.
func TestInspectArtifacts(t *testing.T) {
	root := t.TempDir()
	fi := &fakeInspector{
		reports: map[string]inspect.Report{"b.pkg": setidReport()},
		errs:    map[string]error{"a.pkg": errors.New("bsdtar failed: boom")},
	}
	d := deps.Dependencies{Inspector: fi, Config: domain.WorkspaceConfig{RootPath: root}}
	got := inspectArtifacts(context.Background(), d, "x", []domain.Artifact{{BuildID: "b1", Filename: "a.pkg"}, {BuildID: "b1", Filename: "b.pkg"}})
	if len(got) != 2 || got[0].err == nil || got[1].err != nil || got[1].report.Empty() {
		t.Fatalf("inspections %+v", got)
	}
	if strings.Join(fi.calls, ",") != "b1/a.pkg,b1/b.pkg" {
		t.Errorf("calls %q", fi.calls)
	}
	log, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil || !strings.Contains(string(log), "x: a.pkg: bsdtar failed: boom") {
		t.Errorf("error log %q %v", log, err)
	}
	if inspectArtifacts(context.Background(), deps.Dependencies{}, "x", []domain.Artifact{{}}) != nil {
		t.Error("inspections without an Inspector")
	}
}

// TestArtifactsTabInspection: each artifact's inspection under its lines
// (tui-package-details.md § Artifacts).
func TestArtifactsTabInspection(t *testing.T) {
	arts := []domain.Artifact{{Filename: "a-1-1-any.pkg.tar.zst", Architecture: "any", Version: "1-1", Checksum: "abc"}, {Filename: "b-1-1-any.pkg.tar.zst", Architecture: "any", Version: "1-1"}}
	view := ansi.Strip(renderArtifactsView(false, arts, nil, "", []artifactInspection{{report: setidReport()}, {}}))
	for _, want := range []string{
		"  1. a-1-1-any.pkg.tar.zst (any) 1-1\n     Checksum: abc\n     setuid / setgid (1)\n       -rwsr-xr-x root root  opt/x/sandbox\n",
		"  2. b-1-1-any.pkg.tar.zst (any) 1-1\n     Nothing with special power.\n",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
}

// TestArtifactsLoadInspects: the Artifacts tab's load inspects the
// artifacts it read, in the same tea.Cmd (#525).
func TestArtifactsLoadInspects(t *testing.T) {
	fi := &fakeInspector{reports: map[string]inspect.Report{"yay-1-1-x86_64.pkg.tar.zst": setidReport()}}
	m := NewPackageDetailModel(confirmDeps(t, fi))
	m.pkgName = "yay"
	msg, ok := m.loadArtifactHistoryCmd()().(artifactLoadedMsg)
	if !ok || len(msg.artifacts) != 3 || len(msg.inspections) != 3 {
		t.Fatalf("load %+v", msg)
	}
	found := false
	for i, a := range msg.artifacts {
		if a.Filename == "yay-1-1-x86_64.pkg.tar.zst" && !msg.inspections[i].report.Empty() {
			found = true
		}
	}
	if !found || len(fi.calls) != 3 {
		t.Errorf("inspections %+v, calls %q", msg.inspections, fi.calls)
	}
}
