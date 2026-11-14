package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite contrib/completions/aur-curator.fish from fishCompletion")

// The checked-in fish completion is fishCompletion's output (#511).
func TestFishCompletion(t *testing.T) {
	path := filepath.Join(repoRoot, fishCompletionPath)
	want := fishCompletion()
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil { //nolint:gosec // a checked-in source file
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date: go test ./cmd/aur-curator -run TestFishCompletion -update", fishCompletionPath)
	}
}

func TestFishCompletionLiteral(t *testing.T) {
	s := fishCompletion()
	for _, want := range []string{
		"            case --debug-log -debug-log --trace -trace\n",
		"complete -c aur-curator -s h -l help -f -a full -d 'show this summary; --help=full shows the full guide'\n",
		"complete -c aur-curator -l trace -r -F -d 'write trace logs to <path>'\n",
		"complete -c aur-curator -n '__aur_curator_at 2' -a import -d 'curate a new package'\n",
		"complete -c aur-curator -n '__aur_curator_arg_of remove' -a '(__aur_curator_packages)'\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("completion lacks %q", want)
		}
	}
	if strings.Contains(s, "__aur_curator_arg_of import") {
		t.Error("import's argument is an AUR name: no package completion")
	}
}

func TestFishQuote(t *testing.T) {
	if got, want := fishQuote(`it's a\b`), `'it\'s a\\b'`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
