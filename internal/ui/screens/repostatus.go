package screens

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/repodb"
	"github.com/pramod/aur-curator/internal/theme"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/widgets"
)

// RepoPackageEntry is a single published package (name + version).
type RepoPackageEntry struct {
	Name    string
	Version string
}

// RepoStatusLoadedMsg carries the current repository status.
type RepoStatusLoadedMsg struct {
	Initialized    bool
	LastRepoAdd    time.Time
	DiskUsageBytes int64
	Packages       []RepoPackageEntry
	Err            error
}

// RepositoryStatusModel represents the local package repository
// status screen.
type RepositoryStatusModel struct {
	deps    deps.Dependencies
	width   int
	height  int
	loaded  bool
	status  RepoStatusLoadedMsg
	pkgList viewport.Model

	// helpOpen shows the full action help in place of the compact bar
	// (tui-package-details.md § Action Bar Convention).
	helpOpen bool
}

// NewRepositoryStatusModel constructs a new RepositoryStatusModel.
func NewRepositoryStatusModel(deps deps.Dependencies) RepositoryStatusModel {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return RepositoryStatusModel{
		deps:    deps,
		pkgList: vp,
	}
}

// Init returns the initialization command for the repository status screen.
func (m RepositoryStatusModel) Init() tea.Cmd {
	return loadRepoStatusCmd(m.deps)
}

// loadRepoStatusCmd is the tea.Cmd that performs all I/O for the
// repository status screen: stats the repo directory, the db file,
// and parses the package list from custom.db.tar.gz.
func loadRepoStatusCmd(deps deps.Dependencies) tea.Cmd {
	return func() tea.Msg {
		ctx := trace.Start(context.Background(), "repo_status_load")

		loc := deps.Repo.Location
		dbName := deps.Config.RepoDBName
		dbPath := filepath.Join(loc, dbName)

		s := RepoStatusLoadedMsg{
			Packages: []RepoPackageEntry{},
		}

		dirInfo, err := os.Stat(loc)
		if err != nil {
			if os.IsNotExist(err) {
				msg := fmt.Sprintf("repo location does not exist: %s", loc)
				_ = errorlog.Log(ctx, deps.Config.RootPath, "repo_status", msg)
				trace.Log(ctx, "repo_status", "reason", "location_missing", "location", loc)
				return RepoStatusLoadedMsg{Err: errors.New(msg)}
			}
			trace.Log(ctx, "repo_status", "reason", "stat_failed", "err", err.Error())
			return RepoStatusLoadedMsg{Err: fmt.Errorf("stat repo location: %w", err)}
		}

		s.Initialized = dirInfo.IsDir()
		if !s.Initialized {
			msg := fmt.Sprintf("repo location %s is not a directory", loc)
			_ = errorlog.Log(ctx, deps.Config.RootPath, "repo_status", msg)
			trace.Log(ctx, "repo_status", "reason", "not_a_directory", "location", loc)
			return RepoStatusLoadedMsg{Err: errors.New(msg)}
		}

		diskUsageBytes, err := dirSize(loc)
		if err != nil {
			trace.Log(ctx, "repo_status", "reason", "disk_usage_failed", "err", err.Error())
			_ = errorlog.Log(ctx, deps.Config.RootPath, "repo_status", "disk usage calculation failed: "+err.Error())
			return RepoStatusLoadedMsg{Initialized: true, Err: fmt.Errorf("calculate disk usage: %w", err)}
		}
		s.DiskUsageBytes = diskUsageBytes

		if dbInfo, err := os.Stat(dbPath); err == nil {
			s.LastRepoAdd = dbInfo.ModTime()
		}

		// A missing database is an empty snapshot, not an error: nothing
		// has been published yet (#378).
		snap, err := repodb.Read(dbPath)
		if err != nil {
			trace.Log(ctx, "repo_status", "reason", "read_failed", "err", err.Error())
			_ = errorlog.Log(ctx, deps.Config.RootPath, "repo_status", "repo db read failed: "+err.Error())
			return RepoStatusLoadedMsg{
				Initialized:    true,
				DiskUsageBytes: diskUsageBytes,
				LastRepoAdd:    s.LastRepoAdd,
				Packages:       []RepoPackageEntry{},
				Err:            fmt.Errorf("read repo db: %w", err),
			}
		}
		for _, e := range snap.Entries {
			s.Packages = append(s.Packages, RepoPackageEntry{Name: e.Name, Version: e.Version})
		}

		trace.Log(ctx, "repo_status", "packages", len(s.Packages), "disk_bytes", diskUsageBytes)
		return s
	}
}

// dirSize walks the given directory and returns the total size in bytes.
func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// fixedPartsAboveList returns every fixed part rendered above the
// package list, in render order: nav menu, blank line, the status meta
// lines, blank line, and the "Published Packages:" header.
func (m RepositoryStatusModel) fixedPartsAboveList() []string {
	lines := []string{widgets.RenderNavMenu(widgets.DefaultNavItems()), ""}

	type metaField struct {
		label string
		value string
	}
	loc := m.deps.Repo.Location
	statusText := "Not initialized"
	if m.status.Initialized {
		statusText = "Initialized"
	}
	var fields []metaField
	fields = append(fields, metaField{"Location:", loc})
	fields = append(fields, metaField{"Status:", statusText})
	if m.status.Initialized {
		lastAdd := "Never"
		if !m.status.LastRepoAdd.IsZero() {
			lastAdd = m.status.LastRepoAdd.Format("2006-01-02 15:04:05 MST")
		}
		fields = append(fields, metaField{"Last repo-add:", lastAdd})
		fields = append(fields, metaField{"Disk usage:", humanBytes(m.status.DiskUsageBytes)})
	}
	maxLabel := 0
	for _, f := range fields {
		if len(f.label) > maxLabel {
			maxLabel = len(f.label)
		}
	}
	for _, f := range fields {
		lines = append(lines, theme.HeaderStyle().Render(fmt.Sprintf("%-*s", maxLabel+1, f.label+" "))+theme.SubtleStyle().Render(f.value))
	}

	lines = append(lines, "")
	lines = append(lines, theme.HeaderStyle().Render("Published Packages:"))
	return lines
}

// actions lists the screen's actions in their fixed bar order. There is
// no Back: the screen is reached only through the Nav Menu, which clears
// the back history, so Esc has nowhere to go.
func (m RepositoryStatusModel) actions() []widgets.Action {
	return []widgets.Action{
		{Key: "R", Label: "Refresh", Enabled: true},
	}
}

// fixedPartsBelowList returns the fixed parts rendered below the package
// list: the action bar, or the full help while it is open.
func (m RepositoryStatusModel) fixedPartsBelowList() []string {
	if m.helpOpen {
		return []string{"\n" + widgets.RenderActionHelp(m.actions(), max(m.width-8, 40))}
	}
	return []string{"\n" + widgets.RenderActionBar(m.actions())}
}

// syncLayout sizes the package list viewport to whatever room remains
// for the screen's one flexible section.
func (m *RepositoryStatusModel) syncLayout() {
	m.pkgList.Width = max(m.width-6-theme.SectionBorderStyle().GetHorizontalFrameSize(), 0)
	m.pkgList.Height = widgets.FlexHeight(m.height, append(m.fixedPartsAboveList(), m.fixedPartsBelowList()...)...)
}

// Update processes messages for the repository status screen.
func (m RepositoryStatusModel) Update(msg tea.Msg) (RepositoryStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncLayout()
		return m, nil
	case RepoStatusLoadedMsg:
		m.loaded = true
		m.status = msg
		m.syncLayout()
		m.setPackageContent()
		return m, nil
	case msgs.BackMsg:
		return m, func() tea.Msg { return msgs.BackMsg{Changed: false} }
	case tea.KeyMsg:
		s := msg.String()
		if s == "?" || (m.helpOpen && s == "esc") {
			m.helpOpen = !m.helpOpen
			m.syncLayout()
			return m, nil
		}
		switch s {
		case "esc":
			return m, func() tea.Msg { return msgs.BackMsg{Changed: false} }
		case "1":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenDashboard, FromMenu: true} }
		case "2":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenRepositoryStatus, FromMenu: true} }
		case "3":
			return m, nil // Settings — dimmed, doesn't exist
		case "4":
			return m, func() tea.Msg { return msgs.NavigateMsg{To: msgs.ScreenHelp, FromMenu: true} }
		case "q":
			return m, func() tea.Msg { return msgs.QuitRequestedMsg{} }
		case "r", "R":
			return m, loadRepoStatusCmd(m.deps)
		case "home":
			m.pkgList.GotoTop()
			return m, nil
		case "end":
			m.pkgList.GotoBottom()
			return m, nil
		}
	}
	// Everything else (PgUp/PgDn, arrows, mouse wheel) goes to the
	// viewport so scrolling actually works.
	if m.loaded {
		var cmd tea.Cmd
		m.pkgList, cmd = m.pkgList.Update(msg)
		return m, cmd
	}
	return m, nil
}

// setPackageContent populates the viewport with the package list.
func (m *RepositoryStatusModel) setPackageContent() {
	if !m.loaded {
		return
	}
	var lines []string
	maxName := 0
	for _, p := range m.status.Packages {
		if len(p.Name) > maxName {
			maxName = len(p.Name)
		}
	}
	for _, p := range m.status.Packages {
		lines = append(lines, theme.SubtleStyle().Render(fmt.Sprintf("%-*s  %s", maxName, p.Name, p.Version)))
	}
	if len(lines) == 0 {
		lines = []string{"(none)"}
	}
	m.pkgList.SetContent(strings.Join(lines, "\n"))
}

// View renders the repository status screen inside the same bordered box
// every other screen uses (theme.SectionBorderStyle).
func (m RepositoryStatusModel) View() string {
	if !m.loaded {
		return widgets.WrapScreenBody(m.width, strings.Join([]string{
			widgets.RenderNavMenu(widgets.DefaultNavItems()),
			"",
			"Loading repository status...",
		}, "\n"))
	}

	if m.status.Err != nil {
		return widgets.WrapScreenBody(m.width, strings.Join([]string{
			widgets.RenderNavMenu(widgets.DefaultNavItems()),
			"",
			theme.ErrorStyle().Render("Error: " + m.status.Err.Error()),
		}, "\n"))
	}

	lines := append(m.fixedPartsAboveList(), m.pkgList.View())
	lines = append(lines, m.fixedPartsBelowList()...)

	return widgets.WrapScreenBody(m.width, strings.Join(lines, "\n"))
}

// humanBytes returns a human-readable byte size string (e.g. "1.2 MB").
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	size := float64(n)
	for _, u := range units {
		size /= 1024
		if size < 1024 {
			return fmt.Sprintf("%.1f %s", size, u)
		}
	}
	return fmt.Sprintf("%.1f PB", size)
}
