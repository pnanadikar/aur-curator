package git

import "os/exec"

// DifftoolCmd returns the git difftool command for a review's range, to be
// run in the package's clone (repoDir) with the terminal handed over
// (tea.ExecProcess, #488): base to upstream, the empty tree when base is
// "" (a first review), limited to paths, which git opens one after another.
//
// It adds no options: the user's git configuration chooses the tool
// (diff.tool) and whether git asks before each file (difftool.prompt), as
// when they run git difftool themselves. Nothing is started, and there is
// no timeout: the command ends when the user quits the tool.
func DifftoolCmd(repoDir, base, upstream string, paths []string) *exec.Cmd {
	if base == "" {
		base = emptyTreeHash
	}
	args := append([]string{"difftool", base, upstream, "--"}, paths...)
	cmd := exec.Command("git", args...) //nolint:gosec // git difftool; revisions from the review, paths after --
	cmd.Dir = repoDir
	return cmd
}
