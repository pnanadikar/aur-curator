package git

// RepositoryStatus contains Git-derived state only.
type RepositoryStatus struct {
	Clean        bool
	Detached     bool
	HasUntracked bool
	HasModified  bool
}

// RepositoryMetadata contains repository metadata.
type RepositoryMetadata struct {
	CurrentBranch string
	OriginURL     string
	UpstreamURL   string
}

// HasExpectedRemotes reports whether meta contains both an origin and
// upstream remote URL. Returns true only when neither string is empty.
// This is a presence check only — it does not validate that the URLs
// match the expected Gitea org/base-URL.
func HasExpectedRemotes(meta RepositoryMetadata) bool {
	return meta.OriginURL != "" && meta.UpstreamURL != ""
}

// Commit represents a Git commit.
type Commit struct {
	Hash    string
	Author  string
	Subject string
	Date    string
}

// FileKind classifies a changed file for Review screen tab routing
// (see tui-review.md § Tab Bar). PKGBUILD and Other both render in the
// Diff tab; SRCINFO, Install, and Patch each get their own tab.
type FileKind int

const (
	FileKindPKGBUILD FileKind = iota
	FileKindSRCINFO
	FileKindInstall
	FileKindPatch
	FileKindOther
)

// String returns a readable label for debugging/logging. Not used for
// persistence or UI display — GitDiff is derived, never stored.
func (k FileKind) String() string {
	switch k {
	case FileKindPKGBUILD:
		return "pkgbuild"
	case FileKindSRCINFO:
		return "srcinfo"
	case FileKindInstall:
		return "install"
	case FileKindPatch:
		return "patch"
	case FileKindOther:
		return "other"
	default:
		return "unknown"
	}
}

// FileDiff is the diff for exactly one changed file. GitDiff.Files holds
// one FileDiff per changed file — never a concatenated multi-file patch.
// For example, the Diff tab (tui-review.md) is populated by filtering
// Files for Kind == FileKindPKGBUILD or FileKindOther and rendering each
// as its own FileDiff, not merging them into a single blob.
type FileDiff struct {
	Path  string
	Kind  FileKind
	Patch string
}

// GitDiff is a range of upstream changes for one package: its commits and
// per-file diffs. Provider.Diff fills it from the live comparison of local
// master with upstream/master; Provider.ReviewDiff fills it from a review's
// stored range (domain.md § Reviewed Range, #387).
//
// BaseCommit is the start of the range: the merge base of master and
// upstream/master (Diff), or the review's stored base (ReviewDiff). It is
// empty on a first review (Import), where every entry in Files is a full
// addition and Commits is the entire upstream history.
//
// LocalCommit is master's commit, set by Diff only; it is empty when no
// local master exists yet, and always empty from ReviewDiff, which reads
// no refs.
type GitDiff struct {
	LocalCommit    string
	BaseCommit     string
	UpstreamCommit string
	Commits        []Commit // commits included in this comparison
	Files          []FileDiff
}
