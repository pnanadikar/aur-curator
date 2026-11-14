package main

import "github.com/pramod/aur-curator/internal/domain"

// uncuratedBuildNetwork returns the names in the config's buildNetwork
// that are not curated packages, in the config's order: check 1 reports
// each (#522). Such a name gives nothing the network, so it is a
// finding, never an error.
func uncuratedBuildNetwork(names []domain.PackageName, curated map[domain.PackageName]bool) []domain.PackageName {
	var out []domain.PackageName
	for _, n := range names {
		if !curated[n] {
			out = append(out, n)
		}
	}
	return out
}

// uncuratedBuildNetworkFinding is check 1's line for such a name.
func uncuratedBuildNetworkFinding(name domain.PackageName) string {
	return "buildNetwork: " + string(name) + " is not a curated package; remove it from the config or fix the spelling"
}
