package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
)

// hangingFetchProvider is a fakeGitProviderFetch whose Fetch hangs until
// its ctx is done, like a git fetch against an unreachable host. Once ctx
// is done, Fetch returns errKilled (or nil if fetchSucceeds) and
// UpstreamCommit fails, as git subprocesses killed by procgroup do.
type hangingFetchProvider struct {
	fakeGitProviderFetch
	fetchSucceeds bool
}

var (
	errKilled     = errors.New("signal: killed")
	errNoDeadline = errors.New("fake: fetch was never stopped")
)

func (f *hangingFetchProvider) Fetch(ctx context.Context, _ domain.PackageName) error {
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		return errNoDeadline // keeps a broken timeout from hanging the test
	}
	if f.fetchSucceeds {
		return nil
	}
	return errKilled
}

func (f *hangingFetchProvider) UpstreamCommit(ctx context.Context, pkg domain.PackageName) (string, error) {
	if ctx.Err() != nil {
		return "", errKilled
	}
	return f.fakeGitProviderFetch.UpstreamCommit(ctx, pkg)
}

// TestFetchAndReviewTimeout409: a hanging Fetch is stopped by the fetch
// timeout and the error says it timed out; a cancel or deadline of the
// caller's ctx still stops it but is not reported as this timeout (#409).
func TestFetchAndReviewTimeout409(t *testing.T) {
	const short = 20 * time.Millisecond
	cases := []struct {
		name          string
		timeout       time.Duration
		parent        func() (context.Context, context.CancelFunc)
		fetchSucceeds bool
		want          string
	}{
		{name: "fetch hangs", timeout: short,
			parent: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want:   "fetch pkg: timed out after 20ms: signal: killed"},
		{name: "commit read after fetch times out", timeout: short, fetchSucceeds: true,
			parent: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want:   "fetch pkg: timed out after 20ms: signal: killed"},
		{name: "caller cancels", timeout: time.Hour,
			parent: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				time.AfterFunc(short, cancel)
				return ctx, cancel
			},
			want: "signal: killed"},
		{name: "caller deadline", timeout: time.Hour,
			parent: func() (context.Context, context.CancelFunc) { return context.WithTimeout(context.Background(), short) },
			want:   "signal: killed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.parent()
			defer cancel()
			provider := &hangingFetchProvider{
				fakeGitProviderFetch: fakeGitProviderFetch{beforeCommit: "aaa", afterCommit: "bbb"},
				fetchSucceeds:        tc.fetchSucceeds,
			}
			store := &fakeReviewStoreFetch{}
			engine := NewEngine(provider, store, &fakeAnalyzerManager{}, &fakePolicyEngine{})

			start := time.Now()
			res, err := fetchAndReview(ctx, provider, engine, "pkg", tc.timeout)

			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("returned after %v, want about %v", elapsed, short)
			}
			if err == nil {
				t.Fatalf("err = nil (result %+v), want %q", res, tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("err = %q\nwant  %q", err.Error(), tc.want)
			}
			if !errors.Is(err, errKilled) {
				t.Fatalf("err does not wrap the git error: %v", err)
			}
			if store.saved != nil {
				t.Fatalf("a review was saved: %+v", store.saved)
			}
		})
	}
}

// TestFetchAndReviewUsesGitTimeout409: the exported function runs under
// git.GitTimeout: Fetch gets a deadline about that far away.
func TestFetchAndReviewUsesGitTimeout409(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	provider := &deadlineProbe{onFetch: func(ctx context.Context) {
		deadline, hasDeadline = ctx.Deadline()
	}}

	start := time.Now()
	if _, err := FetchAndReview(context.Background(), provider, nil, "pkg"); err != nil {
		t.Fatal(err)
	}

	if !hasDeadline {
		t.Fatal("Fetch ran without a deadline")
	}
	if d := deadline.Sub(start); d < git.GitTimeout-time.Second || d > git.GitTimeout+time.Second {
		t.Fatalf("deadline %v after start, want about %v", d, git.GitTimeout)
	}
}

type deadlineProbe struct {
	fakeGitProviderFetch
	onFetch func(context.Context)
}

func (p *deadlineProbe) Fetch(ctx context.Context, _ domain.PackageName) error {
	p.onFetch(ctx)
	return nil
}
