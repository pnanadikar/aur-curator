package screens

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/git"
)

// difftoolEnabled: a review is loaded and the active tab shows a diff
// with files (not Findings).
func (m ReviewScreen) difftoolEnabled() bool {
	label := m.tabs.Active().Label
	return m.review != nil && label != "Findings" && len(tabFiles(m.diff, label)) > 0
}

// openDifftool runs git difftool over the review's stored range for every
// file of the active tab, in the package's clone (tui-review.md § Your Own
// Tools, #488). git opens them one after another, asking first unless the
// user's configuration says otherwise.
func (m ReviewScreen) openDifftool() tea.Cmd {
	const what = "Diff tool"
	dir, err := catalog.NewFilesystemCatalog(m.config.RootPath).RepositoryPath(m.pkgName)
	if err != nil {
		return toolFailed(what, m.pkgName, err)
	}
	files := tabFiles(m.diff, m.tabs.Active().Label)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	cmd := git.DifftoolCmd(dir, m.review.BaseCommit, m.review.UpstreamCommit, paths)
	return runTool(what, m.pkgName, cmd, nil)
}

// viewContent is the active tab's whole content, styled: the findings, or
// the tab's diff.
func (m ReviewScreen) viewContent() string {
	if m.tabs.Active().Label == "Findings" {
		return m.findingsPane.Content()
	}
	return m.diffPane.Content()
}

// openView shows the active tab's content in the pager (tui-review.md
// § Your Own Tools, #488): as on the screen, without colour, named .diff
// on a tab that shows a diff.
func (m ReviewScreen) openView() tea.Cmd {
	label := m.tabs.Active().Label
	return openPager(m.pkgName, pagerName(label, label != "Findings"), m.viewContent())
}
