package screens

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

// execProcess hands the terminal to a command (tea.ExecProcess); tests
// replace it to capture the command instead of running it.
var execProcess = tea.ExecProcess

// stderrTail is how much of a tool's stderr is kept for the toast and the
// error log.
const stderrTail = 4096

// tailBuffer keeps the last stderrTail bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTail; over > 0 {
		t.buf = append([]byte(nil), t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// runTool hands the terminal to cmd until it exits (tui-review.md § Your
// Own Tools, #488). Its stderr still reaches the terminal and is also
// kept: ExecProcess keeps a Stderr already set, and the redraw would wipe
// out git's message. cleanup, if set, runs when the tool has exited,
// whatever the outcome. The result is a msgs.ToolDoneMsg for App.
func runTool(what string, pkg domain.PackageName, cmd *exec.Cmd, cleanup func()) tea.Cmd {
	tail := &tailBuffer{}
	cmd.Stderr = io.MultiWriter(os.Stderr, tail)
	return execProcess(cmd, func(err error) tea.Msg {
		if cleanup != nil {
			cleanup()
		}
		return msgs.ToolDoneMsg{What: what, Pkg: pkg, Err: err, Stderr: tail.String()}
	})
}

// toolFailed is a ToolDoneMsg for a tool that never got the terminal.
func toolFailed(what string, pkg domain.PackageName, err error) tea.Cmd {
	return func() tea.Msg { return msgs.ToolDoneMsg{What: what, Pkg: pkg, Err: err} }
}

// pagerCmd returns the pager command for path: $PAGER split on
// whitespace, without a shell, or "less -R" when it is unset or blank. The
// pager runs without LESSOPEN and LESSCLOSE: lesspipe would run a program
// on the hostile file, which is plain text and needs no filter (#488).
func pagerCmd(path string) *exec.Cmd {
	args := strings.Fields(os.Getenv("PAGER"))
	if len(args) == 0 {
		args = []string{"less", "-R"}
	}
	cmd := exec.Command(args[0], append(args[1:], path)...) //nolint:gosec // $PAGER is the user's own choice
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LESSOPEN=") || strings.HasPrefix(kv, "LESSCLOSE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	return cmd
}

// unsafeNameChars are replaced in a temp file's name.
var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// openPager writes content, as the screen shows it but without colour, to
// a private temp file and opens it in the pager; the file is removed when
// the pager exits. name ends the file's name: "<tab>.diff" on a diff tab,
// so a pager such as bat highlights it, "<tab>.txt" otherwise. Escapes
// from sanitize.ForReview (#491) are text, so they stay visible.
func openPager(pkg domain.PackageName, name, content string) tea.Cmd {
	const what = "Pager"
	pattern := "aur-curator-" + unsafeNameChars.ReplaceAllString(string(pkg), "-") +
		"-*-" + unsafeNameChars.ReplaceAllString(name, "-")
	f, err := os.CreateTemp("", pattern) // mode 0600
	if err != nil {
		return toolFailed(what, pkg, err)
	}
	path := f.Name()
	_, werr := f.WriteString(ansi.Strip(content))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return toolFailed(what, pkg, werr)
	}
	return runTool(what, pkg, pagerCmd(path), func() { _ = os.Remove(path) })
}

// pagerName is the temp file's name ending for a tab: its label made safe
// for a file name, with .diff when the tab shows a diff.
func pagerName(tab string, diff bool) string {
	name := strings.ToLower(strings.Trim(unsafeNameChars.ReplaceAllString(tab, "-"), "-."))
	if name == "" {
		name = "content"
	}
	if diff {
		return name + ".diff"
	}
	return name + ".txt"
}
