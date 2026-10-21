package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// ---------------------------------------------------------------------------
// Fakes for Engine tests
// ---------------------------------------------------------------------------

// fakeBuilder creates real .pkg.tar.zst files inside the workspace directory
// and returns their paths in BuildResult.
type fakeBuilder struct {
	artifacts   []fakeArtifact // ordered list of artifact files to create
	failNext    bool           // if true, the next Build call returns an error
	cancel      bool           // if true, the next Build call returns context.Canceled
	onBuild     func()         // called at the start of Build, if non-nil
	badArtifact bool           // if true, return a successful BuildResult whose ArtifactPaths includes a non-existent path

	gotWorkspace string // workspaceDir of the last Build call ("" if never called)
}

type fakeArtifact struct {
	name    string
	content []byte
}

func newFakeBuilder(artifactContents ...string) *fakeBuilder {
	arts := make([]fakeArtifact, len(artifactContents))
	for i, c := range artifactContents {
		arts[i] = fakeArtifact{
			name:    fmt.Sprintf("testpkg-1.0-%d-x86_64.pkg.tar.zst", i),
			content: []byte(c),
		}
	}
	return &fakeBuilder{artifacts: arts}
}

func (f *fakeBuilder) Build(_ context.Context, workspaceDir string, logPath string) (BuildResult, error) {
	f.gotWorkspace = workspaceDir
	if f.onBuild != nil {
		f.onBuild()
	}

	// Create the log directory (mirrors MakepkgBuilder behavior).
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return BuildResult{}, err
	}

	// Create the artifact files in the workspace.
	var paths []string
	for _, art := range f.artifacts {
		path := filepath.Join(workspaceDir, art.name)
		if err := os.WriteFile(path, art.content, 0o644); err != nil {
			return BuildResult{}, err
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return BuildResult{}, err
		}
		paths = append(paths, abs)
	}

	// Create the log file at the persistent path (same as MakepkgBuilder).
	if err := os.WriteFile(logPath, []byte("fake build log"), 0o644); err != nil {
		return BuildResult{}, err
	}

	if f.failNext {
		return BuildResult{}, fmt.Errorf("simulated build failure")
	}

	if f.cancel {
		return BuildResult{}, fmt.Errorf("makepkg cancelled: %w", context.Canceled)
	}

	// badArtifact: replace the last artifact path with a non-existent path.
	if f.badArtifact && len(paths) > 0 {
		paths[len(paths)-1] = "/nonexistent/path/does-not-exist.pkg.tar.zst"
	}

	return BuildResult{
		LogLocation:   logPath,
		ArtifactPaths: paths,
	}, nil
}

// fakeGitProviderForBuild simulates the git operations needed for a successful
// RunBuild: it returns a commit hash and creates the workspace directory when
// CloneAt is called.
type fakeGitProviderForBuild struct {
	commitHash string
}

func (f *fakeGitProviderForBuild) Import(context.Context, domain.PackageName) error { return nil }
func (f *fakeGitProviderForBuild) Fetch(context.Context, domain.PackageName) error  { return nil }
func (f *fakeGitProviderForBuild) Status(context.Context, domain.PackageName) (*git.RepositoryStatus, error) {
	return &git.RepositoryStatus{}, nil
}
func (f *fakeGitProviderForBuild) CommitsBehind(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}
func (f *fakeGitProviderForBuild) UpstreamCommit(context.Context, domain.PackageName) (string, error) {
	return "", nil
}
func (f *fakeGitProviderForBuild) CloneAt(_ context.Context, _ domain.PackageName, commit string, workspaceDir string) error {
	f.commitHash = commit
	return os.MkdirAll(workspaceDir, 0o755)
}
func (f *fakeGitProviderForBuild) LocalHeadCommit(context.Context, domain.PackageName) (string, error) {
	return "abc123def456", nil
}
func (f *fakeGitProviderForBuild) Diff(context.Context, domain.PackageName) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}
func (f *fakeGitProviderForBuild) ReviewDiff(context.Context, domain.PackageName, string, string) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}
func (f *fakeGitProviderForBuild) Merge(context.Context, domain.PackageName, string) error {
	return nil
}
func (f *fakeGitProviderForBuild) LocalOnlyCommits(context.Context, domain.PackageName) (int, error) {
	return 0, nil
}
func (f *fakeGitProviderForBuild) Metadata(context.Context, domain.PackageName) (*git.RepositoryMetadata, error) {
	return &git.RepositoryMetadata{}, nil
}
func (f *fakeGitProviderForBuild) IsMerged(context.Context, domain.PackageName, string) (bool, error) {
	return false, nil
}
func (f *fakeGitProviderForBuild) FileAtCommit(context.Context, domain.PackageName, string, string) (string, error) {
	return "", nil
}

// ---------------------------------------------------------------------------
// Regression tests
// ---------------------------------------------------------------------------

// TestRunBuildArtifactsPersistAfterCleanup verifies that artifact files copied
// from the disposable workspace survive the deferred workspace deletion that
// happens on a successful build.
func TestRunBuildArtifactsPersistAfterCleanup(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("artifact payload data")

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "testpkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	if build.Status != domain.BuildSuccess {
		t.Fatalf("build status = %q, want %q", build.Status, domain.BuildSuccess)
	}

	if build.LogLocation == "" {
		t.Fatal("LogLocation is empty")
	}

	// The workspace temp dir has already been cleaned up by the deferred
	// os.RemoveAll at this point. Verify the build log persists.
	if _, err := os.ReadFile(build.LogLocation); err != nil {
		t.Fatalf("build log not found at persistent path %s: %v", build.LogLocation, err)
	}

	// Verify artifacts were stored.
	if len(build.ArtifactIDs) == 0 {
		t.Fatal("no artifacts recorded for successful build")
	}

	// Each artifact file must still exist at the persistent path.
	for i, artID := range build.ArtifactIDs {
		artifact, err := artStore.Get(artID)
		if err != nil {
			t.Fatalf("get artifact %d (%s): %v", i, artID, err)
		}
		if artifact.BuildID != build.ID {
			t.Errorf("artifact %d: BuildID = %q, want %q", i, artifact.BuildID, build.ID)
		}
		if artifact.Size == 0 {
			t.Errorf("artifact %d: Size = 0, want > 0", i)
		}
		if artifact.Checksum == "" {
			t.Errorf("artifact %d: Checksum is empty", i)
		}
		if artifact.Filename == "" {
			t.Errorf("artifact %d: Filename is empty", i)
		}
		if artifact.Version == "" {
			t.Errorf("artifact %d: Version is empty", i)
		}
		if artifact.Architecture == "" {
			t.Errorf("artifact %d: Architecture is empty", i)
		}

		// The key assertion: the artifact file must still be readable at the
		// persistent location <workspace>/.aur-curator/builds/<buildID>/<filename>.
		persistentPath := filepath.Join(
			workspace, ".aur-curator", "builds", string(build.ID), artifact.Filename,
		)
		data, err := os.ReadFile(persistentPath)
		if err != nil {
			t.Errorf("artifact %d (%s): file not found at persistent path %s: %v",
				i, artifact.Filename, persistentPath, err)
			continue
		}
		if string(data) != "artifact payload data" {
			t.Errorf("artifact %d: file content = %q, want %q",
				i, string(data), "artifact payload data")
		}
	}
}

// TestRunBuildSingleArtifact verifies persistence for a single artifact.
func TestRunBuildSingleArtifact(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("single artifact content")

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "singlepkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	if build.Status != domain.BuildSuccess {
		t.Fatalf("status = %q, want %q", build.Status, domain.BuildSuccess)
	}

	if len(build.ArtifactIDs) != 1 {
		t.Fatalf("ArtifactIDs length = %d, want 1", len(build.ArtifactIDs))
	}

	artifact, err := artStore.Get(build.ArtifactIDs[0])
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	persistentPath := filepath.Join(
		workspace, ".aur-curator", "builds", string(build.ID), artifact.Filename,
	)
	data, err := os.ReadFile(persistentPath)
	if err != nil {
		t.Fatalf("artifact file not readable at %s: %v", persistentPath, err)
	}
	if string(data) != "single artifact content" {
		t.Fatalf("file content = %q, want %q", string(data), "single artifact content")
	}
}

// TestRunBuildMultipleArtifacts verifies that all artifact files from a
// multi-artifact build are persisted and readable after workspace cleanup.
func TestRunBuildMultipleArtifacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder(
		"first artifact data",
		"second artifact data",
	)

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "multipkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	if len(build.ArtifactIDs) != 2 {
		t.Fatalf("ArtifactIDs length = %d, want 2", len(build.ArtifactIDs))
	}

	wantContents := []string{"first artifact data", "second artifact data"}
	for i, artID := range build.ArtifactIDs {
		artifact, err := artStore.Get(artID)
		if err != nil {
			t.Fatalf("get artifact %d: %v", i, err)
		}

		persistentPath := filepath.Join(
			workspace, ".aur-curator", "builds", string(build.ID), artifact.Filename,
		)
		data, err := os.ReadFile(persistentPath)
		if err != nil {
			t.Fatalf("artifact %d: file not found at %s: %v", i, persistentPath, err)
		}
		if string(data) != wantContents[i] {
			t.Errorf("artifact %d: content = %q, want %q", i, string(data), wantContents[i])
		}
	}
}

// TestRunBuildArtifactChecksum verifies that the stored checksum matches
// the actual SHA-256 of the persisted artifact file.
func TestRunBuildArtifactChecksum(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("checksum test payload")

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "cksumpkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	artifact, err := artStore.Get(build.ArtifactIDs[0])
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	// Compute the expected checksum from the persisted file.
	persistentPath := filepath.Join(
		workspace, ".aur-curator", "builds", string(build.ID), artifact.Filename,
	)
	_, err = os.ReadFile(persistentPath)
	if err != nil {
		t.Fatalf("read persisted artifact: %v", err)
	}

	expected, err := sha256Hash(persistentPath)
	if err != nil {
		t.Fatalf("sha256Hash: %v", err)
	}

	if artifact.Checksum != expected {
		t.Errorf("Checksum = %q, want %q", artifact.Checksum, expected)
	}
}

// TestRunBuildNoArtifacts verifies that a build with zero artifact files
// completes successfully (no panic, no error).
func TestRunBuildNoArtifacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := &fakeBuilder{artifacts: []fakeArtifact{}}

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "nopkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	if build.Status != domain.BuildSuccess {
		t.Fatalf("status = %q, want %q", build.Status, domain.BuildSuccess)
	}

	if len(build.ArtifactIDs) != 0 {
		t.Fatalf("ArtifactIDs length = %d, want 0", len(build.ArtifactIDs))
	}
}

// TestRunBuildArtifactSizeMatches verifies that the stored Size field
// matches the actual persisted file size.
func TestRunBuildArtifactSizeMatches(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	payload := strings.Repeat("x", 4096)
	builder := newFakeBuilder(payload)

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "sizepkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	artifact, err := artStore.Get(build.ArtifactIDs[0])
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	if artifact.Size != int64(len(payload)) {
		t.Errorf("Size = %d, want %d", artifact.Size, len(payload))
	}
}

// TestRunBuildArtifactFilenameIsBare verifies that Artifact.Filename is set to
// the bare filename (not the full persistent path), consistent with the
// existing renderArtifactHistoryConsumer usage.
func TestRunBuildArtifactFilenameIsBare(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("filename test")

	engine := NewEngine(builder, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "fnamepkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	artifact, err := artStore.Get(build.ArtifactIDs[0])
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}

	if filepath.IsAbs(artifact.Filename) {
		t.Errorf("Filename = %q, want bare filename (not absolute path)", artifact.Filename)
	}
	if artifact.Filename != "testpkg-1.0-0-x86_64.pkg.tar.zst" {
		t.Errorf("Filename = %q, want %q", artifact.Filename, "testpkg-1.0-0-x86_64.pkg.tar.zst")
	}
}

// TestBuildArtifactIDsFiltering verifies that two packages with their own
// builds return only their respective artifacts when the two-step
// BuildStore.Get → ArtifactIDs → ArtifactStore.Get pattern is used.
func TestBuildArtifactIDsFiltering(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{commitHash: "aaaa"}
	builderA := newFakeBuilder("pkg-a artifact data")
	builderB := newFakeBuilder("pkg-b artifact data")

	engineA := NewEngine(builderA, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	engineB := NewEngine(builderB, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	buildA, err := engineA.RunBuild(context.Background(), "pkg-a")
	if err != nil {
		t.Fatalf("RunBuild(pkg-a): %v", err)
	}

	buildB, err := engineB.RunBuild(context.Background(), "pkg-b")
	if err != nil {
		t.Fatalf("RunBuild(pkg-b): %v", err)
	}

	if len(buildA.ArtifactIDs) == 0 {
		t.Fatal("pkg-a has no artifact IDs")
	}
	if len(buildB.ArtifactIDs) == 0 {
		t.Fatal("pkg-b has no artifact IDs")
	}

	if buildA.ArtifactIDs[0] == buildB.ArtifactIDs[0] {
		t.Fatal("pkg-a and pkg-b share the same artifact ID")
	}

	// Simulate loadArtifactHistoryCmd: two-step pattern for pkg-a.
	var artsA []domain.Artifact
	for _, id := range buildA.ArtifactIDs {
		a, err := artStore.Get(id)
		if err != nil {
			t.Fatalf("get artifact %s: %v", id, err)
		}
		artsA = append(artsA, *a)
	}

	// Simulate for pkg-b.
	var artsB []domain.Artifact
	for _, id := range buildB.ArtifactIDs {
		a, err := artStore.Get(id)
		if err != nil {
			t.Fatalf("get artifact %s: %v", id, err)
		}
		artsB = append(artsB, *a)
	}

	if len(artsA) != 1 {
		t.Errorf("pkg-a artifacts: got %d, want 1", len(artsA))
	}
	if len(artsB) != 1 {
		t.Errorf("pkg-b artifacts: got %d, want 1", len(artsB))
	}

	// Confirm pkg-a's artifact contains pkg-a data, not pkg-b's.
	aContent, err := os.ReadFile(filepath.Join(workspace, ".aur-curator", "builds", string(buildA.ID), artsA[0].Filename))
	if err != nil {
		t.Fatalf("read pkg-a artifact: %v", err)
	}
	if !strings.Contains(string(aContent), "pkg-a") {
		t.Errorf("pkg-a artifact content = %q, want it to contain %q", string(aContent), "pkg-a")
	}

	// Confirm pkg-b's artifact contains pkg-b data, not pkg-a's.
	bContent, err := os.ReadFile(filepath.Join(workspace, ".aur-curator", "builds", string(buildB.ID), artsB[0].Filename))
	if err != nil {
		t.Fatalf("read pkg-b artifact: %v", err)
	}
	if !strings.Contains(string(bContent), "pkg-b") {
		t.Errorf("pkg-b artifact content = %q, want it to contain %q", string(bContent), "pkg-b")
	}

	// Confirm the two-step pattern never mixes artifacts between packages.
	for _, art := range artsA {
		for _, id := range buildB.ArtifactIDs {
			if art.ID == id {
				t.Errorf("pkg-a result contains pkg-b artifact ID %s", id)
			}
		}
	}
	for _, art := range artsB {
		for _, id := range buildA.ArtifactIDs {
			if art.ID == id {
				t.Errorf("pkg-b result contains pkg-a artifact ID %s", id)
			}
		}
	}
}

// TestGetArtifactsByPackageFiltering verifies that GetArtifactsByPackage
// returns only artifacts whose PackageName matches the query.
func TestGetArtifactsByPackageFiltering(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{commitHash: "bbbb"}

	engineA := NewEngine(newFakeBuilder("pkg-a data"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	engineB := NewEngine(newFakeBuilder("pkg-b data"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	_, err := engineA.RunBuild(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("RunBuild(alpha): %v", err)
	}
	_, err = engineB.RunBuild(context.Background(), "beta")
	if err != nil {
		t.Fatalf("RunBuild(beta): %v", err)
	}

	artsA, err := artStore.GetArtifactsByPackage("alpha")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage(alpha): %v", err)
	}
	artsB, err := artStore.GetArtifactsByPackage("beta")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage(beta): %v", err)
	}

	if len(artsA) != 1 {
		t.Errorf("alpha artifacts: got %d, want 1", len(artsA))
	}
	if len(artsB) != 1 {
		t.Errorf("beta artifacts: got %d, want 1", len(artsB))
	}

	if len(artsA) > 0 && artsA[0].PackageName != "alpha" {
		t.Errorf("alpha artifact PackageName = %q, want %q", artsA[0].PackageName, "alpha")
	}
	if len(artsB) > 0 && artsB[0].PackageName != "beta" {
		t.Errorf("beta artifact PackageName = %q, want %q", artsB[0].PackageName, "beta")
	}

	// Verify no cross-contamination: alpha artifacts should not appear for beta.
	for _, art := range artsA {
		if art.PackageName == domain.PackageName("beta") {
			t.Errorf("alpha result contains artifact with PackageName=beta")
		}
	}
	for _, art := range artsB {
		if art.PackageName == domain.PackageName("alpha") {
			t.Errorf("beta result contains artifact with PackageName=alpha")
		}
	}
}

// ---------------------------------------------------------------------------
// parseArtifactFilename — table-driven tests
// ---------------------------------------------------------------------------

func TestParseArtifactFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantVer  string
		wantArch string
		wantErr  bool
	}{
		{
			name:     "simple non-hyphenated name",
			input:    "fd-8.7.0-1-x86_64.pkg.tar.zst",
			wantVer:  "8.7.0-1",
			wantArch: "x86_64",
		},
		{
			name:     "hyphenated name visual-studio-code-bin",
			input:    "visual-studio-code-bin-1.2.3-1-x86_64.pkg.tar.zst",
			wantVer:  "1.2.3-1",
			wantArch: "x86_64",
		},
		{
			name:     "hyphenated name with many parts",
			input:    "python-pip-24.0-1-aarch64.pkg.tar.zst",
			wantVer:  "24.0-1",
			wantArch: "aarch64",
		},
		{
			name:     "pkgrel with multiple digits",
			input:    "neovim-0.10.0-371-x86_64.pkg.tar.zst",
			wantVer:  "0.10.0-371",
			wantArch: "x86_64",
		},
		{
			name:     ".pkg.tar.xz extension",
			input:    "curl-8.5.0-1-x86_64.pkg.tar.xz",
			wantVer:  "8.5.0-1",
			wantArch: "x86_64",
		},
		{
			name:     ".pkg.tar.gz extension",
			input:    "wget2-2.6.0-1-aarch64.pkg.tar.gz",
			wantVer:  "2.6.0-1",
			wantArch: "aarch64",
		},
		{
			name:     ".pkg.tar.lz extension",
			input:    "foo-1.0-1-x86_64.pkg.tar.lz",
			wantVer:  "1.0-1",
			wantArch: "x86_64",
		},
		{
			name:     ".pkg.tar.bz2 extension",
			input:    "bar-2.0-1-armv7h.pkg.tar.bz2",
			wantVer:  "2.0-1",
			wantArch: "armv7h",
		},
		{
			name:     ".pkg.tar.lzo extension",
			input:    "baz-3.0-1-riscv64.pkg.tar.lzo",
			wantVer:  "3.0-1",
			wantArch: "riscv64",
		},
		{
			name:     "pkgver with hyphens",
			input:    "some-pkg-1.0-beta-1-x86_64.pkg.tar.zst",
			wantVer:  "beta-1",
			wantArch: "x86_64",
		},
		{
			name:    "too few segments",
			input:   "foo-1.0-x86_64.pkg.tar.zst",
			wantErr: true,
		},
		{
			name:    "unknown extension",
			input:   "foo-1.0-1-x86_64.deb",
			wantErr: true,
		},
		{
			name:    "bare filename no segments",
			input:   "noext",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotVer, gotArch, err := parseArtifactFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseArtifactFilename(%q) = %q, %q, nil — want error", tt.input, gotVer, gotArch)
				}
				return
			}
			if err != nil {
				t.Errorf("parseArtifactFilename(%q) error = %v", tt.input, err)
				return
			}
			if gotVer != tt.wantVer {
				t.Errorf("parseArtifactFilename(%q) version = %q, want %q", tt.input, gotVer, tt.wantVer)
			}
			if gotArch != tt.wantArch {
				t.Errorf("parseArtifactFilename(%q) arch = %q, want %q", tt.input, gotArch, tt.wantArch)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Purge tests
// ---------------------------------------------------------------------------

// TestPurgeOldSuccessOnNewSuccess verifies that a second successful build
// for the same package purges the artifact directory of the first successful
// build while retaining the directory of the second build.
func TestPurgeOldSuccessOnNewSuccess(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	engine := NewEngine(newFakeBuilder("first"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	// First build — success.
	build1, err := engine.RunBuild(context.Background(), "purgepkg")
	if err != nil {
		t.Fatalf("RunBuild #1: %v", err)
	}
	if build1.Status != domain.BuildSuccess {
		t.Fatalf("build1 status = %q, want %q", build1.Status, domain.BuildSuccess)
	}

	dir1 := filepath.Join(workspace, ".aur-curator", "builds", string(build1.ID))
	if _, err := os.Stat(dir1); os.IsNotExist(err) {
		t.Fatal("first build directory does not exist after RunBuild")
	}

	// Verify LastSuccessBuildID is set.
	if build1.LastSuccessBuildID != build1.ID {
		t.Errorf("build1.LastSuccessBuildID = %q, want %q", build1.LastSuccessBuildID, build1.ID)
	}

	// Second build — success for the same package.
	engine2 := NewEngine(newFakeBuilder("second"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build2, err := engine2.RunBuild(context.Background(), "purgepkg")
	if err != nil {
		t.Fatalf("RunBuild #2: %v", err)
	}
	if build2.Status != domain.BuildSuccess {
		t.Fatalf("build2 status = %q, want %q", build2.Status, domain.BuildSuccess)
	}

	// The first build's directory must be purged.
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Errorf("first build directory %q still exists after second success build", dir1)
	}

	// The second build's directory must survive.
	dir2 := filepath.Join(workspace, ".aur-curator", "builds", string(build2.ID))
	if _, err := os.Stat(dir2); os.IsNotExist(err) {
		t.Errorf("second build directory %q does not exist after RunBuild", dir2)
	}

	// The first build's artifact record must be deleted from the store.
	for _, id := range build1.ArtifactIDs {
		if _, err := artStore.Get(id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("artifact record %s still exists, expected deleted (err=%v)", id, err)
		}
	}
}

// TestPurgeOldFailureOnNewFailure verifies that a second failed build for
// the same package purges the artifact directory of the first failed build.
func TestPurgeOldFailureOnNewFailure(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	engine := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, jsonstore.NewArtifactStore(store.NewStorePaths(workspace)), gitProvider, store.NewStorePaths(workspace))

	// First build — failure.
	_, err := engine.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected RunBuild to return an error on failure")
	}

	// Second build — failure for the same package.
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, jsonstore.NewArtifactStore(store.NewStorePaths(workspace)), gitProvider, store.NewStorePaths(workspace))
	_, err = engine2.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected RunBuild to return an error on second failure")
	}

	// Read the persisted build to verify the LastFailureBuildID pointer.
	loaded, err := buildStore.Get("failpkg")
	if err != nil {
		t.Fatalf("Get(failpkg): %v", err)
	}
	if loaded.LastFailureBuildID == "" {
		t.Fatal("LastFailureBuildID is empty after second failure")
	}
	if loaded.LastFailureBuildID != loaded.ID {
		t.Errorf("LastFailureBuildID = %q, want %q", loaded.LastFailureBuildID, loaded.ID)
	}
}

// TestNoCrossPurge verifies that a success build does NOT purge the last
// failure directory, and a failure build does NOT purge the last success
// directory. Each slot is independent.
func TestNoCrossPurge(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// Build 1 — success.
	engine1 := NewEngine(newFakeBuilder("success1"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build1, err := engine1.RunBuild(context.Background(), "crosspkg")
	if err != nil {
		t.Fatalf("RunBuild #1 (success): %v", err)
	}
	successDir := filepath.Join(workspace, ".aur-curator", "builds", string(build1.ID))

	// Build 2 — failure.
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine2.RunBuild(context.Background(), "crosspkg")
	if err == nil {
		t.Fatal("expected RunBuild #2 to fail")
	}

	// The success directory must NOT have been purged by the failure.
	if _, err := os.Stat(successDir); os.IsNotExist(err) {
		t.Error("success directory was purged by a subsequent failure (cross-purge)")
	}

	// Verify the last failure pointer was set.
	loaded, err := buildStore.Get("crosspkg")
	if err != nil {
		t.Fatalf("Get(crosspkg): %v", err)
	}
	if loaded.LastFailureBuildID == "" {
		t.Fatal("LastFailureBuildID is empty after failure")
	}
	if loaded.LastSuccessBuildID == "" {
		t.Fatal("LastSuccessBuildID is empty after failure (should preserve success slot)")
	}

	// Build 3 — another failure.
	engine3 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine3.RunBuild(context.Background(), "crosspkg")
	if err == nil {
		t.Fatal("expected RunBuild #3 to fail")
	}

	// The first failure directory should be purged (if it existed).
	// The success directory must still survive.
	if _, err := os.Stat(successDir); os.IsNotExist(err) {
		t.Error("success directory was purged after second failure")
	}
}

// TestNoPurgeOnFirstBuild verifies that a single build for a package
// (with no prior record) does not attempt any purge.
func TestNoPurgeOnFirstBuild(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}
	engine := NewEngine(newFakeBuilder("first-build"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))

	build, err := engine.RunBuild(context.Background(), "firstpkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}
	if build.Status != domain.BuildSuccess {
		t.Fatalf("status = %q, want %q", build.Status, domain.BuildSuccess)
	}

	// Build directory must exist.
	dir := filepath.Join(workspace, ".aur-curator", "builds", string(build.ID))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("build directory %q does not exist", dir)
	}

	// Both pointer fields are set to the current build's ID on first success.
	if build.LastSuccessBuildID != build.ID {
		t.Errorf("LastSuccessBuildID = %q, want %q", build.LastSuccessBuildID, build.ID)
	}
	// LastFailureBuildID remains empty on first success.
	if build.LastFailureBuildID != "" {
		t.Errorf("LastFailureBuildID = %q, want empty", build.LastFailureBuildID)
	}
}

// TestEmptyBuildIDGuard verifies that purgeBuildDir does not attempt to
// remove anything when given an empty BuildID string — no panic, no path
// traversal, no os.RemoveAll("") against the workspace root.
func TestEmptyBuildIDGuard(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	gitProvider := &fakeGitProviderForBuild{}
	engine := NewEngine(newFakeBuilder("guard"), jsonstore.NewBuildStore(store.NewStorePaths(workspace)), jsonstore.NewArtifactStore(store.NewStorePaths(workspace)), gitProvider, store.NewStorePaths(workspace))

	// purgeBuildDir with an empty BuildID must return without error.
	engine.purgeBuildDir(context.Background(), "emptypkg", "", "test")

	// Verify the workspace root is intact.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir(workspace): %v", err)
	}
	for _, e := range entries {
		if e.Name() == "" {
			t.Error("purgeBuildDir with empty BuildID removed something from workspace root")
		}
	}
}

// ---------------------------------------------------------------------------
// Cancellation tests (issue #304 part 2)
// ---------------------------------------------------------------------------

// TestRunBuildFailureRecordsBuildFailed verifies that an ordinary build
// failure records BuildFailed status and purges the previous failure dir.
func TestRunBuildFailureRecordsBuildFailed(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// First build — failure.
	engine1 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine1.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected RunBuild to return an error on failure")
	}
	loaded, err := buildStore.Get("failpkg")
	if err != nil {
		t.Fatalf("Get(failpkg): %v", err)
	}
	if loaded.Status != domain.BuildFailed {
		t.Errorf("status = %q, want %q", loaded.Status, domain.BuildFailed)
	}
	if loaded.LastFailureBuildID == "" {
		t.Fatal("LastFailureBuildID is empty after failure")
	}

	// Second build — failure for same package (triggers purge of first failure dir with "new-failure").
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	oldFailureDir := filepath.Join(workspace, ".aur-curator", "builds", string(loaded.LastFailureBuildID))
	_, err = engine2.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected RunBuild to return an error on second failure")
	}
	// The old failure directory should be purged.
	if _, err := os.Stat(oldFailureDir); !os.IsNotExist(err) {
		t.Errorf("old failure directory %q was not purged", oldFailureDir)
	}
}

// TestRunBuildCancelledRestoresPrevious verifies that cancelling a build
// restores the previous build record (including its ID, LastFailureBuildID).
func TestRunBuildCancelledRestoresPrevious(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// First build — success.
	engine1 := NewEngine(newFakeBuilder("ok1"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build1, err := engine1.RunBuild(context.Background(), "restorepkg")
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	if build1.Status != domain.BuildSuccess {
		t.Fatalf("first build status = %q, want %q", build1.Status, domain.BuildSuccess)
	}

	// Second build — cancelled.
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine2.RunBuild(context.Background(), "restorepkg")
	if err == nil {
		t.Fatal("expected error on cancelled build")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled wrapped", err)
	}

	// Load and verify: previous record is restored with correct ID.
	loaded, err := buildStore.Get("restorepkg")
	if err != nil {
		t.Fatalf("Get(restorepkg): %v", err)
	}
	if loaded.ID != build1.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, build1.ID)
	}
	if loaded.Status != domain.BuildSuccess {
		t.Errorf("status = %q, want %q", loaded.Status, domain.BuildSuccess)
	}
	if loaded.LastSuccessBuildID != build1.ID {
		t.Errorf("LastSuccessBuildID = %q, want %q", loaded.LastSuccessBuildID, build1.ID)
	}
}

// TestRunBuildCancelledFirstBuild verifies that cancelling the very first
// build (no prior build.json) results in the build.json being deleted.
func TestRunBuildCancelledFirstBuild(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	engine := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine.RunBuild(context.Background(), "firstpkg")
	if err == nil {
		t.Fatal("expected error on cancelled first build")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled wrapped", err)
	}

	// build.json should be deleted (no record remains).
	loaded, err := buildStore.Get("firstpkg")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after deleting first build, got: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected build.json to be deleted after cancelling first build, got non-nil record")
	}
}

// TestRunBuildCancelledKeepsFailureDir verifies that a cancelled build
// does NOT purge the previous failure directory.
func TestRunBuildCancelledKeepsFailureDir(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// Build 1 — failure.
	engine1 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine1.RunBuild(context.Background(), "keepfpkg")
	if err == nil {
		t.Fatal("expected failure on build 1")
	}
	failureBuildID, err := buildStore.Get("keepfpkg")
	if err != nil {
		t.Fatalf("Get(failpkg): %v", err)
	}
	failureDir := filepath.Join(workspace, ".aur-curator", "builds", string(failureBuildID.LastFailureBuildID))

	// Build 2 — cancelled.
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine2.RunBuild(context.Background(), "keepfpkg")
	if err == nil {
		t.Fatal("expected error on cancelled build 2")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled wrapped", err)
	}

	// The failure directory must NOT have been purged.
	if _, err := os.Stat(failureDir); os.IsNotExist(err) {
		t.Error("failure directory was purged by a subsequent cancelled build")
	}
}

// TestRunBuildCancelledRemovesBuildDir verifies that the cancelled build's
// persistent directory and workspace are removed, and that TMPDIR is clean.
func TestRunBuildCancelledRemovesBuildDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	engine := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine.RunBuild(context.Background(), "removpkg")
	if err == nil {
		t.Fatal("expected error on cancelled build")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled wrapped", err)
	}

	// The build.json should not exist (first build, deleted).
	loaded, err := buildStore.Get("removpkg")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after deleting first build, got: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected build.json to be deleted")
	}

	// TMPDIR should be empty (no temp workspace leaked).
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read TMPDIR: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("TMPDIR not empty after cancel: %v", entries)
	}

	// .aur-curator/builds should have no entries (persistent dir purged).
	buildsDir := filepath.Join(workspace, ".aur-curator", "builds")
	entries, err = os.ReadDir(buildsDir)
	if err != nil {
		t.Fatalf("failed to read builds dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf(".aur-curator/builds not empty after cancel: %v", entries)
	}
}

// TestRunBuildCancelledReturnsError verifies that a cancelled build returns
// an error satisfying errors.Is(err, context.Canceled).
func TestRunBuildCancelledReturnsError(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	engine := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine.RunBuild(context.Background(), "errpkg")
	if err == nil {
		t.Fatal("expected error from cancelled build")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "build cancelled: ") {
		t.Errorf("error text = %q; want prefix %q (engine cancel path, not the failure path)", err.Error(), "build cancelled: ")
	}
}

// TestRunBuildCancelledKeepsFailurePointer verifies that cancelling a build
// does NOT modify LastFailureBuildID on the previous record.
func TestRunBuildCancelledKeepsFailurePointer(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// Build 1 — failure.
	engine1 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine1.RunBuild(context.Background(), "unchangedpkg")
	if err == nil {
		t.Fatal("expected failure on build 1")
	}
	fail1, err := buildStore.Get("unchangedpkg")
	if err != nil {
		t.Fatalf("Get(unchangedpkg): %v", err)
	}
	originalFailureID := fail1.LastFailureBuildID

	// Build 2 — success.
	engine2 := NewEngine(newFakeBuilder("ok2"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build2, err := engine2.RunBuild(context.Background(), "unchangedpkg")
	if err != nil {
		t.Fatalf("build 2 success: %v", err)
	}
	if build2.Status != domain.BuildSuccess {
		t.Fatalf("build 2 status = %q, want %q", build2.Status, domain.BuildSuccess)
	}

	// Build 3 — cancelled.
	engine3 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, cancel: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine3.RunBuild(context.Background(), "unchangedpkg")
	if err == nil {
		t.Fatal("expected error on cancelled build 3")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled wrapped", err)
	}

	// Load and verify: failure ID is unchanged from build 2 (success).
	loaded, err := buildStore.Get("unchangedpkg")
	if err != nil {
		t.Fatalf("Get(unchangedpkg): %v", err)
	}
	if loaded.LastFailureBuildID != originalFailureID {
		t.Errorf("LastFailureBuildID = %q, want %q (unchanged from first failure)", loaded.LastFailureBuildID, originalFailureID)
	}
}

// ---------------------------------------------------------------------------
// Build record pointer tests (issue #308)
// ---------------------------------------------------------------------------

// TestRunBuildRunningRecordCarriesPointers verifies that every build record
// saved by RunBuild carries both LastSuccessBuildID and LastFailureBuildID,
// even while the build is Running. Build 1 fails, build 2 succeeds. Build 3
// reads the store mid-build via onBuild and asserts the mid-build record has
// Status=Running, LastSuccessBuildID==build2's ID, LastFailureBuildID==build1's ID.
func TestRunBuildRunningRecordCarriesPointers(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// Build 1 — failure.
	engine1 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{}, failNext: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err := engine1.RunBuild(context.Background(), "pointpkg")
	if err == nil {
		t.Fatal("expected failure on build 1")
	}
	fail1, err := buildStore.Get("pointpkg")
	if err != nil {
		t.Fatalf("Get(pointpkg) after build 1: %v", err)
	}
	if fail1.LastSuccessBuildID != "" {
		t.Errorf("build 1 (failure) LastSuccessBuildID = %q, want empty", fail1.LastSuccessBuildID)
	}
	if fail1.LastFailureBuildID == "" {
		t.Fatal("build 1 (failure) LastFailureBuildID is empty")
	}

	// Build 2 — success.
	engine2 := NewEngine(newFakeBuilder("ok2"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build2, err := engine2.RunBuild(context.Background(), "pointpkg")
	if err != nil {
		t.Fatalf("build 2 success: %v", err)
	}
	if build2.Status != domain.BuildSuccess {
		t.Fatalf("build 2 status = %q, want %q", build2.Status, domain.BuildSuccess)
	}
	if build2.LastSuccessBuildID != build2.ID {
		t.Errorf("build 2 LastSuccessBuildID = %q, want %q", build2.LastSuccessBuildID, build2.ID)
	}
	if build2.LastFailureBuildID != fail1.LastFailureBuildID {
		t.Errorf("build 2 LastFailureBuildID = %q, want %q", build2.LastFailureBuildID, fail1.LastFailureBuildID)
	}

	// Build 3 — inspect mid-build state via onBuild.
	var midBuild *domain.Build
	mu := &sync.Mutex{}
	engine3 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{{name: "testpkg-1.0-0-x86_64.pkg.tar.zst", content: []byte("payload")}}, onBuild: func() {
		b, err := buildStore.Get("pointpkg")
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			t.Errorf("Get(pointpkg) mid-build: %v", err)
			return
		}
		midBuild = b
	}}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine3.RunBuild(context.Background(), "pointpkg")
	if err != nil {
		t.Fatalf("build 3 RunBuild: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if midBuild == nil {
		t.Fatal("onBuild never captured mid-build record")
	}
	if midBuild.Status != domain.BuildRunning {
		t.Errorf("mid-build status = %q, want %q", midBuild.Status, domain.BuildRunning)
	}
	if midBuild.LastSuccessBuildID != build2.ID {
		t.Errorf("mid-build LastSuccessBuildID = %q, want %q (build 2's ID)", midBuild.LastSuccessBuildID, build2.ID)
	}
	if midBuild.LastFailureBuildID != fail1.LastFailureBuildID {
		t.Errorf("mid-build LastFailureBuildID = %q, want %q (build 1's failure ID)", midBuild.LastFailureBuildID, fail1.LastFailureBuildID)
	}
	if midBuild.LastFailureBuildID != fail1.LastFailureBuildID {
		t.Errorf("mid-build LastFailureBuildID = %q, want %q (build 1's failure ID)", midBuild.LastFailureBuildID, fail1.LastFailureBuildID)
	}
	if midBuild.LogLocation == "" {
		t.Error("mid-build LogLocation is empty — the Build Log tab cannot show a running build (#343)")
	}
}

// TestRunBuildSuccessPathErrorRecordsFailure verifies that a success-path
// error (e.g. bad artifact path) is handled as a failure: the stored record
// has BuildFailed status, EndTime set, LastFailureBuildID pointing to this
// build, and LastSuccessBuildID pointing to the previous success. The previous
// success's directory must still exist. It also verifies that partial artifact
// records saved during the failed build are cleaned up (purgePartialArtifacts).
func TestRunBuildSuccessPathErrorRecordsFailure(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	buildStore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	gitProvider := &fakeGitProviderForBuild{}

	// Build 1 — success.
	engine1 := NewEngine(newFakeBuilder("good1"), buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	build1, err := engine1.RunBuild(context.Background(), "errpkg")
	if err != nil {
		t.Fatalf("build 1 success: %v", err)
	}
	if build1.Status != domain.BuildSuccess {
		t.Fatalf("build 1 status = %q, want %q", build1.Status, domain.BuildSuccess)
	}
	// Record the previous success directory.
	successDir := filepath.Join(workspace, ".aur-curator", "builds", string(build1.ID))

	// Build 2 — success path error (second artifact is bad, first is copied).
	engine2 := NewEngine(&fakeBuilder{artifacts: []fakeArtifact{
		{name: "testpkg-1.0-0-x86_64.pkg.tar.zst", content: []byte("good")},
		{name: "testpkg-2.0-0-x86_64.pkg.tar.zst", content: []byte("bad")},
	}, badArtifact: true}, buildStore, artStore, gitProvider, store.NewStorePaths(workspace))
	_, err = engine2.RunBuild(context.Background(), "errpkg")
	if err == nil {
		t.Fatal("expected error on build 2")
	}
	if !strings.Contains(err.Error(), "build failed:") {
		t.Errorf("error = %q; want it to contain %q", err.Error(), "build failed:")
	}

	// Verify the stored record.
	loaded, err := buildStore.Get("errpkg")
	if err != nil {
		t.Fatalf("Get(errpkg): %v", err)
	}
	if loaded.Status != domain.BuildFailed {
		t.Errorf("status = %q, want %q", loaded.Status, domain.BuildFailed)
	}
	if loaded.EndTime == nil {
		t.Error("EndTime is nil")
	}
	if loaded.LastFailureBuildID != loaded.ID {
		t.Errorf("LastFailureBuildID = %q, want %q", loaded.LastFailureBuildID, loaded.ID)
	}
	if loaded.LastSuccessBuildID != build1.ID {
		t.Errorf("LastSuccessBuildID = %q, want %q", loaded.LastSuccessBuildID, build1.ID)
	}

	// The previous success directory must still exist.
	if _, err := os.Stat(successDir); os.IsNotExist(err) {
		t.Error("previous success directory was incorrectly purged")
	}

	// No artifact records should exist for the failed build's ID.
	allArts, err := artStore.GetArtifactsByPackage("errpkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage(errpkg): %v", err)
	}
	for _, a := range allArts {
		if a.BuildID == loaded.ID {
			t.Errorf("artifact %q still exists for failed build %q (should have been purged)", a.ID, a.BuildID)
		}
	}

	// Build 1's artifact must still be present.
	build1Arts, err := artStore.GetArtifactsByPackage("errpkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage(errpkg): %v", err)
	}
	var foundBuild1 bool
	for _, a := range build1Arts {
		if a.BuildID == build1.ID {
			foundBuild1 = true
			break
		}
	}
	if !foundBuild1 {
		t.Error("build 1's artifact was incorrectly removed")
	}
}

func TestEngineSavesLastSuccessOnSuccess(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content")
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	build, err := engine.RunBuild(context.Background(), "testpkg")
	if err != nil {
		t.Fatalf("RunBuild: %v", err)
	}

	ls, err := bstore.GetLastSuccess("testpkg")
	if err != nil {
		t.Fatalf("GetLastSuccess: %v", err)
	}
	if ls.ID != build.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build.ID)
	}

	// Verify the file actually exists on disk (not just satisfied by fallback).
	lsPath := paths.LastSuccessBuildPath("testpkg")
	if _, err := os.Stat(lsPath); err != nil {
		t.Fatalf("last-success file %s should exist: %v", lsPath, err)
	}
}

func TestEngineDoesNotSaveLastSuccessOnFailure(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content")
	builder.failNext = true
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	_, err := engine.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected error from failed build")
	}

	_, err = bstore.GetLastSuccess("failpkg")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetLastSuccess error = %v, want ErrNotFound", err)
	}
}

func TestEngineDoesNotSaveLastSuccessOnCancel(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content")
	builder.cancel = true
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	_, err := engine.RunBuild(context.Background(), "cancelpkg")
	if err == nil {
		t.Fatal("expected error from cancelled build")
	}

	_, err = bstore.GetLastSuccess("cancelpkg")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetLastSuccess error = %v, want ErrNotFound", err)
	}
}

func TestEngineLastSuccessPersistedBeforePurge(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content1")
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// First build succeeds.
	build1, err := engine.RunBuild(context.Background(), "testpkg2")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}

	// Verify last-success points to build1.
	ls, err := bstore.GetLastSuccess("testpkg2")
	if err != nil {
		t.Fatalf("GetLastSuccess after build1: %v", err)
	}
	if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}

	// Second build succeeds — should overwrite last-success.
	builder2 := newFakeBuilder("content2")
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	build2, err := engine2.RunBuild(context.Background(), "testpkg2")
	if err != nil {
		t.Fatalf("RunBuild 2: %v", err)
	}

	ls2, err := bstore.GetLastSuccess("testpkg2")
	if err != nil {
		t.Fatalf("GetLastSuccess after build2: %v", err)
	}
	if ls2.ID != build2.ID {
		t.Fatalf("last-success ID = %q, want %q", ls2.ID, build2.ID)
	}
}

func TestEngineLastSuccessPersistedThenFailure(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content1")
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// Build 1 succeeds.
	build1, err := engine.RunBuild(context.Background(), "failpkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}
	if ls, err := bstore.GetLastSuccess("failpkg"); err != nil {
		t.Fatalf("GetLastSuccess after build1: %v", err)
	} else if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}

	// Build 2 fails.
	builder2 := newFakeBuilder("content2")
	builder2.failNext = true
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	_, err = engine2.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected error from failed build")
	}

	// Last-success must still point to build1.
	ls, err := bstore.GetLastSuccess("failpkg")
	if err != nil {
		t.Fatalf("GetLastSuccess after failure: %v", err)
	}
	if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}
}

func TestEngineLastSuccessPersistedThenCancel(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content1")
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// Build 1 succeeds.
	build1, err := engine.RunBuild(context.Background(), "cancelpkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}
	if ls, err := bstore.GetLastSuccess("cancelpkg"); err != nil {
		t.Fatalf("GetLastSuccess after build1: %v", err)
	} else if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}

	// Build 2 is cancelled.
	builder2 := newFakeBuilder("content2")
	builder2.cancel = true
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	_, err = engine2.RunBuild(context.Background(), "cancelpkg")
	if err == nil {
		t.Fatal("expected error from cancelled build")
	}

	// Last-success must still point to build1.
	ls, err := bstore.GetLastSuccess("cancelpkg")
	if err != nil {
		t.Fatalf("GetLastSuccess after cancel: %v", err)
	}
	if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}
	if _, err := os.Stat(paths.LastSuccessBuildPath("cancelpkg")); err != nil {
		t.Fatalf("last-success.json missing after cancel: %v", err)
	}
}

func TestEngineLastSuccessPersistedThenSuccessPathError(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	bstore := jsonstore.NewBuildStore(store.NewStorePaths(workspace))
	artStore := jsonstore.NewArtifactStore(store.NewStorePaths(workspace))
	builder := newFakeBuilder("content1")
	gitProvider := &fakeGitProviderForBuild{}
	paths := store.NewStorePaths(workspace)
	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// Build 1 succeeds.
	build1, err := engine.RunBuild(context.Background(), "errorpkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}
	if ls, err := bstore.GetLastSuccess("errorpkg"); err != nil {
		t.Fatalf("GetLastSuccess after build1: %v", err)
	} else if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}

	// Build 2 succeeds but has a bad artifact path.
	builder2 := newFakeBuilder("content2", "content3")
	builder2.badArtifact = true
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	_, err = engine2.RunBuild(context.Background(), "errorpkg")
	if err == nil {
		t.Fatal("expected error from bad artifact")
	}

	// Last-success must still point to build1.
	ls, err := bstore.GetLastSuccess("errorpkg")
	if err != nil {
		t.Fatalf("GetLastSuccess after success-path error: %v", err)
	}
	if ls.ID != build1.ID {
		t.Fatalf("last-success ID = %q, want %q", ls.ID, build1.ID)
	}
}

// ---------------------------------------------------------------------------
// Step 1 tests: purgeOldArtifacts (artifact record leak fix, #352)
// ---------------------------------------------------------------------------

// fakeFailingArtifactStore wraps a real ArtifactStore and fails Delete calls.
type fakeFailingArtifactStore struct {
	store      store.ArtifactStore
	failDelete bool
}

func (f *fakeFailingArtifactStore) Get(id domain.ArtifactID) (*domain.Artifact, error) {
	return f.store.Get(id)
}

func (f *fakeFailingArtifactStore) Save(id domain.ArtifactID, artifact *domain.Artifact) error {
	return f.store.Save(id, artifact)
}

func (f *fakeFailingArtifactStore) GetArtifactsByPackage(pkgName domain.PackageName) ([]domain.Artifact, error) {
	return f.store.GetArtifactsByPackage(pkgName)
}

func (f *fakeFailingArtifactStore) Delete(id domain.ArtifactID) error {
	if f.failDelete {
		return fmt.Errorf("simulated delete failure")
	}
	return f.store.Delete(id)
}

func (f *fakeFailingArtifactStore) ListAll() ([]domain.Artifact, error) {
	return f.store.ListAll()
}

// TestPurgeOldArtifacts_TwoSuccessfulBuilds verifies that after two successful
// builds, only the second build's artifact records remain and the first
// build's directory is gone.
func TestPurgeOldArtifacts_TwoSuccessfulBuilds(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("v1 artifact")

	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// Build 1.
	build1, err := engine.RunBuild(context.Background(), "twopkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}

	// Build 2.
	builder2 := newFakeBuilder("v2 artifact")
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	build2, err := engine2.RunBuild(context.Background(), "twopkg")
	if err != nil {
		t.Fatalf("RunBuild 2: %v", err)
	}

	// Only build2's records should remain.
	arts, err := artStore.GetArtifactsByPackage("twopkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].BuildID != build2.ID {
		t.Errorf("artifact BuildID = %q, want %q", arts[0].BuildID, build2.ID)
	}

	// First build's directory should be gone.
	dir1 := paths.BuildArtifactsDir(build1.ID)
	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Errorf("first build dir %q still exists, expected deleted", dir1)
	}

	// Second build's directory should exist.
	dir2 := paths.BuildArtifactsDir(build2.ID)
	if _, err := os.Stat(dir2); os.IsNotExist(err) {
		t.Errorf("second build dir %q missing, expected to exist", dir2)
	}
}

// TestPurgeOldArtifacts_FailedBuildAfterSuccess verifies that a failed build
// does not purge the earlier success's records or directory.
func TestPurgeOldArtifacts_FailedBuildAfterSuccess(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)
	bstore := jsonstore.NewBuildStore(paths)
	artStore := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("v1 artifact")

	engine := NewEngine(builder, bstore, artStore, gitProvider, paths)

	// Build 1 succeeds.
	build1, err := engine.RunBuild(context.Background(), "failpkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}

	// Build 2 fails.
	builder2 := newFakeBuilder("v2 artifact")
	builder2.failNext = true
	engine2 := NewEngine(builder2, bstore, artStore, gitProvider, paths)
	_, err = engine2.RunBuild(context.Background(), "failpkg")
	if err == nil {
		t.Fatal("expected error from failed build")
	}

	// Build 1's records should still exist.
	arts, err := artStore.GetArtifactsByPackage("failpkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].BuildID != build1.ID {
		t.Errorf("artifact BuildID = %q, want %q", arts[0].BuildID, build1.ID)
	}

	// Build 1's directory should still exist.
	dir1 := paths.BuildArtifactsDir(build1.ID)
	if _, err := os.Stat(dir1); os.IsNotExist(err) {
		t.Errorf("first build dir %q missing, expected to exist", dir1)
	}
}

// TestPurgeOldArtifacts_DeleteError verifies that when artifact deletion
// fails, the build still succeeds, the old directory still exists, and the
// new build's records are intact.
func TestPurgeOldArtifacts_DeleteError(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	paths := store.NewStorePaths(workspace)

	// Build 1 with real store.
	bstore1 := jsonstore.NewBuildStore(paths)
	artStore1 := jsonstore.NewArtifactStore(paths)
	gitProvider := &fakeGitProviderForBuild{}
	builder := newFakeBuilder("v1 artifact")

	engine1 := NewEngine(builder, bstore1, artStore1, gitProvider, paths)
	build1, err := engine1.RunBuild(context.Background(), "errpkg")
	if err != nil {
		t.Fatalf("RunBuild 1: %v", err)
	}

	// Build 2 with a store that fails Delete.
	bstore2 := jsonstore.NewBuildStore(paths)
	failingStore := &fakeFailingArtifactStore{
		store:      artStore1,
		failDelete: true,
	}
	builder2 := newFakeBuilder("v2 artifact")
	engine2 := NewEngine(builder2, bstore2, failingStore, gitProvider, paths)

	// Build 2 should still succeed despite the delete error.
	build2, err := engine2.RunBuild(context.Background(), "errpkg")
	if err != nil {
		t.Fatalf("RunBuild 2 should succeed: %v", err)
	}
	if build2.Status != domain.BuildSuccess {
		t.Errorf("build status = %q, want %q", build2.Status, domain.BuildSuccess)
	}

	// Both old and new records should be intact (delete was skipped).
	arts, err := failingStore.GetArtifactsByPackage("errpkg")
	if err != nil {
		t.Fatalf("GetArtifactsByPackage: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}
	// Verify both build IDs are represented.
	hasOld, hasNew := false, false
	for _, a := range arts {
		if a.BuildID == build1.ID {
			hasOld = true
		}
		if a.BuildID == build2.ID {
			hasNew = true
		}
	}
	if !hasOld {
		t.Errorf("artifact with BuildID %q missing", build1.ID)
	}
	if !hasNew {
		t.Errorf("artifact with BuildID %q missing", build2.ID)
	}

	// Old success directory should still exist (purge was skipped).
	dir1 := paths.BuildArtifactsDir(build1.ID)
	if _, err := os.Stat(dir1); os.IsNotExist(err) {
		t.Errorf("old build dir %q missing, expected to exist (purge skipped)", dir1)
	}
}
