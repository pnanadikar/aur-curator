package main

import (
	"bufio"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// #424 — update offers each bucket-3 candidate in turn with [y/n/a/q].

func threeCandidates() []updateCandidate {
	return []updateCandidate{
		{pkg: "alpha", localVer: "1.0-1", aurVer: "1.1-1"},
		{pkg: "beta", localVer: "2.0-1", aurVer: "2.1-1"},
		{pkg: "gamma", localVer: "", aurVer: "3.1-1", localUnknown: true},
	}
}

func TestPromptBucket3(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantFetched []domain.PackageName
		wantReason  string
		wantOut     []string // substrings that must appear in the output
	}{
		{
			name:        "y n y: each answer applies to its own package",
			input:       "y\nn\ny\n",
			wantFetched: []domain.PackageName{"alpha", "gamma"},
			wantOut:     []string{"beta: skipped.\n"},
		},
		{
			name:        "n n n: nothing fetched",
			input:       "n\nn\nn\n",
			wantFetched: nil,
			wantOut:     []string{"alpha: skipped.\n", "beta: skipped.\n", "gamma: skipped.\n"},
		},
		{
			name:        "a at the first prompt fetches all, asked once",
			input:       "a\n",
			wantFetched: []domain.PackageName{"alpha", "beta", "gamma"},
		},
		{
			name:        "n then a: all remaining is forward only",
			input:       "n\na\n",
			wantFetched: []domain.PackageName{"beta", "gamma"},
			wantOut:     []string{"alpha: skipped.\n"},
		},
		{
			name:        "y then q: the rest are skipped and named",
			input:       "y\nq\n",
			wantFetched: []domain.PackageName{"alpha"},
			wantReason:  abortUserQuit,
			wantOut:     []string{"Aborted. Skipped: beta, gamma.\n"},
		},
		{
			name:        "q at the first prompt",
			input:       "q\n",
			wantFetched: nil,
			wantReason:  abortUserQuit,
			wantOut:     []string{"Aborted. Skipped: alpha, beta, gamma.\n"},
		},
		{
			name:        "unaccepted and empty answers are asked again",
			input:       "maybe\n\nyolo\ny\nn\nn\n",
			wantFetched: []domain.PackageName{"alpha"},
			wantOut:     []string{"Please answer y (fetch)"},
		},
		{
			name:        "upper case and full words",
			input:       "YES\nNo\nALL\n",
			wantFetched: []domain.PackageName{"alpha", "gamma"},
		},
		{
			name:       "quit as a full word",
			input:      "Quit\n",
			wantReason: abortUserQuit,
			wantOut:    []string{"Aborted. Skipped: alpha, beta, gamma.\n"},
		},
		{
			name:        "EOF mid-walk is treated as q",
			input:       "y\n",
			wantFetched: []domain.PackageName{"alpha"},
			wantReason:  abortEOF,
			wantOut:     []string{"Aborted. Skipped: beta, gamma.\n"},
		},
		{
			name:       "EOF at the first prompt",
			input:      "",
			wantReason: abortEOF,
			wantOut:    []string{"Aborted. Skipped: alpha, beta, gamma.\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched []domain.PackageName
			var out strings.Builder
			r := bufio.NewReader(strings.NewReader(tt.input))

			reason, err := promptBucket3(r, &out, threeCandidates(), func(p domain.PackageName) {
				fetched = append(fetched, p)
			})
			if err != nil {
				t.Errorf("err = %v, want nil (EOF is not a failure)", err)
			}

			if !reflect.DeepEqual(fetched, tt.wantFetched) {
				t.Errorf("fetched = %v, want %v", fetched, tt.wantFetched)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			for _, s := range tt.wantOut {
				if !strings.Contains(out.String(), s) {
					t.Errorf("output lacks %q; got:\n%s", s, out.String())
				}
			}
		})
	}
}

// Exact prompt and list lines, so a change in wording is a deliberate one.
func TestPromptBucket3Output(t *testing.T) {
	var out strings.Builder
	r := bufio.NewReader(strings.NewReader("n\nn\nn\n"))
	_, _ = promptBucket3(r, &out, threeCandidates(), func(domain.PackageName) {})

	for _, want := range []string{
		"\n=== Packages with updates available ===\n",
		"  [1] alpha: 1.0-1 -> 1.1-1\n",
		"  [3] gamma:  -> 3.1-1 (local version unknown)\n",
		"[1/3] alpha? [y/n/a/q] ",
		"[3/3] gamma? [y/n/a/q] ",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q; got:\n%s", want, out.String())
		}
	}
	if n := strings.Count(out.String(), "[y/n/a/q] "); n != 3 {
		t.Errorf("prompted %d times, want 3", n)
	}
}

// After a, the remaining candidates are fetched without another prompt.
func TestPromptBucket3AllAsksOnce(t *testing.T) {
	var out strings.Builder
	r := bufio.NewReader(strings.NewReader("a\n"))
	_, _ = promptBucket3(r, &out, threeCandidates(), func(domain.PackageName) {})
	if n := strings.Count(out.String(), "[y/n/a/q] "); n != 1 {
		t.Errorf("prompted %d times, want 1", n)
	}
}

func TestPromptBucket3Empty(t *testing.T) {
	var out strings.Builder
	called := false
	reason, _ := promptBucket3(bufio.NewReader(strings.NewReader("")), &out, nil, func(domain.PackageName) { called = true })
	if reason != "" || called || out.Len() != 0 {
		t.Errorf("empty list: reason=%q called=%v output=%q, want nothing", reason, called, out.String())
	}
}

// One candidate: no list; the prompt carries the versions.
func TestPromptBucket3Single(t *testing.T) {
	var out strings.Builder
	var fetched []domain.PackageName
	r := bufio.NewReader(strings.NewReader("y\n"))
	cands := []updateCandidate{{pkg: "pfetch", localVer: "1.10.0-1", aurVer: "1.11.0-1"}}
	reason, _ := promptBucket3(r, &out, cands, func(p domain.PackageName) { fetched = append(fetched, p) })

	want := "pfetch: 1.10.0-1 -> 1.11.0-1  fetch? [y/n/a/q] "
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
	if reason != "" || !reflect.DeepEqual(fetched, []domain.PackageName{"pfetch"}) {
		t.Errorf("reason = %q, fetched = %v; want \"\", [pfetch]", reason, fetched)
	}
}

// #402 — a read error other than EOF stops the walk like q and is a
// failure.
type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestPromptBucket3ReadError(t *testing.T) {
	var out strings.Builder
	boom := errors.New("boom")
	reason, err := promptBucket3(bufio.NewReader(failingReader{boom}), &out, threeCandidates(), func(domain.PackageName) {
		t.Error("fetch called after a read error")
	})
	if reason != abortReadError || !errors.Is(err, boom) {
		t.Errorf("reason = %q, err = %v; want %q, boom", reason, err, abortReadError)
	}
	if !strings.Contains(out.String(), "Aborted. Skipped: alpha, beta, gamma.\n") {
		t.Errorf("skipped packages not named; got:\n%s", out.String())
	}
}

// #402 — individual prompts take y/n/q; q or end of input skips the rest
// of both groups and names them.
func TestOfferUpdates(t *testing.T) {
	individual := func() []individualCandidate {
		return []individualCandidate{
			{updateCandidate{pkg: "pend", localVer: "1.0-1", aurVer: "1.1-1"}, lossPendingReview},
			{updateCandidate{pkg: "appr", localVer: "2.0-1", aurVer: "2.1-1"}, lossUnmergedApproval},
		}
	}
	tests := []struct {
		name        string
		input       string
		wantFetched []domain.PackageName
		wantReason  string
		wantOut     []string
		wantNotOut  []string
	}{
		{
			name:        "y n, then the batch",
			input:       "y\nn\ny\nn\nn\n",
			wantFetched: []domain.PackageName{"pend", "alpha"},
			wantOut:     []string{"appr: skipped.\n", "[1/3] alpha? [y/n/a/q] "},
		},
		{
			name:        "q at an individual prompt skips both groups",
			input:       "y\nq\n",
			wantFetched: []domain.PackageName{"pend"},
			wantReason:  abortUserQuit,
			wantOut:     []string{"Aborted. Skipped: appr, alpha, beta, gamma.\n"},
			wantNotOut:  []string{"[y/n/a/q]"},
		},
		{
			name:       "EOF at an individual prompt skips both groups",
			input:      "",
			wantReason: abortEOF,
			wantOut:    []string{"Aborted. Skipped: pend, appr, alpha, beta, gamma.\n"},
			wantNotOut: []string{"[y/n/a/q]"},
		},
		{
			name:        "a is not accepted at an individual prompt; typos are asked again",
			input:       "a\nyse\nYES\nN\nn\nn\nn\n",
			wantFetched: []domain.PackageName{"pend"},
			wantOut:     []string{"Please answer y (fetch), n (skip) or q (skip all remaining).\n"},
		},
		{
			name:        "Quit as a full word",
			input:       "Quit\n",
			wantReason:  abortUserQuit,
			wantOut:     []string{"Aborted. Skipped: pend, appr, alpha, beta, gamma.\n"},
			wantFetched: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetched []domain.PackageName
			var out strings.Builder
			r := bufio.NewReader(strings.NewReader(tt.input))
			reason, err := offerUpdates(r, &out, individual(), threeCandidates(), func(p domain.PackageName) {
				fetched = append(fetched, p)
			})
			if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if !reflect.DeepEqual(fetched, tt.wantFetched) {
				t.Errorf("fetched = %v, want %v", fetched, tt.wantFetched)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			for _, s := range tt.wantOut {
				if !strings.Contains(out.String(), s) {
					t.Errorf("output lacks %q; got:\n%s", s, out.String())
				}
			}
			for _, s := range tt.wantNotOut {
				if strings.Contains(out.String(), s) {
					t.Errorf("output has %q; got:\n%s", s, out.String())
				}
			}
		})
	}
}

func TestOfferUpdatesReadError(t *testing.T) {
	var out strings.Builder
	boom := errors.New("boom")
	ind := []individualCandidate{{updateCandidate{pkg: "pend", localVer: "1.0-1", aurVer: "1.1-1"}, lossPendingReview}}
	reason, err := offerUpdates(bufio.NewReader(failingReader{boom}), &out, ind, threeCandidates(), func(domain.PackageName) {
		t.Error("fetch called after a read error")
	})
	if reason != abortReadError || !errors.Is(err, boom) {
		t.Errorf("reason = %q, err = %v; want %q, boom", reason, err, abortReadError)
	}
	if !strings.Contains(out.String(), "Aborted. Skipped: pend, alpha, beta, gamma.\n") {
		t.Errorf("skipped packages not named; got:\n%s", out.String())
	}
}
