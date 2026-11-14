package screens

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/pkgname"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/review"
)

// A directory the catalog rejects is skipped and logged like a package
// with bad remotes, so the status bar names it (#504, #513).
func TestLoadPackagesRejectedNames504(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	g := metadataGit513{}
	store := &fakeReviewStore{review: &domain.Review{ID: "r1", Status: domain.ReviewPending}}
	m := NewDashboardModel(deps.Dependencies{
		Git: g,
		Catalog: &mockCatalog{
			pkgs:     []domain.PackageName{"yay"},
			rejected: []catalog.Rejected{{Name: "Foo", Err: pkgname.Check("Foo")}},
		},
		Reviews: review.NewEngine(g, store, fakeAnalyzerManager{}, policy.NewEngine(domain.PolicyConfig{})),
		Config:  domain.WorkspaceConfig{RootPath: root},
	})
	msg, ok := m.loadPackagesCmd(context.Background())().(PackagesLoadedMsg)
	if !ok {
		t.Fatal("loadPackagesCmd did not return a PackagesLoadedMsg")
	}
	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if len(msg.Packages) != 1 || msg.Packages[0] != "yay" {
		t.Errorf("Packages = %v, want [yay]", msg.Packages)
	}
	if len(msg.Skipped) != 1 || msg.Skipped[0] != "Foo" {
		t.Errorf("Skipped = %v, want [Foo]", msg.Skipped)
	}
	logged, err := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if want := `skipped "Foo": invalid package name "Foo": the AUR allows`; !strings.Contains(string(logged), want) {
		t.Errorf("error log lacks %q:\n%s", want, logged)
	}
}
