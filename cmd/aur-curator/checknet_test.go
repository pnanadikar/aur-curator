package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestUncuratedBuildNetwork(t *testing.T) {
	curated := map[domain.PackageName]bool{"yay": true, "rua": true}
	for _, tc := range []struct {
		name  string
		names []domain.PackageName
		want  []domain.PackageName
	}{
		{"none", nil, nil},
		{"all curated", []domain.PackageName{"yay", "rua"}, nil},
		{"some not", []domain.PackageName{"tor-browser-bin", "yay", "yya"}, []domain.PackageName{"tor-browser-bin", "yya"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := uncuratedBuildNetwork(tc.names, curated); !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := uncuratedBuildNetworkFinding("yya"); got != "buildNetwork: yya is not a curated package; remove it from the config or fix the spelling" {
		t.Errorf("finding %q", got)
	}
}

// check reports a buildNetwork name that is not curated, and only that
// one (#522).
func TestCheckBuildNetwork522(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	repo := filepath.Join(dir, "repo")
	for _, d := range []string{filepath.Join(root, "yay"), filepath.Join(root, ".aur-curator"), repo} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(dir, "cfg.json")
	body := fmt.Sprintf(`{"rootPath": %q, "repositoryPath": %q, "giteaBaseURL": "https://gitea.invalid", "giteaOrg": "o", "giteaToken": "t", "buildNetwork": ["yay", "yya"]}`, root, repo)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runMainOut(t, cfg, "check")
	if code != 1 {
		t.Errorf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"Check 1 — buildNetwork names not curated (report-only): 1\n",
		"buildNetwork: yya is not a curated package",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "buildNetwork: yay ") {
		t.Errorf("a curated name was reported:\n%s", stdout)
	}
}
