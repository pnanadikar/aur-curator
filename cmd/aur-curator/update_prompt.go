package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// updateCandidate is a curated package whose AUR version differs from
// the local one, as found by the update scan.
type updateCandidate struct {
	pkg          domain.PackageName
	aurVer       string
	localVer     string
	localUnknown bool
}

// describe returns "<local> -> <aur>", marked when the local version
// could not be read.
func (c updateCandidate) describe() string {
	s := c.localVer + " -> " + c.aurVer
	if c.localUnknown {
		s += " (local version unknown)"
	}
	return s
}

// Reasons the prompts stop before the end of the list.
const (
	abortUserQuit  = "user_quit"
	abortEOF       = "eof"        // end of input: treated as q
	abortReadError = "read_error" // any other read error: q, and a failure (#402)
)

// abortFor returns the abort reason for a read error, and the error
// itself when it is a failure (anything but EOF).
func abortFor(err error) (string, error) {
	if errors.Is(err, io.EOF) {
		return abortEOF, nil
	}
	return abortReadError, err
}

// batchAnswer is one parsed answer to the [y/n/a/q] prompt.
type batchAnswer int

const (
	answerYes batchAnswer = iota
	answerNo
	answerAll
	answerQuit
)

// parseIndividualAnswer accepts y/yes, n/no and q/quit in any case — the
// batch answers without a/all, which belongs to the batch list.
func parseIndividualAnswer(s string) (batchAnswer, bool) {
	ans, ok := parseBatchAnswer(s)
	if !ok || ans == answerAll {
		return 0, false
	}
	return ans, true
}

// offerUpdates asks about every stale package, the individual ones first
// (each with its warning, [y/n/q]), then the batch list ([y/n/a/q],
// promptBucket3). q, or a read error, at any prompt skips everything
// still unasked in both groups and names it (workflow.md § Offer).
//
// fetch is called for each confirmed package before the next prompt.
// abort says why the walk stopped early ("" when every package was
// answered); err is non-nil only for a read error other than EOF.
func offerUpdates(r *bufio.Reader, w io.Writer, individual []individualCandidate, batch []updateCandidate, fetch func(domain.PackageName)) (abort string, err error) {
	for i, c := range individual {
		ans, rerr := askIndividual(r, w, c)
		if rerr != nil {
			_, _ = fmt.Fprintln(w)
			printSkippedRest(w, append(individualNames(individual[i:]), candidateNames(batch)...))
			return abortFor(rerr)
		}
		switch ans {
		case answerYes:
			fetch(c.pkg)
		case answerNo:
			_, _ = fmt.Fprintf(w, "%s: skipped.\n", c.pkg)
		case answerQuit:
			printSkippedRest(w, append(individualNames(individual[i:]), candidateNames(batch)...))
			return abortUserQuit, nil
		}
	}
	return promptBucket3(r, w, batch, fetch)
}

// askIndividual asks about one individual candidate until an accepted
// answer is given or reading fails.
func askIndividual(r *bufio.Reader, w io.Writer, c individualCandidate) (batchAnswer, error) {
	for {
		_, _ = fmt.Fprint(w, individualPrompt(c))
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		if ans, ok := parseIndividualAnswer(line); ok {
			return ans, nil
		}
		_, _ = fmt.Fprintln(w, "Please answer y (fetch), n (skip) or q (skip all remaining).")
	}
}

// parseBatchAnswer accepts y/yes, n/no, a/all and q/quit in any case.
// Anything else, including an empty answer, is not accepted.
func parseBatchAnswer(s string) (batchAnswer, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return answerYes, true
	case "n", "no":
		return answerNo, true
	case "a", "all":
		return answerAll, true
	case "q", "quit":
		return answerQuit, true
	}
	return 0, false
}

// promptBucket3 lists cands to w when there is more than one, then
// offers each in turn with [y/n/a/q] (#424): y fetches this one, n
// skips it, a fetches this one and every one after it without asking
// again, q skips this one and every one after it. An answer that is not accepted is asked again.
// A read error (EOF included) is treated as q.
//
// fetch is called for each confirmed package, in list order, before
// the next prompt. abort says why the walk stopped early: "" when every
// candidate was answered, abortUserQuit, abortEOF or abortReadError; err
// is the read error for abortReadError.
func promptBucket3(r *bufio.Reader, w io.Writer, cands []updateCandidate, fetch func(domain.PackageName)) (abort string, err error) {
	if len(cands) == 0 {
		return "", nil
	}
	if len(cands) > 1 {
		_, _ = fmt.Fprintln(w, "\n=== Packages with updates available ===")
		for i, c := range cands {
			_, _ = fmt.Fprintf(w, "  [%d] %s: %s\n", i+1, c.pkg, c.describe())
		}
	}

	all := false
	for i, c := range cands {
		if all {
			fetch(c.pkg)
			continue
		}
		ans, rerr := askBatch(r, w, i, len(cands), c)
		if rerr != nil {
			_, _ = fmt.Fprintln(w)
			printSkippedRest(w, candidateNames(cands[i:]))
			return abortFor(rerr)
		}
		switch ans {
		case answerYes:
			fetch(c.pkg)
		case answerNo:
			_, _ = fmt.Fprintf(w, "%s: skipped.\n", c.pkg)
		case answerAll:
			all = true
			fetch(c.pkg)
		case answerQuit:
			printSkippedRest(w, candidateNames(cands[i:]))
			return abortUserQuit, nil
		}
	}
	return "", nil
}

// askBatch asks about one candidate until an accepted answer is given
// or reading fails.
func askBatch(r *bufio.Reader, w io.Writer, i, n int, c updateCandidate) (batchAnswer, error) {
	for {
		_, _ = fmt.Fprint(w, promptLine(i, n, c))
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		if ans, ok := parseBatchAnswer(line); ok {
			return ans, nil
		}
		_, _ = fmt.Fprintln(w, "Please answer y (fetch), n (skip), a (fetch this and all remaining) or q (skip all remaining).")
	}
}

// promptLine is the question for candidate i of n. With more than one
// candidate the list above already shows the versions, so the prompt
// names only the package; a single candidate has no list, so its
// prompt carries the versions.
func promptLine(i, n int, c updateCandidate) string {
	if n == 1 {
		return fmt.Sprintf("%s: %s  fetch? [y/n/a/q] ", c.pkg, c.describe())
	}
	return fmt.Sprintf("[%d/%d] %s? [y/n/a/q] ", i+1, n, c.pkg)
}

// printSkippedRest reports the packages left unanswered by q or a read
// error, by name.
func printSkippedRest(w io.Writer, rest []domain.PackageName) {
	names := make([]string, len(rest))
	for i, p := range rest {
		names[i] = string(p)
	}
	_, _ = fmt.Fprintf(w, "Aborted. Skipped: %s.\n", strings.Join(names, ", "))
}

func candidateNames(cs []updateCandidate) []domain.PackageName {
	names := make([]domain.PackageName, len(cs))
	for i, c := range cs {
		names[i] = c.pkg
	}
	return names
}

func individualNames(cs []individualCandidate) []domain.PackageName {
	names := make([]domain.PackageName, len(cs))
	for i, c := range cs {
		names[i] = c.pkg
	}
	return names
}
