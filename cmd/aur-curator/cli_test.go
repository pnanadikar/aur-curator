package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// repoRoot is the repository root, relative to this package's directory.
const repoRoot = "../.."

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    invocation
		wantCmd string // "" for the TUI
		wantErr string // "" for none
	}{
		{name: "TUI", args: []string{"c.json"}, want: invocation{configPath: "c.json"}},
		{name: "import", args: []string{"c.json", "import", "foo"}, want: invocation{configPath: "c.json", arg: "foo"}, wantCmd: "import"},
		{name: "remove", args: []string{"c.json", "remove", "foo"}, want: invocation{configPath: "c.json", arg: "foo"}, wantCmd: "remove"},
		{name: "check", args: []string{"c.json", "check"}, want: invocation{configPath: "c.json"}, wantCmd: "check"},
		{name: "update", args: []string{"c.json", "update"}, want: invocation{configPath: "c.json"}, wantCmd: "update"},
		{name: "outdated", args: []string{"c.json", "outdated"}, want: invocation{configPath: "c.json"}, wantCmd: "outdated"},

		{name: "trace space", args: []string{"--trace", "t.log", "c.json"}, want: invocation{configPath: "c.json", trace: "t.log"}},
		{name: "trace equals", args: []string{"--trace=t.log", "c.json"}, want: invocation{configPath: "c.json", trace: "t.log"}},
		{name: "trace one dash", args: []string{"-trace", "t.log", "c.json"}, want: invocation{configPath: "c.json", trace: "t.log"}},
		{name: "trace one dash equals", args: []string{"-trace=t.log", "c.json"}, want: invocation{configPath: "c.json", trace: "t.log"}},
		{name: "debug-log after command", args: []string{"c.json", "check", "--debug-log", "d.log"}, want: invocation{configPath: "c.json", debugLog: "d.log"}, wantCmd: "check"},

		{name: "help", args: []string{"--help"}, want: invocation{help: helpShort}},
		{name: "h", args: []string{"-h"}, want: invocation{help: helpShort}},
		{name: "help full", args: []string{"--help=full"}, want: invocation{help: helpFull}},
		{name: "help anywhere", args: []string{"c.json", "remove", "--help"}, want: invocation{help: helpShort}},
		{name: "help beats a bad line", args: []string{"--tarce", "c.json", "nope", "-h"}, want: invocation{help: helpShort}},
		{name: "version", args: []string{"--version"}, want: invocation{version: true}},
		{name: "version one dash", args: []string{"-version"}, want: invocation{version: true}},
		{name: "first of help and version wins", args: []string{"--version", "--help"}, want: invocation{version: true}},

		{name: "no args", args: nil, wantErr: "no config file given"},
		{name: "flags only", args: []string{"--trace", "t.log"}, wantErr: "no config file given"},
		{name: "unknown command", args: []string{"c.json", "foo"}, wantErr: `unknown command "foo"`},
		{name: "missing argument", args: []string{"c.json", "import"}, wantErr: "import needs <package-name>"},
		{name: "extra argument", args: []string{"c.json", "check", "x"}, wantErr: "check takes no arguments"},
		{name: "two arguments", args: []string{"c.json", "remove", "a", "b"}, wantErr: "remove takes one <package-name>"},
		{name: "unknown flag", args: []string{"--tarce", "x", "c.json"}, wantErr: `unknown flag "--tarce"`},
		{name: "unknown short flag", args: []string{"c.json", "-x"}, wantErr: `unknown flag "-x"`},
		{name: "double dash alone", args: []string{"--", "c.json"}, wantErr: `unknown flag "--"`},
		{name: "trace without value", args: []string{"c.json", "--trace"}, wantErr: "--trace needs <path>"},
		{name: "trace empty", args: []string{"--trace=", "c.json"}, wantErr: "--trace needs <path>"},
		{name: "help bad value", args: []string{"--help=all"}, wantErr: `unknown --help value "all" (only --help=full)`},
		{name: "import invalid name", args: []string{"c.json", "import", "Foo"}, wantErr: `import: invalid package name "Foo": the AUR allows lowercase letters, digits and . + _ -, starting with a letter or digit`},
		{name: "remove path", args: []string{"c.json", "remove", "../x"}, wantErr: `remove: invalid package name "../x": the AUR allows lowercase letters, digits and . + _ -, starting with a letter or digit`},
		{name: "remove dot", args: []string{"c.json", "remove", "."}, wantErr: `remove: invalid package name ".": the AUR allows lowercase letters, digits and . + _ -, starting with a letter or digit`},
		{name: "version with value", args: []string{"--version=1"}, wantErr: "--version takes no value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if !errors.Is(err, errUsage) {
					t.Fatalf("err = %v, want a usage error", err)
				}
				if msg := strings.TrimPrefix(err.Error(), "usage: "); msg != tt.wantErr {
					t.Fatalf("err = %q, want %q", msg, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			gotCmd := ""
			if got.cmd != nil {
				gotCmd = got.cmd.name
			}
			if gotCmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", gotCmd, tt.wantCmd)
			}
			got.cmd = nil
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Every command in the table can run, and parses as itself.
func TestCommandsRunnable(t *testing.T) {
	for _, c := range commands {
		if c.run == nil {
			t.Errorf("%s: no run function", c.name)
		}
		args := []string{"c.json", c.name}
		if c.arg != "" {
			args = append(args, "x")
		}
		inv, err := parseArgs(args)
		if err != nil || inv.cmd == nil || inv.cmd.name != c.name {
			t.Errorf("%s: parseArgs(%q) = %+v, %v", c.name, args, inv, err)
		}
	}
}

func TestPrintUsageError(t *testing.T) {
	var b bytes.Buffer
	printUsageError(&b, usageErrorf("unknown command %q", "foo"))
	want := `aur-curator: unknown command "foo"
usage: aur-curator [flags] <config-file>
       aur-curator [flags] <config-file> import <package-name>
       aur-curator [flags] <config-file> update
       aur-curator [flags] <config-file> outdated
       aur-curator [flags] <config-file> check
       aur-curator [flags] <config-file> remove <package-name>
Run 'aur-curator --help' for commands and flags.
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// The synopsis fits a terminal (#511): at most 40 lines of 80 columns.
func TestSynopsisFits(t *testing.T) {
	s := synopsis()
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) > 40 {
		t.Errorf("%d lines, want at most 40", len(lines))
	}
	for _, l := range lines {
		// 80, not synopsisWidth: a literal, so widening the layout fails.
		if n := utf8.RuneCountInString(l); n > 80 {
			t.Errorf("%d columns, want at most 80: %q", n, l)
		}
	}
}

// Literal lines, so the layout is pinned by more than the table it is
// built from.
func TestSynopsisLiteral(t *testing.T) {
	s := synopsis()
	for _, want := range []string{
		"\n  import <package-name>  curate a new package\n",
		"\n  -h, --help          show this summary; --help=full shows the full guide\n",
		"\n  AUR_CURATOR_TRACE  trace logs to <workspace>/.aur-curator/trace.log when\n                     --trace is not given\n",
		"\nFull guide: aur-curator --help=full, or ? in the TUI.\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("synopsis lacks %q:\n%s", want, s)
		}
	}
}

func TestSynopsisNamesEverything(t *testing.T) {
	s := synopsis()
	for _, c := range commands {
		if !strings.Contains(s, "\n  "+c.name) {
			t.Errorf("command %s missing", c.name)
		}
	}
	for _, f := range flags {
		if !strings.Contains(s, "--"+f.name) {
			t.Errorf("flag --%s missing", f.name)
		}
	}
	for _, e := range envVars {
		if !strings.Contains(s, e.name) {
			t.Errorf("variable %s missing", e.name)
		}
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("aa bb cc dddddd", 5)
	want := []string{"aa bb", "cc", "dddddd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

// section returns the text of the Markdown section headed heading (e.g.
// "## Usage"), up to the next heading of the same or a higher level.
func section(t *testing.T, doc, heading string) string {
	t.Helper()
	level := strings.Index(heading, " ")
	var out []string
	in := false
	for _, l := range strings.Split(doc, "\n") {
		if in && strings.HasPrefix(l, "#") {
			if n := strings.Index(l, " "); n > 0 && n <= level && strings.Trim(l[:n], "#") == "" {
				break
			}
		}
		if l == heading {
			in = true
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	if !in {
		t.Fatalf("no %q section", heading)
	}
	return strings.Join(out, "\n")
}

// firstCodeBlock returns the lines of the first fenced code block in s.
func firstCodeBlock(t *testing.T, s string) []string {
	t.Helper()
	start := strings.Index(s, "```")
	if start < 0 {
		t.Fatal("no code block")
	}
	body := s[start:]
	body = body[strings.Index(body, "\n")+1:]
	end := strings.Index(body, "```")
	if end < 0 {
		t.Fatal("unterminated code block")
	}
	return strings.Split(strings.TrimSuffix(body[:end], "\n"), "\n")
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// README.md § Usage and help.md § Usage show the table's usage lines.
func TestUsageLinesInDocs(t *testing.T) {
	for _, tt := range []struct {
		doc         string
		withSummary bool
	}{
		{"README.md", true},
		{"internal/help/help.md", false},
	} {
		got := firstCodeBlock(t, section(t, readDoc(t, tt.doc), "## Usage"))
		want := usageLines(tt.withSummary)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s § Usage:\n%s\nwant:\n%s", tt.doc, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// help.md describes every command, flag and environment variable.
func TestHelpGuideNamesEverything(t *testing.T) {
	doc := readDoc(t, "internal/help/help.md")
	cmds := section(t, doc, "## Commands")
	for _, c := range commands {
		if !strings.Contains(cmds, "**`"+c.name) {
			t.Errorf("help.md § Commands lacks %s", c.name)
		}
	}
	fl := section(t, doc, "## Flags")
	for _, f := range flags {
		if !strings.Contains(fl, "`--"+f.name) {
			t.Errorf("help.md § Flags lacks --%s", f.name)
		}
	}
	if !strings.Contains(fl, "`--help="+helpFullValue+"`") {
		t.Errorf("help.md § Flags lacks --help=%s", helpFullValue)
	}
	env := section(t, doc, "## Environment variables")
	for _, e := range envVars {
		if !strings.Contains(env, "`"+e.name+"`") {
			t.Errorf("help.md § Environment variables lacks %s", e.name)
		}
	}
}
