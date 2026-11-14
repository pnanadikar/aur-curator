package main

import (
	"fmt"
	"strings"
)

// fishCompletionPath is the checked-in fish completion, relative to the
// repository root; `make install` installs it (#511).
const fishCompletionPath = "contrib/completions/" + appName + ".fish"

// fishCompletion returns the fish completion script for the commands and
// flags in cli.go. contrib/completions/aur-curator.fish is its output; a
// test fails when the two differ.
func fishCompletion() string {
	var valueFlags []string
	for _, f := range flags {
		if f.value != "" {
			valueFlags = append(valueFlags, "--"+f.name, "-"+f.name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `# fish completion for %[1]s (#511).
# Generated from cmd/%[1]s/cli.go: do not edit. To regenerate:
#   go test ./cmd/%[1]s -run TestFishCompletion -update

# The arguments typed before the current word, without flags and their values.
function __%[2]s_args
    set -l skip 0
    for t in (commandline -opc)[2..-1]
        if test $skip = 1
            set skip 0
            continue
        end
        switch $t
            case %[3]s
                set skip 1
            case '-*'
            case '*'
                echo $t
        end
    end
end

# True if the current word is argument $argv[1] (1 is the config file).
function __%[2]s_at
    string match -q -- '-*' (commandline -ct); and return 1
    test (count (__%[2]s_args)) -eq (math $argv[1] - 1)
end

# True if the current word is the argument of command $argv[1].
function __%[2]s_arg_of
    string match -q -- '-*' (commandline -ct); and return 1
    set -l a (__%[2]s_args)
    test (count $a) -eq 2; and test "$a[2]" = $argv[1]
end

# Directories, and .json and .jsonc files.
function __%[2]s_configs
    __fish_complete_path (commandline -ct) | string match -r -- '^[^\t]*(?:\.jsonc?|/)(?:\t.*)?$'
end

# The curated packages: the directories in the config's rootPath whose names
# do not start with "." (the catalog's rule). rootPath is read with a regex,
# without running %[1]s; a commented-out rootPath line before the real one
# would be used instead.
function __%[2]s_packages
    set -l cfg (__%[2]s_args)[1]
    test -f "$cfg"; or return
    set -l root (string match -rg -- '"rootPath"\s*:\s*"([^"]*)"' <$cfg)[1]
    test -n "$root"; or return
    set root (string replace -r -- '^~(?=/|$)' $HOME $root)
    for d in $root/*/
        basename $d
    end
end

complete -c %[1]s -f
`, appName, fishIdent(appName), strings.Join(valueFlags, " "))

	b.WriteString("\n# Flags\n")
	for _, f := range flags {
		line := "complete -c " + appName
		if f.short != "" {
			line += " -s " + f.short
		}
		line += " -l " + f.name
		switch {
		case f.value != "":
			line += " -r -F"
		case f.name == "help":
			line += " -f -a " + helpFullValue
		}
		b.WriteString(line + " -d " + fishQuote(f.summary) + "\n")
	}

	b.WriteString("\n# Config file\n")
	fmt.Fprintf(&b, "complete -c %s -n '__%s_at 1' -a '(__%s_configs)'\n", appName, fishIdent(appName), fishIdent(appName))

	b.WriteString("\n# Commands\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "complete -c %s -n '__%s_at 2' -a %s -d %s\n", appName, fishIdent(appName), c.name, fishQuote(c.summary))
	}

	b.WriteString("\n# Command arguments\n")
	for _, c := range commands {
		if c.packages {
			fmt.Fprintf(&b, "complete -c %s -n '__%s_arg_of %s' -a '(__%s_packages)'\n", appName, fishIdent(appName), c.name, fishIdent(appName))
		}
	}
	return b.String()
}

// fishIdent turns name into a fish function-name part: aur-curator → aur_curator.
func fishIdent(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// fishQuote single-quotes s for fish.
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
