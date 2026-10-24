package git

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestImportSequence(t *testing.T) {
	r := &fakeRunner{}
	c := &fakeCatalog{}
	f := &fakeForge{cloneURL: "https://gitea.example.com/aur/yay.git"}
	p := &provider{catalog: c, runner: r, forge: f, client: mockAUROk()}

	repo, err := c.RepositoryPath(domain.PackageName("yay"))
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Import(context.Background(), domain.PackageName("yay")); err != nil {
		t.Fatal(err)
	}

	if len(f.created) != 1 || f.created[0] != "yay" {
		t.Fatal("repository not created")
	}

	if len(r.calls) != 10 {
		t.Fatalf("expected 10 git calls, got %d", len(r.calls))
	}

	if r.calls[0].Dir != filepath.Dir(repo) {
		t.Fatalf("expected clone step to run in %q, got %q", filepath.Dir(repo), r.calls[0].Dir)
	}
	for i := 1; i < len(r.calls); i++ {
		if r.calls[i].Dir != repo {
			t.Fatalf("expected step %d to run in %q, got %q", i, repo, r.calls[i].Dir)
		}
	}

	remoteRenameCall := r.calls[1]
	if len(remoteRenameCall.Args) != 4 || remoteRenameCall.Args[0] != "remote" || remoteRenameCall.Args[1] != "rename" || remoteRenameCall.Args[2] != "origin" || remoteRenameCall.Args[3] != "upstream" {
		t.Fatalf("unexpected remote rename call: %#v", remoteRenameCall.Args)
	}

	remoteAddCall := r.calls[2]
	if len(remoteAddCall.Args) != 4 || remoteAddCall.Args[0] != "remote" || remoteAddCall.Args[1] != "add" || remoteAddCall.Args[2] != "origin" {
		t.Fatalf("unexpected remote add call: %#v", remoteAddCall.Args)
	}
	if remoteAddCall.Args[3] != "https://gitea.example.com/aur/yay.git" {
		t.Fatalf("expected forge clone URL, got %q", remoteAddCall.Args[3])
	}

	symbolicRefCall := r.calls[3]
	if len(symbolicRefCall.Args) != 3 || symbolicRefCall.Args[0] != "symbolic-ref" || symbolicRefCall.Args[1] != "--short" || symbolicRefCall.Args[2] != "HEAD" {
		t.Fatalf("unexpected symbolic-ref call: %#v", symbolicRefCall.Args)
	}

	checkoutCall := r.calls[4]
	if len(checkoutCall.Args) != 2 || checkoutCall.Args[0] != "checkout" || checkoutCall.Args[1] != "--detach" {
		t.Fatalf("unexpected checkout --detach call: %#v", checkoutCall.Args)
	}

	branchDeleteCall := r.calls[5]
	if len(branchDeleteCall.Args) != 3 || branchDeleteCall.Args[0] != "branch" || branchDeleteCall.Args[1] != "-D" {
		t.Fatalf("unexpected branch -D call: %#v", branchDeleteCall.Args)
	}

	switchCall := r.calls[6]
	if len(switchCall.Args) != 3 || switchCall.Args[0] != "switch" || switchCall.Args[1] != "--orphan" || switchCall.Args[2] != "master" {
		t.Fatalf("unexpected switch --orphan call: %#v", switchCall.Args)
	}

	rmCall := r.calls[7]
	if len(rmCall.Args) != 4 || rmCall.Args[0] != "rm" || rmCall.Args[1] != "-rf" || rmCall.Args[2] != "--ignore-unmatch" || rmCall.Args[3] != "." {
		t.Fatalf("unexpected rm -rf call: %#v", rmCall.Args)
	}

	cleanCall := r.calls[8]
	if len(cleanCall.Args) != 2 || cleanCall.Args[0] != "clean" || cleanCall.Args[1] != "-fdx" {
		t.Fatalf("unexpected clean -fdx call: %#v", cleanCall.Args)
	}

	commitCall := r.calls[9]
	if len(commitCall.Args) != 4 || commitCall.Args[0] != "commit" || commitCall.Args[1] != "--allow-empty" || commitCall.Args[2] != "-m" {
		t.Fatalf("unexpected commit call: %#v", commitCall.Args)
	}
}

func TestImportForgeFailure(t *testing.T) {
	p := &provider{
		catalog: &fakeCatalog{},
		runner:  &fakeRunner{},
		forge:   &fakeForge{err: errors.New("boom")},
		client:  mockAUROk(),
	}
	err := p.Import(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "Before retrying") {
		t.Fatalf("forge failure should not give cleanup guidance, got: %s", err.Error())
	}
}

func TestImportRepoPathFailure(t *testing.T) {
	p := &provider{
		catalog: &fakeCatalog{err: errors.New("boom")},
		runner:  &fakeRunner{},
		forge:   &fakeForge{cloneURL: "https://gitea.example.com/aur/yay.git"},
		client:  mockAUROk(),
	}
	err := p.Import(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Before retrying, delete the Gitea repository https://gitea.example.com/aur/yay.git.") {
		t.Fatalf("expected cleanup guidance naming the Gitea repo, got: %s", msg)
	}
	if !strings.Contains(msg, "create Gitea repository") {
		t.Fatalf("expected completed steps to mention Gitea repository creation, got: %s", msg)
	}
}

func TestImportStepFailureListsCompletedSteps(t *testing.T) {
	r := &fakeRunner{errs: []error{nil, nil, nil, nil, errors.New("boom")}}
	p := &provider{
		catalog: &fakeCatalog{},
		runner:  r,
		forge:   &fakeForge{cloneURL: "https://gitea.example.com/aur/yay.git"},
		client:  mockAUROk(),
	}

	err := p.Import(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()

	for _, want := range []string{
		"create Gitea repository",
		"resolve repository path",
		"clone AUR repository",
		"rename origin to upstream",
		"add origin remote",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected completed step %q in error, got: %s", want, msg)
		}
	}
	for _, notWant := range []string{
		"detach HEAD from cloned branch",
		"delete cloned branch",
		"switch to orphan master",
		"clear working tree",
		"remove untracked files",
		"commit empty master",
	} {
		if strings.Contains(msg, notWant) {
			t.Fatalf("expected step %q (never reached) absent from error, got: %s", notWant, msg)
		}
	}
}

func TestImportSymbolicRefReturnsNonMaster(t *testing.T) {
	r := &fakeRunner{
		outputs: []string{
			"", "", "", "main", // call 3 (symbolic-ref) returns "main"
		},
	}
	c := &fakeCatalog{}
	f := &fakeForge{cloneURL: "https://gitea.example.com/aur/yay.git"}
	p := &provider{catalog: c, runner: r, forge: f, client: mockAUROk()}

	if err := p.Import(context.Background(), domain.PackageName("yay")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The branch delete should use "main", not the hardcoded "master".
	deleted := false
	for _, call := range r.calls {
		if len(call.Args) >= 3 && call.Args[0] == "branch" && call.Args[1] == "-D" && call.Args[2] == "main" {
			deleted = true
			break
		}
	}
	if !deleted {
		t.Fatalf("expected branch -D main to be called, got calls: %#v", r.calls)
	}
}

func TestImportSymbolicRefFails(t *testing.T) {
	r := &fakeRunner{errs: []error{nil, nil, nil, errors.New("symbolic-ref failed")}}
	p := &provider{
		catalog: &fakeCatalog{},
		runner:  r,
		forge:   &fakeForge{cloneURL: "https://gitea.example.com/aur/yay.git"},
		client:  mockAUROk(),
	}

	err := p.Import(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrCreateMaster) {
		t.Fatalf("expected ErrCreateMaster, got: %v", err)
	}
	msg := err.Error()

	// Five completed steps: 2 pre-git + 3 preSteps.
	for _, want := range []string{
		"create Gitea repository",
		"resolve repository path",
		"clone AUR repository",
		"rename origin to upstream",
		"add origin remote",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected completed step %q in error, got: %s", want, msg)
		}
	}

	// Post-steps should not appear since symbolic-ref failed before them.
	for _, notWant := range []string{
		"detach HEAD from cloned branch",
		"switch to orphan master",
	} {
		if strings.Contains(msg, notWant) {
			t.Fatalf("expected step %q (never reached) absent from error, got: %s", notWant, msg)
		}
	}
}

func TestImportPackageNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resultcount":0,"version":5}`))
	}))
	defer ts.Close()

	p := &provider{
		catalog: &fakeCatalog{},
		runner:  &fakeRunner{},
		forge:   &fakeForge{},
		client: &http.Client{
			Transport: &roundTripper{fn: func(req *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"resultcount":0,"version":5}`))
				return w.Result(), nil
			}},
		},
	}

	err := p.Import(context.Background(), domain.PackageName("nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got: %v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "Before retrying") {
		t.Fatal("package not found should not give cleanup guidance")
	}
}

type roundTripper struct {
	fn func(*http.Request) (*http.Response, error)
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.fn(req)
}

// mockAUROk returns an httpDoer that responds with resultcount=1 for AUR RPC calls.
func mockAUROk() httpDoer {
	return &http.Client{Transport: &roundTripper{fn: func(req *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resultcount":1,"version":5}`))
		return w.Result(), nil
	}}}
}

func TestImportPackageVerifyNetworkFailure(t *testing.T) {
	p := &provider{
		catalog: &fakeCatalog{},
		runner:  &fakeRunner{},
		forge:   &fakeForge{},
		client: &http.Client{
			Transport: &roundTripper{fn: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			}},
		},
	}

	err := p.Import(context.Background(), domain.PackageName("yay"))
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	msg := err.Error()
	if strings.Contains(msg, "package not found") {
		t.Fatalf("network failure should not say 'package not found', got: %s", msg)
	}
	if !strings.Contains(msg, "could not verify package exists on AUR") {
		t.Fatalf("expected 'could not verify' message, got: %s", msg)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Fatalf("expected cause to include connection refused, got: %s", msg)
	}
}
