package build

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// TestRunBuild522_BuildNetwork: the Engine tells the Builder the package
// and whether it is in buildNetwork (#522).
func TestRunBuild522_BuildNetwork(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pkg  domain.PackageName
		opts []EngineOption
		want BuildOptions
	}{
		{"listed", "yay", []EngineOption{WithBuildNetwork([]domain.PackageName{"rua", "yay"})}, BuildOptions{Package: "yay", Network: true}},
		{"not listed", "pfetch", []EngineOption{WithBuildNetwork([]domain.PackageName{"yay"})}, BuildOptions{Package: "pfetch"}},
		{"no option", "yay", nil, BuildOptions{Package: "yay"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ws := t.TempDir()
			paths := store.NewStorePaths(ws)
			b := newFakeBuilder("content")
			e := NewEngine(b, jsonstore.NewBuildStore(paths), jsonstore.NewArtifactStore(paths), &fakeGitProviderForBuild{}, paths, tc.opts...)
			if _, err := e.RunBuild(context.Background(), tc.pkg); err != nil {
				t.Fatalf("RunBuild: %v", err)
			}
			if b.gotOpts != tc.want {
				t.Errorf("Builder got %+v, want %+v", b.gotOpts, tc.want)
			}
		})
	}
}
