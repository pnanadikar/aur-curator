package git

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestFetchDoesNotTouchMasterOrOrigin(t *testing.T) {
	requireGit(t)

	workspace, pkgName, _ := createGitFixture(t)
	pkgDir := filepath.Join(workspace, pkgName)

	masterBefore := strings.TrimSpace(runGit(t, pkgDir, "rev-parse", "master"))
	originBefore := strings.TrimSpace(runGit(t, pkgDir, "remote", "get-url", "origin"))

	p := &provider{
		runner:  ExecRunner{},
		catalog: &fakeCatalog{repoPath: pkgDir},
		client:  http.DefaultClient,
	}
	if err := p.Fetch(context.Background(), domain.PackageName(pkgName)); err != nil {
		t.Fatal(err)
	}

	masterAfter := strings.TrimSpace(runGit(t, pkgDir, "rev-parse", "master"))
	originAfter := strings.TrimSpace(runGit(t, pkgDir, "remote", "get-url", "origin"))

	if masterAfter != masterBefore {
		t.Fatalf("Fetch must not move master: before %q, after %q", masterBefore, masterAfter)
	}
	if originAfter != originBefore {
		t.Fatalf("Fetch must not modify origin: before %q, after %q", originBefore, originAfter)
	}
}
