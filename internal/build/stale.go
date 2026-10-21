package build

import "github.com/pramod/aur-curator/internal/domain"

// StaleArtifacts returns artifacts that are stale: they belong to a package
// whose BuildID does not match the current (latest-success) BuildID.
//
// A package absent from current (never built) is treated as stale, because
// its artifacts have no active build record to claim them.
func StaleArtifacts(arts []domain.Artifact, current map[domain.PackageName]domain.BuildID) []domain.Artifact {
	var stale []domain.Artifact
	for _, a := range arts {
		cid, ok := current[a.PackageName]
		if !ok {
			stale = append(stale, a)
			continue
		}
		if a.BuildID != cid {
			stale = append(stale, a)
		}
	}
	return stale
}
