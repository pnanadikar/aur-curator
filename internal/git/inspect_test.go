package git

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestParseStatusClean(t *testing.T) {
	rs := parseStatus("## master...upstream/master\n")
	if !rs.Clean {
		t.Fatal("expected clean repository")
	}
}

func TestParseStatusUntracked(t *testing.T) {
	rs := parseStatus("## master...upstream/master\n?? foo\n")
	if rs.Clean {
		t.Fatal("expected dirty repository")
	}
	if !rs.HasUntracked {
		t.Fatal("expected untracked files")
	}
}

func TestParseStatusModified(t *testing.T) {
	rs := parseStatus("## master...upstream/master\n M visual-studio-code-bin.sh\n")
	if rs.Clean {
		t.Fatal("expected dirty repository")
	}
	if !rs.HasModified {
		t.Fatal("expected modified files")
	}
	if rs.HasUntracked {
		t.Fatal("unexpected untracked files")
	}
}

func TestCommitsBehind(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := &fakeRunner{output: "3\n"}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		n, err := p.CommitsBehind(context.Background(), domain.PackageName("yay"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Fatalf("expected 3, got %d", n)
		}

		call := r.calls[0]
		if len(call.Args) != 3 || call.Args[0] != "rev-list" || call.Args[1] != "--count" || call.Args[2] != reviewRange {
			t.Fatalf("unexpected call: %#v", call.Args)
		}
	})

	t.Run("non-numeric output", func(t *testing.T) {
		r := &fakeRunner{output: "not-a-number\n"}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		if _, err := p.CommitsBehind(context.Background(), domain.PackageName("yay")); err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("runner error", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		if _, err := p.CommitsBehind(context.Background(), domain.PackageName("yay")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUpstreamCommit(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := &fakeRunner{output: "abc123def456\n"}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		hash, err := p.UpstreamCommit(context.Background(), domain.PackageName("yay"))
		if err != nil {
			t.Fatal(err)
		}
		if hash != "abc123def456" {
			t.Fatalf("expected abc123def456, got %q", hash)
		}

		call := r.calls[0]
		if len(call.Args) != 2 || call.Args[0] != "rev-parse" || call.Args[1] != "upstream/master" {
			t.Fatalf("unexpected call: %#v", call.Args)
		}
	})

	t.Run("runner error", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		if _, err := p.UpstreamCommit(context.Background(), domain.PackageName("yay")); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		r := &fakeRunner{output: "  abc123def456  \n"}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		hash, err := p.UpstreamCommit(context.Background(), domain.PackageName("yay"))
		if err != nil {
			t.Fatal(err)
		}
		if hash != "abc123def456" {
			t.Fatalf("expected abc123def456, got %q", hash)
		}
	})
}

func TestMetadata(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := &fakeRunner{
			outputs: []string{
				"master\n",
				"https://gitea.example.com/aur/yay.git\n",
				"https://aur.archlinux.org/yay.git\n",
			},
		}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		md, err := p.Metadata(context.Background(), domain.PackageName("yay"))
		if err != nil {
			t.Fatal(err)
		}
		if md.CurrentBranch != "master" {
			t.Fatalf("unexpected CurrentBranch: %q", md.CurrentBranch)
		}
		if md.OriginURL != "https://gitea.example.com/aur/yay.git" {
			t.Fatalf("unexpected OriginURL: %q", md.OriginURL)
		}
		if md.UpstreamURL != "https://aur.archlinux.org/yay.git" {
			t.Fatalf("unexpected UpstreamURL: %q", md.UpstreamURL)
		}
	})

	t.Run("branch lookup fails", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		p := &provider{runner: r, catalog: &fakeCatalog{}, client: http.DefaultClient}

		if _, err := p.Metadata(context.Background(), domain.PackageName("yay")); err == nil {
			t.Fatal("expected error")
		}
	})
}
