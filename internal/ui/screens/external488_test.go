package screens

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// fakeExec replaces execProcess: it records the command and the callback
// instead of handing over the terminal. Not parallel-safe (a package var).
type fakeExec struct {
	cmd *exec.Cmd
	fn  tea.ExecCallback
}

func useFakeExec(t *testing.T) *fakeExec {
	t.Helper()
	f := &fakeExec{}
	old := execProcess
	execProcess = func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		f.cmd, f.fn = c, fn
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { execProcess = old })
	return f
}

// TestRunToolKeepsStderrAndCleansUp: stderr goes to a writer of runTool's
// own (kept for the toast), and cleanup runs on success and on failure
// (#488).
func TestRunToolKeepsStderrAndCleansUp(t *testing.T) {
	for _, runErr := range []error{nil, errors.New("exit status 128")} {
		f := useFakeExec(t)
		cleaned := false
		runTool("Diff tool", "foo", exec.Command("git", "difftool"), func() { cleaned = true })

		if f.cmd == nil || f.cmd.Stderr == nil {
			t.Fatal("no command, or its Stderr left for ExecProcess to set")
		}
		_, _ = f.cmd.Stderr.Write([]byte("This message is displayed because 'diff.tool' is not configured.\nNo known diff tool is available.\n"))

		msg, ok := f.fn(runErr).(msgs.ToolDoneMsg)
		if !ok {
			t.Fatalf("callback returned %T, want msgs.ToolDoneMsg", f.fn(runErr))
		}
		if !cleaned {
			t.Errorf("err %v: cleanup did not run", runErr)
		}
		if msg.What != "Diff tool" || msg.Pkg != "foo" || !errors.Is(msg.Err, runErr) {
			t.Errorf("msg = %+v", msg)
		}
		if !strings.Contains(msg.Stderr, "No known diff tool is available.") {
			t.Errorf("Stderr = %q", msg.Stderr)
		}
	}
}

// TestTailBufferKeepsTheEnd: only the last stderrTail bytes are kept.
func TestTailBufferKeepsTheEnd(t *testing.T) {
	var b tailBuffer
	_, _ = b.Write([]byte(strings.Repeat("x", stderrTail)))
	_, _ = b.Write([]byte("the end"))
	got := b.String()
	if len(got) != stderrTail || !strings.HasSuffix(got, "the end") {
		t.Errorf("len %d, suffix %q", len(got), got[len(got)-10:])
	}
}

// TestPagerCmd: $PAGER split on whitespace without a shell, less -R when
// unset or blank; LESSOPEN and LESSCLOSE removed from its environment.
func TestPagerCmd(t *testing.T) {
	t.Setenv("LESSOPEN", "|lesspipe.sh %s")
	t.Setenv("LESSCLOSE", "lesspipe.sh %s %s")
	t.Setenv("KEEP_ME", "1")

	for _, tt := range []struct {
		pager string
		want  []string
	}{
		{"", []string{"less", "-R", "/tmp/f.diff"}},
		{"   ", []string{"less", "-R", "/tmp/f.diff"}},
		{"bat --paging=always", []string{"bat", "--paging=always", "/tmp/f.diff"}},
		{"less", []string{"less", "/tmp/f.diff"}},
	} {
		t.Setenv("PAGER", tt.pager)
		cmd := pagerCmd("/tmp/f.diff")
		if !reflect.DeepEqual(cmd.Args, tt.want) {
			t.Errorf("PAGER=%q: Args = %q, want %q", tt.pager, cmd.Args, tt.want)
		}
		env := strings.Join(cmd.Env, "\n")
		if strings.Contains(env, "LESSOPEN=") || strings.Contains(env, "LESSCLOSE=") {
			t.Errorf("PAGER=%q: LESSOPEN or LESSCLOSE passed to the pager", tt.pager)
		}
		if !strings.Contains(env, "KEEP_ME=1") {
			t.Errorf("PAGER=%q: the rest of the environment was dropped", tt.pager)
		}
	}
}

// TestOpenPagerWritesPlainPrivateFileAndRemovesIt: the file holds the
// content without ANSI styling, the #491 escapes as text; mode 0600; the
// name ends with the tab's name; removed once the pager exits.
func TestOpenPagerWritesPlainPrivateFileAndRemovesIt(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PAGER", "")
	f := useFakeExec(t)

	styled := "\x1b[32m+curl x | sh\x1b[0m \x1b[31m\\x1b\x1b[0m[2K"
	openPager("foo", pagerName("Diff", true), styled)

	path := f.cmd.Args[len(f.cmd.Args)-1]
	if !strings.HasPrefix(path, os.Getenv("TMPDIR")) || !strings.HasSuffix(path, "-diff.diff") ||
		!strings.Contains(path, "aur-curator-foo-") {
		t.Errorf("path = %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if want := `+curl x | sh \x1b[2K`; string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}

	f.fn(nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file still there after the pager exited: %v", err)
	}
}

func TestPagerName(t *testing.T) {
	for _, tt := range []struct {
		tab  string
		diff bool
		want string
	}{
		{"Diff", true, "diff.diff"},
		{".SRCINFO", true, "srcinfo.diff"},
		{"Build Log", false, "build-log.txt"},
		{"Findings", false, "findings.txt"},
		{"", false, "content.txt"},
	} {
		if got := pagerName(tt.tab, tt.diff); got != tt.want {
			t.Errorf("pagerName(%q, %v) = %q, want %q", tt.tab, tt.diff, got, tt.want)
		}
	}
}
