package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/ui/msgs"
	"github.com/pramod/aur-curator/internal/ui/screens"
)

func TestSkippedWarning513(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		skipped []domain.PackageName
		want    string
	}{
		{"none", nil, ""},
		{"one", []domain.PackageName{"a"}, "1 package not loaded (see the error log): a"},
		{"three", []domain.PackageName{"a", "b", "c"}, "3 packages not loaded (see the error log): a, b, c"},
		{"five", []domain.PackageName{"a", "b", "c", "d", "e"}, "5 packages not loaded (see the error log): a, b, c, +2 more"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := skippedWarning(tt.skipped); got != tt.want {
				t.Errorf("skippedWarning(%v) = %q, want %q", tt.skipped, got, tt.want)
			}
		})
	}
}

// TestStatusBarNamesSkippedPackages513: packages the Dashboard's load left
// out are named on the status bar, on any screen, until a load leaves
// none out (tui-dashboard-wireframe.md § 7, #513).
func TestStatusBarNamesSkippedPackages513(t *testing.T) {
	t.Parallel()
	app := App{
		screen:    msgs.ScreenPackageDetail,
		dashboard: screens.NewDashboardModel(deps.Dependencies{}),
		detail:    screens.NewPackageDetailModel(deps.Dependencies{}),
		width:     200,
	}
	lastLine := func(a App) string {
		lines := strings.Split(a.View(), "\n")
		return lines[len(lines)-1]
	}

	updated, _ := app.Update(screens.PackagesLoadedMsg{
		Packages: []domain.PackageName{"good"},
		Skipped:  []domain.PackageName{"a", "b"},
		TraceCtx: context.Background(),
	})
	app = updated.(App)
	want := "⚠ 2 packages not loaded (see the error log): a, b"
	if got := lastLine(app); !strings.Contains(got, want) || !strings.Contains(got, "Idle") {
		t.Errorf("status bar = %q, want Idle and %q", got, want)
	}

	updated, _ = app.Update(screens.PackagesLoadedMsg{
		Packages: []domain.PackageName{"good"},
		TraceCtx: context.Background(),
	})
	app = updated.(App)
	if got := lastLine(app); strings.Contains(got, "not loaded") {
		t.Errorf("status bar after a clean load = %q, want no warning", got)
	}
}
