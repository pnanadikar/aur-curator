package git

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestLocalOnlyCommits(t *testing.T) {
	cases := []struct {
		name       string
		runner     *fakeRunner
		catalogErr error
		want       int
		wantErr    bool
		wantCalls  int
	}{
		{
			name:      "master absent",
			runner:    &fakeRunner{outputs: []string{""}},
			want:      0,
			wantCalls: 1,
		},
		{
			name:      "import placeholder is not a local commit",
			runner:    &fakeRunner{outputs: []string{"abc123\n", emptyTreeHash + "\n"}},
			want:      0,
			wantCalls: 2, // no rev-list
		},
		{
			name:      "counts commits not on upstream",
			runner:    &fakeRunner{outputs: []string{"abc123\n", "deadbeef\n", "3\n"}},
			want:      3,
			wantCalls: 3,
		},
		{
			name:      "none",
			runner:    &fakeRunner{outputs: []string{"abc123\n", "deadbeef\n", "0\n"}},
			want:      0,
			wantCalls: 3,
		},
		{
			name:       "repo path failure",
			runner:     &fakeRunner{},
			catalogErr: errors.New("boom"),
			wantErr:    true,
			wantCalls:  0,
		},
		{
			name:      "master lookup failure",
			runner:    &fakeRunner{errs: []error{errors.New("git broke")}},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name: "tree check failure",
			runner: &fakeRunner{
				outputs: []string{"abc123\n"},
				errs:    []error{nil, errors.New("bad object")},
			},
			wantErr:   true,
			wantCalls: 2,
		},
		{
			name: "rev-list failure (e.g. no upstream ref)",
			runner: &fakeRunner{
				outputs: []string{"abc123\n", "deadbeef\n"},
				errs:    []error{nil, nil, errors.New("unknown revision upstream/master")},
			},
			wantErr:   true,
			wantCalls: 3,
		},
		{
			name:      "non-numeric output",
			runner:    &fakeRunner{outputs: []string{"abc123\n", "deadbeef\n", "lots\n"}},
			wantErr:   true,
			wantCalls: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &provider{runner: tc.runner, catalog: &fakeCatalog{err: tc.catalogErr}, forge: &fakeForge{}, client: http.DefaultClient}
			got, err := p.LocalOnlyCommits(context.Background(), domain.PackageName("yay"))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				if got != 0 {
					t.Fatalf("error must come with 0, got %d", got)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("got %d, want %d", got, tc.want)
				}
			}
			if len(tc.runner.calls) != tc.wantCalls {
				t.Fatalf("expected %d calls, got %#v", tc.wantCalls, tc.runner.calls)
			}
			if tc.wantCalls == 3 && !tc.wantErr {
				if !mergeArgsEqual(tc.runner.calls[2].Args, "rev-list", "--count", "upstream/master..master") {
					t.Fatalf("unexpected count call: %#v", tc.runner.calls[2].Args)
				}
			}
		})
	}
}
