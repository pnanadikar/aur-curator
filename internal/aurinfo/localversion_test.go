package aurinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// mockProvider implements git.Provider with configurable per-call behavior.
type mockProvider struct {
	headCommit    string
	headErr       error
	fileContent   string
	fileErr       error
	isMergedVal   bool
	isMergedErr   error
	importCalls   []domain.PackageName
	fetchCalls    []domain.PackageName
	statusCalls   []domain.PackageName
	diffCalls     []domain.PackageName
	mergeCalls    []domain.PackageName
	metadataCalls []domain.PackageName
	cloneCalls    []string
	behindCalls   []domain.PackageName
	upstreamCalls []domain.PackageName
}

func (m *mockProvider) Import(ctx context.Context, packageName domain.PackageName) error {
	m.importCalls = append(m.importCalls, packageName)
	return nil
}
func (m *mockProvider) Fetch(ctx context.Context, packageName domain.PackageName) error {
	m.fetchCalls = append(m.fetchCalls, packageName)
	return nil
}
func (m *mockProvider) Status(ctx context.Context, packageName domain.PackageName) (*git.RepositoryStatus, error) {
	m.statusCalls = append(m.statusCalls, packageName)
	return &git.RepositoryStatus{}, nil
}
func (m *mockProvider) CommitsBehind(ctx context.Context, packageName domain.PackageName) (int, error) {
	m.behindCalls = append(m.behindCalls, packageName)
	return 0, nil
}
func (m *mockProvider) UpstreamCommit(ctx context.Context, packageName domain.PackageName) (string, error) {
	m.upstreamCalls = append(m.upstreamCalls, packageName)
	return "", nil
}
func (m *mockProvider) IsMerged(ctx context.Context, packageName domain.PackageName, reviewedCommit string) (bool, error) {
	return m.isMergedVal, m.isMergedErr
}
func (m *mockProvider) Diff(ctx context.Context, packageName domain.PackageName) (*git.GitDiff, error) {
	m.diffCalls = append(m.diffCalls, packageName)
	return &git.GitDiff{}, nil
}
func (m *mockProvider) ReviewDiff(ctx context.Context, packageName domain.PackageName, base, upstream string) (*git.GitDiff, error) {
	return &git.GitDiff{}, nil
}
func (m *mockProvider) Merge(ctx context.Context, packageName domain.PackageName, commit string) error {
	m.mergeCalls = append(m.mergeCalls, packageName)
	return nil
}
func (m *mockProvider) LocalOnlyCommits(ctx context.Context, packageName domain.PackageName) (int, error) {
	return 0, nil
}
func (m *mockProvider) Metadata(ctx context.Context, packageName domain.PackageName) (*git.RepositoryMetadata, error) {
	m.metadataCalls = append(m.metadataCalls, packageName)
	return &git.RepositoryMetadata{}, nil
}
func (m *mockProvider) CloneAt(ctx context.Context, pkgName domain.PackageName, commit string, destDir string) error {
	m.cloneCalls = append(m.cloneCalls, commit)
	return nil
}
func (m *mockProvider) LocalHeadCommit(ctx context.Context, packageName domain.PackageName) (string, error) {
	return m.headCommit, m.headErr
}
func (m *mockProvider) FileAtCommit(ctx context.Context, packageName domain.PackageName, commit, path string) (string, error) {
	return m.fileContent, m.fileErr
}

// mockReviewStore implements domain.ReviewStore with configurable per-package behavior.
type mockReviewStore struct {
	reviews map[domain.PackageName]*domain.Review
	err     error
}

func (m *mockReviewStore) Get(name domain.PackageName) (*domain.Review, error) {
	if m.err != nil {
		return nil, m.err
	}
	r, ok := m.reviews[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r, nil
}
func (m *mockReviewStore) Save(name domain.PackageName, review *domain.Review) error {
	m.reviews[name] = review
	return nil
}
func (m *mockReviewStore) Delete(name domain.PackageName) error {
	delete(m.reviews, name)
	return nil
}

func TestNewGitLocalVersionResolver_ReviewPending(t *testing.T) {
	provider := &mockProvider{
		isMergedVal: false,
		fileContent: "pkgname=testpkg\npkgver=1.0\npkgrel=2\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewPending,
			},
		},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for ReviewPending with valid PKGBUILD")
	}
	if ver != "1.0-2" {
		t.Errorf("ver = %q, want %q", ver, "1.0-2")
	}
	// Verify it read from UpstreamCommit, not HEAD.
	if provider.fileContent != "pkgname=testpkg\npkgver=1.0\npkgrel=2\n" {
		t.Error("expected FileAtCommit to be called with review content")
	}
}

func TestNewGitLocalVersionResolver_Approved(t *testing.T) {
	provider := &mockProvider{
		headCommit:  "def456",
		fileContent: "pkgname=testpkg\npkgver=2.0\npkgrel=1\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for Approved with valid PKGBUILD")
	}
	if ver != "2.0-1" {
		t.Errorf("ver = %q, want %q", ver, "2.0-1")
	}
}

func TestNewGitLocalVersionResolver_Merged(t *testing.T) {
	provider := &mockProvider{
		headCommit:  "def456",
		fileContent: "pkgname=testpkg\npkgver=2.0\npkgrel=1\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	provider.isMergedVal = true
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for Merged with valid PKGBUILD")
	}
	if ver != "2.0-1" {
		t.Errorf("ver = %q, want %q", ver, "2.0-1")
	}
}

func TestNewGitLocalVersionResolver_Built(t *testing.T) {
	provider := &mockProvider{
		headCommit:  "def456",
		fileContent: "pkgname=testpkg\npkgver=2.0\npkgrel=1\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	provider.isMergedVal = true
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for Built with valid PKGBUILD")
	}
	if ver != "2.0-1" {
		t.Errorf("ver = %q, want %q", ver, "2.0-1")
	}
}

func TestNewGitLocalVersionResolver_Rejected(t *testing.T) {
	provider := &mockProvider{
		headCommit:  "def456",
		fileContent: "pkgname=testpkg\npkgver=3.0\npkgrel=1\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewRejected,
			},
		},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for Rejected with valid PKGBUILD")
	}
	if ver != "3.0-1" {
		t.Errorf("ver = %q, want %q", ver, "3.0-1")
	}
}

func TestNewGitLocalVersionResolver_Published(t *testing.T) {
	provider := &mockProvider{
		headCommit:  "def456",
		fileContent: "pkgname=testpkg\npkgver=3.0\npkgrel=1\n",
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	provider.isMergedVal = true
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	ver, unknown := resolver(ctx, "testpkg")
	if unknown {
		t.Fatal("expected unknown = false for Published with valid PKGBUILD")
	}
	if ver != "3.0-1" {
		t.Errorf("ver = %q, want %q", ver, "3.0-1")
	}
}

func TestNewGitLocalVersionResolver_ReviewNotFound(t *testing.T) {
	provider := &mockProvider{}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	_, unknown := resolver(ctx, "testpkg")
	if !unknown {
		t.Error("expected unknown = true when review not found")
	}
}

func TestNewGitLocalVersionResolver_ReviewStoreError(t *testing.T) {
	provider := &mockProvider{}
	store := &mockReviewStore{
		err: fmt.Errorf("store error"),
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	_, unknown := resolver(ctx, "testpkg")
	if !unknown {
		t.Error("expected unknown = true when review store returns error")
	}
}

func TestNewGitLocalVersionResolver_HeadCommitError(t *testing.T) {
	provider := &mockProvider{
		headErr: fmt.Errorf("git error"),
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	_, unknown := resolver(ctx, "testpkg")
	if !unknown {
		t.Error("expected unknown = true when LocalHeadCommit fails")
	}
}

func TestNewGitLocalVersionResolver_FileAtCommitError(t *testing.T) {
	provider := &mockProvider{
		headCommit: "def456",
		fileErr:    fmt.Errorf("file error"),
	}
	store := &mockReviewStore{
		reviews: map[domain.PackageName]*domain.Review{
			"testpkg": {
				PackageName:    "testpkg",
				UpstreamCommit: "abc123",
				Status:         domain.ReviewApproved,
			},
		},
	}
	resolver := NewGitLocalVersionResolver(provider, store)
	ctx := context.Background()
	_, unknown := resolver(ctx, "testpkg")
	if !unknown {
		t.Error("expected unknown = true when FileAtCommit fails")
	}
}

// TestNewGitLocalVersionResolver_BucketStates verifies that Approved,
// Merged, Built, Confirmed, Rejected and Published all read from HEAD,
// while ReviewPending reads from UpstreamCommit.
func TestNewGitLocalVersionResolver_BucketStates(t *testing.T) {
	pkg := domain.PackageName("testpkg")
	pkgbuild := "pkgname=testpkg\npkgver=1.0\npkgrel=1\n"

	testCases := []struct {
		name         string
		status       domain.ReviewStatus
		isMerged     bool
		expectBucket string
	}{
		{"ReviewPending", domain.ReviewPending, false, "bucket2"},
		{"Approved", domain.ReviewApproved, false, "bucket3"},
		{"Merged", domain.ReviewApproved, true, "bucket3"},
		{"Rejected", domain.ReviewRejected, false, "bucket3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &mockProvider{
				headCommit:  "head123",
				fileContent: pkgbuild,
				isMergedVal: tc.isMerged,
			}
			store := &mockReviewStore{
				reviews: map[domain.PackageName]*domain.Review{
					pkg: {
						PackageName:    pkg,
						UpstreamCommit: "abc123",
						Status:         tc.status,
					},
				},
			}
			resolver := NewGitLocalVersionResolver(provider, store)
			ctx := context.Background()
			ver, unknown := resolver(ctx, pkg)
			if unknown {
				t.Fatalf("expected unknown = false for %s", tc.name)
			}
			if ver != "1.0-1" {
				t.Errorf("ver = %q, want %q", ver, "1.0-1")
			}
		})
	}
}

// bulkCheckDiskResolver creates a LocalVersionResolver that reads from
// a filesystem directory — matching the old NewClient behavior for
// existing BulkCheck tests.
func bulkCheckDiskResolver(rootPath string) LocalVersionResolver {
	return func(ctx context.Context, pkg domain.PackageName) (string, bool) {
		pkgbuildPath := filepath.Join(rootPath, string(pkg), "PKGBUILD")
		data, err := os.ReadFile(pkgbuildPath)
		if err != nil {
			return "", true
		}
		return ExtractFullVersion(string(data))
	}
}

func TestBulkCheck_SinglePackage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := aurRPCResponse{
			ResultCount: 1,
			Type:        "info",
			Version:     5,
			Results: []aurResult{
				{Name: "testpkg", Version: "1.0-2"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "testpkg"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "testpkg", "PKGBUILD"), []byte(`
pkgname=testpkg
pkgver=1.0
pkgrel=2
`), 0644)

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(tmpDir))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"testpkg"})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Pairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(result.Pairs))
	}
	pair := result.Pairs[0]
	if pair.AURVersion != "1.0-2" {
		t.Errorf("AURVersion = %q, want %q", pair.AURVersion, "1.0-2")
	}
	if pair.LocalVersion != "1.0-2" {
		t.Errorf("LocalVersion = %q, want %q", pair.LocalVersion, "1.0-2")
	}
	if pair.AURStale {
		t.Error("expected AURStale = false (local 1.0-2 vs aur 1.0-2)")
	}
	if pair.AURUnknown {
		t.Error("expected AURUnknown = false")
	}
	if pair.LocalUnknown {
		t.Error("expected LocalUnknown = false")
	}
}

func TestBulkCheck_MultiplePackages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := aurRPCResponse{
			ResultCount: 3,
			Type:        "info",
			Version:     5,
			Results: []aurResult{
				{Name: "pkg1", Version: "2.0-1"},
				{Name: "pkg2", Version: "3.1-4"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	for _, name := range []string{"pkg1", "pkg2", "pkg3"} {
		_ = os.MkdirAll(filepath.Join(tmpDir, name), 0755)
		_ = os.WriteFile(filepath.Join(tmpDir, name, "PKGBUILD"), []byte(fmt.Sprintf("pkgname=%s\npkgver=1.0\npkgrel=1\n", name)), 0644)
	}

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(tmpDir))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"pkg1", "pkg2", "pkg3"})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Pairs) != 3 {
		t.Fatalf("got %d pairs, want 3", len(result.Pairs))
	}

	if result.Pairs[0].AURVersion != "2.0-1" || result.Pairs[0].LocalVersion != "1.0-1" || !result.Pairs[0].AURStale {
		t.Error("pkg1: expected stale")
	}
	if result.Pairs[1].AURVersion != "3.1-4" || result.Pairs[1].LocalVersion != "1.0-1" || !result.Pairs[1].AURStale {
		t.Error("pkg2: expected stale")
	}
	if !result.Pairs[2].AURUnknown || result.Pairs[2].AURVersion != "" {
		t.Error("pkg3: expected AURUnknown = true, AURVersion = \"\"")
	}
}

func TestBulkCheck_NetworkFailure(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", 100*time.Millisecond, bulkCheckDiskResolver(t.TempDir()))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"testpkg"})

	if result.Err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !errors.Is(result.Err, ErrAURClientUnreachable) {
		t.Fatalf("expected ErrAURClientUnreachable, got: %v", result.Err)
	}
}

func TestBulkCheck_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(t.TempDir()))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"testpkg"})

	if result.Err == nil {
		t.Fatal("expected error for HTTP 502")
	}
	if !errors.Is(result.Err, ErrAURClientUnreachable) {
		t.Fatalf("expected ErrAURClientUnreachable, got: %v", result.Err)
	}
}

func TestBulkCheck_MalformedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(t.TempDir()))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"testpkg"})

	if result.Err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !errors.Is(result.Err, ErrAURClientUnreachable) {
		t.Fatalf("expected ErrAURClientUnreachable, got: %v", result.Err)
	}
}

func TestBulkCheck_EmptyPackages(t *testing.T) {
	client := NewClient("http://example.com", 5*time.Second, bulkCheckDiskResolver(t.TempDir()))
	result := client.BulkCheck(context.Background(), []domain.PackageName{})

	if result.Err != nil {
		t.Fatalf("unexpected error for empty package list: %v", result.Err)
	}
	if len(result.Pairs) != 0 {
		t.Fatalf("got %d pairs, want 0", len(result.Pairs))
	}
}

func TestBulkCheck_MissingPKGBUILD(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := aurRPCResponse{
			ResultCount: 1,
			Type:        "info",
			Version:     5,
			Results: []aurResult{
				{Name: "nopkgbuild", Version: "1.0-1"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(tmpDir))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"nopkgbuild"})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Pairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(result.Pairs))
	}
	pair := result.Pairs[0]
	if pair.LocalVersion != "" {
		t.Errorf("expected empty LocalVersion for missing PKGBUILD, got %q", pair.LocalVersion)
	}
	if !pair.LocalUnknown {
		t.Error("expected LocalUnknown = true for missing PKGBUILD")
	}
	if pair.AURStale {
		t.Error("expected AURStale = false when local version is unknown")
	}
}

func TestBulkCheck_URLContainsAllArgs(t *testing.T) {
	var receivedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		resp := aurRPCResponse{ResultCount: 0, Type: "info", Version: 5, Results: nil}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(t.TempDir()))
	pkgs := []domain.PackageName{"alpha", "beta", "gamma"}
	_ = client.BulkCheck(context.Background(), pkgs)

	for _, pkg := range pkgs {
		if !contains(receivedURL, fmt.Sprintf("arg[]=%s", pkg)) {
			t.Errorf("URL %q missing arg[]=%s", receivedURL, pkg)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAURStale_Ties(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := aurRPCResponse{
			ResultCount: 1,
			Type:        "info",
			Version:     5,
			Results:     []aurResult{{Name: "tiepkg", Version: "1.0-1"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, "tiepkg"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "tiepkg", "PKGBUILD"), []byte("pkgname=tiepkg\npkgver=1.0-1\n"), 0644)

	client := NewClient(ts.URL, 5*time.Second, bulkCheckDiskResolver(tmpDir))
	result := client.BulkCheck(context.Background(), []domain.PackageName{"tiepkg"})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Pairs[0].AURStale {
		t.Error("expected AURStale = false for version tie")
	}
}
