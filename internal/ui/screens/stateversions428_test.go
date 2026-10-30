package screens

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/widgets"
)

// versionGit428 is a mockGitProvider whose FileAtCommit returns a PKGBUILD
// per commit, or an error, and records the commits read.
type versionGit428 struct {
	*mockGitProvider
	files map[string]string // commit -> PKGBUILD
	errs  map[string]error  // commit -> read error
	reads []string
}

func (g *versionGit428) FileAtCommit(_ context.Context, _ domain.PackageName, commit, _ string) (string, error) {
	g.reads = append(g.reads, commit)
	if err := g.errs[commit]; err != nil {
		return "", err
	}
	return g.files[commit], nil
}

func pkgbuild428(ver string) string {
	return "pkgname=p\npkgver=" + ver + "\npkgrel=1\n"
}

// TestReadStateVersions: which commits are read for each review state,
// and what a failed or unreadable version leaves (tui-dashboard-wireframe.md
// § State, #428).
func TestReadStateVersions(t *testing.T) {
	files := map[string]string{
		"base":     pkgbuild428("1.10.0"),
		"reviewed": pkgbuild428("1.11.0"),
		"head":     pkgbuild428("1.9.0"),
		"computed": "pkgname=p\npkgver=$(date)\npkgrel=1\n",
	}
	readErr := errors.New("git broke")
	cases := []struct {
		name      string
		review    domain.Review
		isMerged  bool
		errs      map[string]error
		want      widgets.StateVersions
		wantReads []string
		wantLog   bool
	}{
		{"pending: base and reviewed",
			domain.Review{Status: domain.ReviewPending, BaseCommit: "base", UpstreamCommit: "reviewed"}, false, nil,
			widgets.StateVersions{Base: "1.10.0-1", Reviewed: "1.11.0-1"}, []string{"reviewed", "base"}, false},
		{"approved, not merged: base and reviewed",
			domain.Review{Status: domain.ReviewApproved, BaseCommit: "base", UpstreamCommit: "reviewed"}, false, nil,
			widgets.StateVersions{Base: "1.10.0-1", Reviewed: "1.11.0-1"}, []string{"reviewed", "base"}, false},
		{"first review: no base read",
			domain.Review{Status: domain.ReviewPending, UpstreamCommit: "reviewed"}, false, nil,
			widgets.StateVersions{Reviewed: "1.11.0-1", FirstReview: true}, []string{"reviewed"}, false},
		{"approved and merged: master HEAD only",
			domain.Review{Status: domain.ReviewApproved, BaseCommit: "base", UpstreamCommit: "reviewed"}, true, nil,
			widgets.StateVersions{Head: "1.9.0-1"}, []string{"head"}, false},
		{"rejected: reviewed only",
			domain.Review{Status: domain.ReviewRejected, BaseCommit: "base", UpstreamCommit: "reviewed"}, false, nil,
			widgets.StateVersions{Reviewed: "1.11.0-1"}, []string{"reviewed"}, false},
		{"reviewed read fails: logged, base not read",
			domain.Review{Status: domain.ReviewPending, BaseCommit: "base", UpstreamCommit: "reviewed"}, false,
			map[string]error{"reviewed": readErr},
			widgets.StateVersions{}, []string{"reviewed"}, true},
		{"base read fails: logged, reviewed kept",
			domain.Review{Status: domain.ReviewPending, BaseCommit: "base", UpstreamCommit: "reviewed"}, false,
			map[string]error{"base": readErr},
			widgets.StateVersions{Reviewed: "1.11.0-1"}, []string{"reviewed", "base"}, true},
		{"computed version: empty, not logged",
			domain.Review{Status: domain.ReviewRejected, UpstreamCommit: "computed"}, false, nil,
			widgets.StateVersions{}, []string{"computed"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			g := &versionGit428{mockGitProvider: &mockGitProvider{}, files: files, errs: tc.errs}
			m := DashboardModel{deps: deps.Dependencies{Git: g, Config: domain.WorkspaceConfig{RootPath: root}}}

			review := tc.review
			got := m.readStateVersions(context.Background(), "p", &review, tc.isMerged, "head")

			if got != tc.want {
				t.Errorf("versions = %+v, want %+v", got, tc.want)
			}
			if strings.Join(g.reads, ",") != strings.Join(tc.wantReads, ",") {
				t.Errorf("commits read = %v, want %v", g.reads, tc.wantReads)
			}
			logData, _ := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
			switch {
			case tc.wantLog && !strings.Contains(string(logData), "git broke"):
				t.Errorf("read error not in the error log; log:\n%s", logData)
			case !tc.wantLog && len(logData) != 0:
				t.Errorf("error log should be empty; log:\n%s", logData)
			}
		})
	}
}
