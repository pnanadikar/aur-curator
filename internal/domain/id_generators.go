package domain

import (
	"crypto/rand"
	"encoding/hex"
)

// This file adds ID constructors alongside the existing type
// declarations in ids.go (PackageName, ReviewID, FindingID, etc., per
// go-domain-structs.md's Package Layout). It's a separate file rather
// than a manual edit to ids.go — Go doesn't require every ID-related
// declaration to live in one file, and a new file avoids hand-merging
// text into a file this session has never seen the current contents of.

// NewFindingID generates a new, unique FindingID. IDs are opaque random
// strings — not sequential, not derived from content — which is
// sufficient uniqueness at this project's ~10-12 package scale without
// pulling in a UUID dependency. See go-domain-structs.md § Finding.
func NewFindingID() FindingID {
	return FindingID(newRandomID())
}

// NewRiskAssessmentID generates a new, unique RiskAssessmentID. Same
// approach as NewFindingID, reusing the same newRandomID helper below.
func NewRiskAssessmentID() RiskAssessmentID {
	return RiskAssessmentID(newRandomID())
}

// newRandomID returns a 128-bit random value as a hex string, for
// record IDs with no natural key: FindingID (ReviewEngine.RunAnalyzers)
// and RiskAssessmentID (ReviewEngine.RecordDecision). It is not for
// identifiers with a natural key — ReviewID is deterministic
// (pkg@upstreamCommit) and PackageName is the package's own name — so
// do not generate either of those here.
func newRandomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing indicates a broken platform RNG —
		// practically never happens on any supported target. Panicking
		// here is preferable to silently persisting a record with an
		// empty ID.
		panic("domain: failed to generate random ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
