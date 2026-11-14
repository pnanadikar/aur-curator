# fish completion for aur-curator (#511).
# Generated from cmd/aur-curator/cli.go: do not edit. To regenerate:
#   go test ./cmd/aur-curator -run TestFishCompletion -update

# The arguments typed before the current word, without flags and their values.
function __aur_curator_args
    set -l skip 0
    for t in (commandline -opc)[2..-1]
        if test $skip = 1
            set skip 0
            continue
        end
        switch $t
            case --debug-log -debug-log --trace -trace
                set skip 1
            case '-*'
            case '*'
                echo $t
        end
    end
end

# True if the current word is argument $argv[1] (1 is the config file).
function __aur_curator_at
    string match -q -- '-*' (commandline -ct); and return 1
    test (count (__aur_curator_args)) -eq (math $argv[1] - 1)
end

# True if the current word is the argument of command $argv[1].
function __aur_curator_arg_of
    string match -q -- '-*' (commandline -ct); and return 1
    set -l a (__aur_curator_args)
    test (count $a) -eq 2; and test "$a[2]" = $argv[1]
end

# Directories, and .json and .jsonc files.
function __aur_curator_configs
    __fish_complete_path (commandline -ct) | string match -r -- '^[^\t]*(?:\.jsonc?|/)(?:\t.*)?$'
end

# The curated packages: the directories in the config's rootPath whose names
# do not start with "." (the catalog's rule). rootPath is read with a regex,
# without running aur-curator; a commented-out rootPath line before the real one
# would be used instead.
function __aur_curator_packages
    set -l cfg (__aur_curator_args)[1]
    test -f "$cfg"; or return
    set -l root (string match -rg -- '"rootPath"\s*:\s*"([^"]*)"' <$cfg)[1]
    test -n "$root"; or return
    set root (string replace -r -- '^~(?=/|$)' $HOME $root)
    for d in $root/*/
        basename $d
    end
end

complete -c aur-curator -f

# Flags
complete -c aur-curator -l debug-log -r -F -d 'write debug logs to <path>'
complete -c aur-curator -l trace -r -F -d 'write trace logs to <path>'
complete -c aur-curator -s h -l help -f -a full -d 'show this summary; --help=full shows the full guide'
complete -c aur-curator -l version -d 'print the version'

# Config file
complete -c aur-curator -n '__aur_curator_at 1' -a '(__aur_curator_configs)'

# Commands
complete -c aur-curator -n '__aur_curator_at 2' -a import -d 'curate a new package'
complete -c aur-curator -n '__aur_curator_at 2' -a update -d 'check curated packages against the AUR'
complete -c aur-curator -n '__aur_curator_at 2' -a outdated -d 'list packages the AUR has something different for'
complete -c aur-curator -n '__aur_curator_at 2' -a check -d 'run consistency checks'
complete -c aur-curator -n '__aur_curator_at 2' -a remove -d 'stop curating a package'

# Command arguments
complete -c aur-curator -n '__aur_curator_arg_of remove' -a '(__aur_curator_packages)'
