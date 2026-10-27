package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
)

// TestImportFailureGuidance401: every Import failure and timeout names
// what it left behind — the Gitea repository once it was created, the
// local directory once the clone ran (or may have, if interrupted) — and
// is written to errors.log (#401).
func TestImportFailureGuidance401(t *testing.T) {
	const gitea = "delete the Gitea repository https://gitea.example.com/aur/yay.git"
	const dir = "remove the local directory /ws/yay"
	boom := errors.New("boom")
	timeout := fmt.Errorf("git: %w", context.DeadlineExceeded)
	// labelled is a timeout as the runner reports it since #417.
	expired, cancelExpired := WithTimeout(context.Background(), time.Millisecond)
	defer cancelExpired()
	<-expired.Done()
	labelled := fmt.Errorf("git [clone]: %w", Interrupted(expired, errors.New("signal: killed")))

	// runnerErrs index: 0 clone, 1 rename origin, 2 add origin,
	// 3 symbolic-ref, 4 detach … 9 commit.
	at := func(i int, err error) []error {
		errs := make([]error, i+1)
		errs[i] = err
		return errs
	}

	cases := []struct {
		name        string
		aurErr      error
		forgeErr    error
		catalogErr  error
		runnerErrs  []error
		wantTimeout bool
		want        string // the guidance; "" = none
	}{
		{name: "verify times out", aurErr: context.DeadlineExceeded, wantTimeout: true},
		{name: "create fails", forgeErr: boom},
		{name: "resolve path fails", catalogErr: boom, want: "Before retrying, " + gitea + "."},
		{name: "clone fails", runnerErrs: at(0, boom), want: "Before retrying, " + gitea + "."},
		{name: "clone times out", runnerErrs: at(0, timeout), wantTimeout: true,
			want: "Before retrying, " + gitea + " and " + dir + " if it exists (the clone was interrupted)."},
		{name: "clone times out, labelled by the runner", runnerErrs: at(0, labelled), wantTimeout: true,
			want: "Before retrying, " + gitea + " and " + dir + " if it exists (the clone was interrupted)."},
		{name: "rename origin fails", runnerErrs: at(1, boom), want: "Before retrying, " + gitea + " and " + dir + "."},
		{name: "detect branch fails", runnerErrs: at(3, boom), want: "Before retrying, " + gitea + " and " + dir + "."},
		{name: "commit times out", runnerErrs: at(9, timeout), wantTimeout: true,
			want: "Before retrying, " + gitea + " and " + dir + "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			client := mockAUROk()
			if tc.aurErr != nil {
				client = &http.Client{Transport: &roundTripper{fn: func(*http.Request) (*http.Response, error) {
					return nil, tc.aurErr
				}}}
			}
			p := &provider{
				catalog:  &fakeCatalog{repoPath: "/ws/yay", err: tc.catalogErr},
				runner:   &fakeRunner{errs: tc.runnerErrs},
				forge:    &fakeForge{err: tc.forgeErr},
				client:   client,
				rootPath: root,
			}

			err := p.Import(context.Background(), domain.PackageName("yay"))

			if err == nil {
				t.Fatal("err = nil, want an Import failure")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, `import "yay": `) {
				t.Fatalf("message does not start with the package: %s", msg)
			}
			if got := strings.Contains(msg, "timed out after"); got != tc.wantTimeout {
				t.Fatalf("timed out = %v, want %v: %s", got, tc.wantTimeout, msg)
			}
			if tc.want == "" {
				if strings.Contains(msg, "Before retrying") {
					t.Fatalf("unexpected guidance: %s", msg)
				}
			} else if !strings.HasSuffix(msg, "\n\n"+tc.want) {
				t.Fatalf("message does not end with the guidance\n got: %q\nwant suffix: %q", msg, "\n\n"+tc.want)
			}

			log, readErr := os.ReadFile(filepath.Join(root, ".aur-curator", "errors.log"))
			if readErr != nil {
				t.Fatalf("errors.log not written: %v", readErr)
			}
			lines := strings.Split(strings.TrimSpace(string(log)), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], `import "yay" failed: `) {
				t.Fatalf("errors.log = %q, want one import failure line", string(log))
			}
			if tc.want != "" && !strings.HasSuffix(lines[0], "; "+tc.want) {
				t.Fatalf("errors.log line lacks the guidance: %q", lines[0])
			}
			if tc.wantTimeout && strings.Count(lines[0], "timed out") != 1 {
				t.Fatalf("errors.log line should say it timed out exactly once: %q", lines[0])
			}
		})
	}
}
