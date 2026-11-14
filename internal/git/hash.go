package git

import "fmt"

// checkFullHash returns an error unless commit is a full commit hash: 40
// (SHA-1) or 64 (SHA-256) lowercase hex digits. Callers that take a stored
// commit (Review.UpstreamCommit, always `git rev-parse` output) check it
// before it reaches git's command line, where anything starting with "-"
// would be read as an option (#504). Unlike verifyCommit, it costs no git
// call: IsMerged runs for every package on every Dashboard load.
func checkFullHash(commit string) error {
	if n := len(commit); n != 40 && n != 64 {
		return fmt.Errorf("not a full commit hash: %q", commit)
	}
	for _, c := range commit {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("not a full commit hash: %q", commit)
		}
	}
	return nil
}
