package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/pkgname"
)

// Every per-package path checks the name (#504).
func TestPackagePathsCheckName(t *testing.T) {
	p := NewStorePaths("/ws")
	paths := map[string]func(domain.PackageName) (string, error){
		"PackageDir":           p.PackageDir,
		"ReviewPath":           p.ReviewPath,
		"BuildPath":            p.BuildPath,
		"LastSuccessBuildPath": p.LastSuccessBuildPath,
	}
	for name, f := range paths {
		for _, bad := range []domain.PackageName{"", ".", "..", "../x", "a/b", "Foo", "-x"} {
			got, err := f(bad)
			if err == nil || got != "" || !strings.Contains(err.Error(), pkgname.Rule) {
				t.Errorf("%s(%q) = %q, %v; want an error naming the rule", name, bad, got, err)
			}
		}
	}
	got, err := p.ReviewPath("yay")
	if want := filepath.Join("/ws", ".aur-curator", "packages", "yay", "review.json"); err != nil || got != want {
		t.Errorf("ReviewPath(yay) = %q, %v; want %q", got, err, want)
	}
}
