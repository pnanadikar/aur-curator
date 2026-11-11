package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/policy"
	"github.com/pramod/aur-curator/internal/publish"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// Dependencies holds all injected interfaces and concrete services
// required by App and its sub-models.
type Dependencies struct {
	Git         git.Provider
	Catalog     catalog.PackageCatalog
	Reviews     *review.Engine
	Analyzers   analyzer.Manager
	Policy      policy.Engine
	Builder     build.Builder
	Repo        domain.PackageRepository
	Publisher   publish.Publisher
	RepoStore   interface{} // RepositoryStore placeholder (WP2)
	ReviewDB    domain.ReviewStore
	BuildDB     domain.BuildStore
	ArtifactDB  domain.ArtifactStore
	BuildEngine *build.Engine
	Config      domain.WorkspaceConfig
	AUR         *aurinfo.Client
	// SandboxPaths are what the build sandbox is made from; check 4
	// self-tests the sandbox with them (#496).
	SandboxPaths build.SandboxPaths
}

// NewDependencies constructs all injected services required by the
// application. Stores are created via jsonstore using paths derived
// from cfg.RootPath. This function is the single shared construction
// point for both CLI (runImport) and TUI (runTUI) entry paths.
func NewDependencies(
	cfg domain.WorkspaceConfig,
	configPath string,
	provider git.Provider,
	pkgCatalog catalog.PackageCatalog,
) Dependencies {
	paths := store.NewStorePaths(cfg.RootPath)
	reviewStore := jsonstore.NewReviewStore(paths)
	buildStore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	analyzers := analyzer.New()
	// An unmapped finding category is a bug: ranked High, and logged once
	// per category per run (#475).
	policyEngine := policy.NewEngine(domain.PolicyConfig{}, policy.WithUnmappedHook(func(category string) {
		_ = errorlog.Log(context.Background(), cfg.RootPath, "policy",
			fmt.Sprintf("finding category %q has no severity mapping; ranked High", category))
	}))
	aurClient := aurinfo.NewClient("https://aur.archlinux.org/rpc/", 15*time.Second, aurinfo.NewGitLocalVersionResolver(provider, reviewStore))
	// The review engine reads the AUR record during analysis (#495).
	reviewEngine := review.NewEngine(provider, reviewStore, analyzers, policyEngine, review.WithAURLookup(aurClient))

	// Builds run in the bubblewrap sandbox (#496). A path that cannot be
	// found stays empty, and the build fails saying which.
	sandboxPaths := build.SandboxPaths{
		RootPath:       cfg.RootPath,
		RepositoryPath: cfg.RepositoryPath,
		ConfigFile:     configPath,
		ActivityLog:    cfg.ActivityLogPath,
		Helper:         helperPath(),
		Home:           homeDir(),
	}
	makepkgBuilder := build.NewMakepkgBuilder(sandboxPaths)
	buildEngine := build.NewEngine(makepkgBuilder, buildStore, artStore, provider, paths)

	return Dependencies{
		Git:       provider,
		Catalog:   pkgCatalog,
		Reviews:   reviewEngine,
		Analyzers: analyzers,
		Policy:    policyEngine,
		Builder:   makepkgBuilder,
		Repo: domain.PackageRepository{
			Location:      cfg.RepositoryPath,
			SigningStatus: "", // reserved for future signing support (roadmap Phase 3)
		},
		Publisher:   publish.NewPublisher(buildStore, artStore, domain.PackageRepository{Location: cfg.RepositoryPath}, cfg.RootPath, cfg.RepoDBName),
		RepoStore:   interface{}(nil), // RepositoryStore placeholder (WP2)
		ReviewDB:    reviewStore,
		BuildDB:     buildStore,
		ArtifactDB:  artStore,
		BuildEngine: buildEngine,
		Config:      cfg,
		AUR:         aurClient,

		SandboxPaths: sandboxPaths,
	}
}

// helperPath is this binary, resolved through symlinks so the sandbox
// can bind the real file. "" when it cannot be found.
func helperPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	return real
}

// homeDir is the user's home directory, or "" when unknown.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(h)
}
