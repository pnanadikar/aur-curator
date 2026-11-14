package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/pkgname"
)

// The command line (#511): one table of commands, flags and environment
// variables. Dispatch, the usage message, the --help synopsis and the fish
// completion (contrib/completions) are all built from it, and tests check
// README.md and internal/help/help.md against it, so none can drift from
// what aur-curator accepts.

// appName is the program name shown in usage, help and completion.
const appName = "aur-curator"

// tuiSummary describes running with a config file and no command.
const tuiSummary = "start the TUI"

// cliEnv is what a command runs with: the loaded config and the services
// built from it, and the command's argument ("" if it takes none).
type cliEnv struct {
	cfg        domain.WorkspaceConfig
	configPath string
	catalog    catalog.PackageCatalog
	provider   git.Provider
	forge      git.Forge
	arg        string
}

// packageArg is the placeholder of an argument that is a package name;
// parseArgs checks it against the AUR's rule (#504).
const packageArg = "<package-name>"

// command is one CLI command: aur-curator [flags] <config-file> <name> [<arg>].
type command struct {
	name     string
	arg      string // the argument's placeholder, e.g. "<package-name>"; "" if none
	summary  string
	packages bool // the argument is a curated package (fish completes it)
	run      func(cliEnv)
}

// commands lists every command, in the order help shows them.
var commands = []command{
	{name: "import", arg: packageArg, summary: "curate a new package",
		run: func(e cliEnv) { runImport(e.cfg, e.configPath, e.catalog, e.provider, e.arg) }},
	{name: "update", summary: "check curated packages against the AUR",
		run: func(e cliEnv) { runUpdate(e.cfg, e.configPath, e.catalog, e.provider) }},
	{name: "outdated", summary: "list packages the AUR has something different for",
		run: func(e cliEnv) { runOutdated(e.cfg, e.catalog, e.provider) }},
	{name: "check", summary: "run consistency checks",
		run: func(e cliEnv) { runCheck(e.cfg, e.configPath, e.catalog, e.provider) }},
	{name: "remove", arg: packageArg, summary: "stop curating a package", packages: true,
		run: func(e cliEnv) { runRemove(e.cfg, e.configPath, e.catalog, e.provider, e.forge, e.arg) }},
}

// cliFlag is one flag. Every flag is accepted with two dashes or one
// (--trace, -trace), and a value either as the next argument or after "=".
type cliFlag struct {
	name    string
	short   string // one-letter alias, "" if none
	value   string // the value's placeholder, e.g. "<path>"; "" for a switch
	summary string
}

// flags lists every flag, in the order help shows them.
var flags = []cliFlag{
	{name: "debug-log", value: "<path>", summary: "write debug logs to <path>"},
	{name: "trace", value: "<path>", summary: "write trace logs to <path>"},
	{name: "help", short: "h", summary: "show this summary; --help=full shows the full guide"},
	{name: "version", summary: "print the version"},
}

// helpFullValue is the one value --help takes: --help=full prints the guide.
const helpFullValue = "full"

// envVar is one environment variable aur-curator reads.
type envVar struct {
	name    string
	summary string
}

// envVars lists the environment variables, in the order help shows them.
var envVars = []envVar{
	{name: "AUR_CURATOR_DEBUG", summary: "debug logs to stderr when --debug-log is not given"},
	{name: "AUR_CURATOR_TRACE", summary: "trace logs to <workspace>/.aur-curator/trace.log when --trace is not given"},
}

// helpMode is what --help asked for.
type helpMode int

const (
	helpNone  helpMode = iota
	helpShort          // --help, -h: the synopsis
	helpFull           // --help=full: the whole guide
)

// invocation is a parsed command line.
type invocation struct {
	help       helpMode
	version    bool
	debugLog   string
	trace      string
	configPath string
	cmd        *command // nil: the TUI
	arg        string
}

// errUsage marks a command line aur-curator cannot run; main prints it
// with the usage lines and exits 2.
var errUsage = errors.New("usage")

func usageErrorf(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errUsage}, a...)...)
}

// parseArgs parses the arguments after the program name, without reading
// the config. --help and --version win wherever they appear, even on an
// otherwise wrong line (the first one given counts); otherwise the first
// problem is returned as an errUsage error.
func parseArgs(args []string) (invocation, error) {
	var (
		inv        invocation
		firstErr   error
		positional []string
	)
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
		f := lookupFlag(name)
		if f == nil {
			fail(usageErrorf("unknown flag %q", a))
			continue
		}
		switch f.name {
		case "help":
			if inv.help != helpNone || inv.version {
				continue
			}
			switch {
			case !hasValue:
				inv.help = helpShort
			case value == helpFullValue:
				inv.help = helpFull
			default:
				fail(usageErrorf("unknown --help value %q (only --help=%s)", value, helpFullValue))
			}
			continue
		case "version":
			if hasValue {
				fail(usageErrorf("--version takes no value"))
				continue
			}
			if inv.help == helpNone {
				inv.version = true
			}
			continue
		}
		// A flag with a value.
		if !hasValue {
			if i+1 < len(args) {
				i++
				value = args[i]
			}
		}
		if value == "" {
			fail(usageErrorf("--%s needs %s", f.name, f.value))
			continue
		}
		switch f.name {
		case "debug-log":
			inv.debugLog = value
		case "trace":
			inv.trace = value
		}
	}
	if inv.help != helpNone || inv.version {
		return inv, nil
	}
	if firstErr != nil {
		return inv, firstErr
	}
	if len(positional) == 0 {
		return inv, usageErrorf("no config file given")
	}
	inv.configPath = positional[0]
	if len(positional) == 1 {
		return inv, nil
	}
	cmd := lookupCommand(positional[1])
	if cmd == nil {
		return inv, usageErrorf("unknown command %q", positional[1])
	}
	rest := positional[2:]
	switch {
	case cmd.arg == "" && len(rest) > 0:
		return inv, usageErrorf("%s takes no arguments", cmd.name)
	case cmd.arg != "" && len(rest) == 0:
		return inv, usageErrorf("%s needs %s", cmd.name, cmd.arg)
	case cmd.arg != "" && len(rest) > 1:
		return inv, usageErrorf("%s takes one %s", cmd.name, cmd.arg)
	}
	if cmd.arg == packageArg {
		if err := pkgname.Check(rest[0]); err != nil {
			return inv, usageErrorf("%s: %v", cmd.name, err)
		}
	}
	inv.cmd = cmd
	if cmd.arg != "" {
		inv.arg = rest[0]
	}
	return inv, nil
}

func lookupFlag(name string) *cliFlag {
	for i := range flags {
		if flags[i].name == name || (flags[i].short != "" && flags[i].short == name) {
			return &flags[i]
		}
	}
	return nil
}

func lookupCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// usageLines returns one usage line per way to run aur-curator: the TUI,
// then each command. With summaries, each line ends with its summary,
// aligned (README.md § Usage); without, the lines are bare (help.md § Usage
// and the usage message).
func usageLines(withSummary bool) []string {
	type line struct{ text, summary string }
	prefix := appName + " [flags] <config-file>"
	lines := []line{{prefix, tuiSummary}}
	for _, c := range commands {
		text := prefix + " " + c.name
		if c.arg != "" {
			text += " " + c.arg
		}
		lines = append(lines, line{text, c.summary})
	}
	width := 0
	for _, l := range lines {
		width = max(width, len(l.text))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
		if withSummary {
			out[i] = fmt.Sprintf("%-*s  %s", width, l.text, l.summary)
		}
	}
	return out
}

// printUsageError writes err, the usage lines and where to find more to w.
func printUsageError(w io.Writer, err error) {
	msg := strings.TrimPrefix(err.Error(), errUsage.Error()+": ")
	_, _ = fmt.Fprintf(w, "%s: %s\n", appName, msg)
	for i, l := range usageLines(false) {
		lead := "       "
		if i == 0 {
			lead = "usage: "
		}
		_, _ = fmt.Fprintln(w, lead+l)
	}
	_, _ = fmt.Fprintf(w, "Run '%s --help' for commands and flags.\n", appName)
}

// synopsisWidth is the width the synopsis is laid out for.
const synopsisWidth = 80

// synopsis is what --help prints: usage, commands, flags and environment
// variables, laid out for an 80-column terminal.
func synopsis() string {
	var b strings.Builder
	b.WriteString(appName + " — curate AUR packages through a review-gated lifecycle\n\n")

	b.WriteString("Usage:\n")
	writeRows(&b, [][2]string{
		{appName + " [flags] <config-file>", tuiSummary},
		{appName + " [flags] <config-file> <command> [<arg>]", "run one command and exit"},
	})

	b.WriteString("\nCommands:\n")
	rows := make([][2]string, 0, len(commands))
	for _, c := range commands {
		rows = append(rows, [2]string{strings.TrimSpace(c.name + " " + c.arg), c.summary})
	}
	writeRows(&b, rows)

	b.WriteString("\nFlags:\n")
	rows = rows[:0]
	for _, f := range flags {
		left := "--" + f.name
		if f.value != "" {
			left += " " + f.value
		}
		if f.short != "" {
			left = "-" + f.short + ", " + left
		}
		rows = append(rows, [2]string{left, f.summary})
	}
	writeRows(&b, rows)
	b.WriteString("  A single dash works too: -trace <path>.\n")

	b.WriteString("\nEnvironment:\n")
	rows = rows[:0]
	for _, e := range envVars {
		rows = append(rows, [2]string{e.name, e.summary})
	}
	writeRows(&b, rows)

	b.WriteString("\nFull guide: " + appName + " --help=" + helpFullValue + ", or ? in the TUI.\n")
	return b.String()
}

// writeRows writes two-column rows indented by two spaces, the second
// column aligned and word-wrapped to synopsisWidth.
func writeRows(b *strings.Builder, rows [][2]string) {
	left := 0
	for _, r := range rows {
		left = max(left, len(r[0]))
	}
	indent := 2 + left + 2
	for _, r := range rows {
		lines := wrapWords(r[1], synopsisWidth-indent)
		fmt.Fprintf(b, "  %-*s  %s\n", left, r[0], lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(b, "%s%s\n", strings.Repeat(" ", indent), l)
		}
	}
}

// wrapWords splits s into lines of at most width runes, at spaces. A word
// longer than width gets a line of its own.
func wrapWords(s string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	return append(lines, cur)
}
