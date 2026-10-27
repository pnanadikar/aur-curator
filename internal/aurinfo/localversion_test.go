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
	"strings"
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
	headCalls     int      // LocalHeadCommit calls
	mergedCalls   int      // IsMerged calls
	fileCommits   []string // commit argument of each FileAtCommit call
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
	m.mergedCalls++
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
	m.headCalls++
	return m.headCommit, m.headErr
}
func (m *mockProvider) FileAtCommit(ctx context.Context, packageName domain.PackageName, commit, path string) (string, error) {
	m.fileCommits = append(m.fileCommits, commit)
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

// #426 — the local version is read at the reviewed commit
// (Review.UpstreamCommit) whatever the review state; local master is never
// read (workflow.md § Local Version).
func TestNewGitLocalVersionResolver_ReadsReviewedCommit(t *testing.T) {
	pkg := domain.PackageName("testpkg")
	tests := []struct {
		name     string
		status   domain.ReviewStatus
		isMerged bool
	}{
		{"Pending", domain.ReviewPending, false},
		{"Approved, not merged", domain.ReviewApproved, false},
		{"Approved and merged", domain.ReviewApproved, true},
		{"Rejected", domain.ReviewRejected, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockProvider{
				headCommit:  "head123", // local master; must not be read
				isMergedVal: tt.isMerged,
				fileContent: "pkgname=testpkg\npkgver=1.11.0\npkgrel=1\n",
			}
			store := &mockReviewStore{reviews: map[domain.PackageName]*domain.Review{
				pkg: {PackageName: pkg, UpstreamCommit: "reviewed456", Status: tt.status},
			}}

			ver, err := NewGitLocalVersionResolver(provider, store)(context.Background(), pkg)

			if err != nil || ver != "1.11.0-1" {
				t.Errorf("got (%q, %v), want (\"1.11.0-1\", nil)", ver, err)
			}
			if want := []string{"reviewed456"}; !equalStrings(provider.fileCommits, want) {
				t.Errorf("FileAtCommit commits = %v, want %v", provider.fileCommits, want)
			}
			if provider.headCalls != 0 {
				t.Errorf("LocalHeadCommit called %d times, want 0", provider.headCalls)
			}
			if provider.mergedCalls != 0 {
				t.Errorf("IsMerged called %d times, want 0", provider.mergedCalls)
			}
		})
	}
}

// #402 — an unknown local version carries its reason.
func TestNewGitLocalVersionResolver_Unknown(t *testing.T) {
	pkg := domain.PackageName("testpkg")
	reviewed := func(commit string) *mockReviewStore {
		return &mockReviewStore{reviews: map[domain.PackageName]*domain.Review{
			pkg: {PackageName: pkg, UpstreamCommit: commit, Status: domain.ReviewApproved},
		}}
	}
	storeErr := errors.New("store error")
	fileErr := errors.New("file error")
	const static = "pkgname=testpkg\npkgver=1.0\npkgrel=1\n"
	// Same shape as TestExtractFullVersion_DynamicPkgver.
	const dynamic = "pkgver() {\n  git describe\n}\npkgrel=1\n"
	tests := []struct {
		name      string
		store     *mockReviewStore
		content   string
		fileErr   error
		wantErr   error  // matched with errors.Is
		wantMsg   string // substring of the message
		wantReads int
	}{
		{"no review", &mockReviewStore{reviews: map[domain.PackageName]*domain.Review{}}, static, nil, ErrNoReview, "no review record", 0},
		{"review store error", &mockReviewStore{err: storeErr}, static, nil, storeErr, "read review: store error", 0},
		{"review without UpstreamCommit", reviewed(""), static, nil, ErrNoUpstreamCommit, "no upstream commit", 0},
		{"PKGBUILD read fails", reviewed("reviewed456789"), static, fileErr, fileErr, "read PKGBUILD at reviewe: file error", 1},
		{"pkgver() function", reviewed("reviewed456789"), dynamic, nil, ErrVersionNotReadable, "pkgver()", 1},
		{"no pkgver", reviewed("reviewed456789"), "pkgname=testpkg\n", nil, ErrVersionNotReadable, "pkgver()", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockProvider{fileContent: tt.content, fileErr: tt.fileErr}
			ver, err := NewGitLocalVersionResolver(provider, tt.store)(context.Background(), pkg)
			if ver != "" {
				t.Errorf("version = %q, want \"\"", ver)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want errors.Is %v", err, tt.wantErr)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %v, want message containing %q", err, tt.wantMsg)
			}
			if len(provider.fileCommits) != tt.wantReads {
				t.Errorf("FileAtCommit called %d times, want %d", len(provider.fileCommits), tt.wantReads)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// bulkCheckDiskResolver creates a LocalVersionResolver that reads from
// a filesystem directory — matching the old NewClient behavior for
// existing BulkCheck tests.
func bulkCheckDiskResolver(rootPath string) LocalVersionResolver {
	return func(ctx context.Context, pkg domain.PackageName) (string, error) {
		pkgbuildPath := filepath.Join(rootPath, string(pkg), "PKGBUILD")
		data, err := os.ReadFile(pkgbuildPath)
		if err != nil {
			return "", err
		}
		ver, unknown := ExtractFullVersion(string(data))
		if unknown {
			return "", ErrVersionNotReadable
		}
		return ver, nil
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
	if pair.LocalErr != nil {
		t.Errorf("LocalErr = %v, want nil", pair.LocalErr)
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
	if !errors.Is(pair.LocalErr, os.ErrNotExist) {
		t.Errorf("LocalErr = %v, want the resolver's error (os.ErrNotExist)", pair.LocalErr)
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

// blockingDoer never answers: Do returns only once the request's context
// is done, standing in for a stalled AUR endpoint.
type blockingDoer struct{}

func (blockingDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// TestFetchAURVersions_ClientTimeout guards #416: the client's own timeout
// bounds the request even when the caller's context has no deadline.
func TestFetchAURVersions_ClientTimeout(t *testing.T) {
	client := NewClient("http://aur.invalid/rpc/", 50*time.Millisecond, bulkCheckDiskResolver(t.TempDir()))
	client.client = blockingDoer{}

	done := make(chan error, 1)
	go func() {
		_, err := client.FetchAURVersions(context.Background(), []domain.PackageName{"testpkg"})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrAURClientUnreachable) {
			t.Fatalf("expected ErrAURClientUnreachable, got: %v", err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FetchAURVersions did not return: client timeout not applied")
	}
}
