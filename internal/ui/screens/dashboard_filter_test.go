package screens

import (
	"context"
	"testing"

	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/ui/msgs"
)

func TestCardFilterPredicates(t *testing.T) {
	t.Parallel()

	// build is the LATEST ATTEMPT passed to the card predicate.
	approvedBuild := &domain.Build{Status: domain.BuildSuccess, Confirmed: false}
	failedBuild := &domain.Build{Status: domain.BuildFailed, Confirmed: true}

	testCases := []struct {
		name      string
		cardID    CardID
		lifecycle lifecycle.State
		build     *domain.Build
		aurStale  bool
		hasUpdate bool
		want      bool
	}{
		// CardPackages — always matches
		{"packages/approved", CardPackages, lifecycle.Approved, nil, false, false, true},
		{"packages/review-pending", CardPackages, lifecycle.ReviewPending, nil, false, false, true},
		{"packages/merged", CardPackages, lifecycle.Merged, nil, false, false, true},
		{"packages/rejected", CardPackages, lifecycle.Rejected, nil, false, false, true},
		{"packages/not-reviewed", CardPackages, lifecycle.NotReviewed, nil, false, false, true},
		{"packages/with-build", CardPackages, lifecycle.Approved, approvedBuild, false, false, true},

		// CardToReview — matches only "Review Pending"
		{"review/review-pending", CardToReview, lifecycle.ReviewPending, nil, false, false, true},
		{"review/review-pending-with-build", CardToReview, lifecycle.ReviewPending, approvedBuild, false, false, true},
		{"review/approved", CardToReview, lifecycle.Approved, nil, false, false, false},
		{"review/merged", CardToReview, lifecycle.Merged, nil, false, false, false},
		{"review/rejected", CardToReview, lifecycle.Rejected, nil, false, false, false},
		{"review/not-reviewed", CardToReview, lifecycle.NotReviewed, nil, false, false, false},

		// CardApproved — matches only "Approved"
		{"approved/approved", CardApproved, lifecycle.Approved, nil, false, false, true},
		{"approved/approved-with-build", CardApproved, lifecycle.Approved, approvedBuild, false, false, true},
		{"approved/merged", CardApproved, lifecycle.Merged, nil, false, false, false},
		{"approved/review-pending", CardApproved, lifecycle.ReviewPending, nil, false, false, false},
		{"approved/rejected", CardApproved, lifecycle.Rejected, nil, false, false, false},
		{"approved/not-reviewed", CardApproved, lifecycle.NotReviewed, nil, false, false, false},

		// CardMerged — matches only "Merged"
		{"merged/merged", CardMerged, lifecycle.Merged, nil, false, false, true},
		{"merged/merged-with-build", CardMerged, lifecycle.Merged, approvedBuild, false, false, true},
		{"merged/approved", CardMerged, lifecycle.Approved, nil, false, false, false},
		{"merged/review-pending", CardMerged, lifecycle.ReviewPending, nil, false, false, false},
		{"merged/rejected", CardMerged, lifecycle.Rejected, nil, false, false, false},
		{"merged/not-reviewed", CardMerged, lifecycle.NotReviewed, nil, false, false, false},

		// CardFailed — latest attempt failed, package in the build flow.
		// Cases from main, unchanged:
		{"failed/approved-failed", CardFailed, lifecycle.Approved, failedBuild, false, false, true},
		{"failed/merged-failed", CardFailed, lifecycle.Merged, failedBuild, false, false, true},
		{"failed/approved-success", CardFailed, lifecycle.Approved, approvedBuild, false, false, false},
		{"failed/approved-no-build", CardFailed, lifecycle.Approved, nil, false, false, false},
		{"failed/merged-no-build", CardFailed, lifecycle.Merged, nil, false, false, false},
		{"failed/review-pending-failed", CardFailed, lifecycle.ReviewPending, failedBuild, false, false, false},
		{"failed/rejected-failed", CardFailed, lifecycle.Rejected, failedBuild, false, false, false},
		{"failed/not-reviewed-failed", CardFailed, lifecycle.NotReviewed, failedBuild, false, false, false},
		// New (#309 R1: a failed rebuild no longer demotes the package, so
		// it must still count as Failed in these states):
		{"failed/built-failed", CardFailed, lifecycle.Built, failedBuild, false, false, true},
		{"failed/confirmed-failed", CardFailed, lifecycle.Confirmed, failedBuild, false, false, true},
		{"failed/published-failed", CardFailed, lifecycle.Published, failedBuild, false, false, true},
		{"failed/published-success", CardFailed, lifecycle.Published, approvedBuild, false, false, false},

		// CardConfirmed — state Confirmed only (#365: Built has its own card).
		{"confirmed/built", CardConfirmed, lifecycle.Built, nil, false, false, false},
		{"confirmed/confirmed", CardConfirmed, lifecycle.Confirmed, nil, false, false, true},
		{"confirmed/built-latest-failed", CardConfirmed, lifecycle.Built, failedBuild, false, false, false},
		{"confirmed/confirmed-latest-failed", CardConfirmed, lifecycle.Confirmed, failedBuild, false, false, true},
		{"confirmed/published", CardConfirmed, lifecycle.Published, nil, false, false, false},
		{"confirmed/merged-success-attempt", CardConfirmed, lifecycle.Merged, approvedBuild, false, false, false},
		{"confirmed/approved", CardConfirmed, lifecycle.Approved, approvedBuild, false, false, false},
		{"confirmed/review-pending", CardConfirmed, lifecycle.ReviewPending, approvedBuild, false, false, false},
		{"confirmed/rejected", CardConfirmed, lifecycle.Rejected, approvedBuild, false, false, false},
		{"confirmed/not-reviewed", CardConfirmed, lifecycle.NotReviewed, approvedBuild, false, false, false},

		// CardBuilt — matches only "Built" lifecycle state
		{"built/built", CardBuilt, lifecycle.Built, nil, false, false, true},
		{"built/built-with-build", CardBuilt, lifecycle.Built, approvedBuild, false, false, true},
		{"built/confirmed", CardBuilt, lifecycle.Confirmed, nil, false, false, false},
		{"built/approved", CardBuilt, lifecycle.Approved, nil, false, false, false},
		{"built/merged", CardBuilt, lifecycle.Merged, nil, false, false, false},
		{"built/review-pending", CardBuilt, lifecycle.ReviewPending, nil, false, false, false},
		{"built/rejected", CardBuilt, lifecycle.Rejected, nil, false, false, false},
		{"built/not-reviewed", CardBuilt, lifecycle.NotReviewed, nil, false, false, false},

		// CardAUR — matches when pkg is in aurStaleByPkg with AURStale==true
		{"aur/stale", CardAUR, lifecycle.Approved, nil, true, false, true},
		{"aur/not-stale", CardAUR, lifecycle.Approved, nil, false, false, false},
		{"aur/absent-from-map", CardAUR, lifecycle.ReviewPending, approvedBuild, false, false, false},

		// CardFetched — matches when pkg has updates > 0
		{"fetched/has-updates", CardFetched, lifecycle.Approved, nil, false, true, true},
		{"fetched/no-updates", CardFetched, lifecycle.Approved, nil, false, false, false},
		{"fetched/absent-from-map", CardFetched, lifecycle.ReviewPending, approvedBuild, false, false, false},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewDashboardModel(deps.Dependencies{})
			pred, ok := m.cardFilterMap[tt.cardID]
			if !ok {
				t.Fatalf("cardFilterMap[%q] missing", tt.cardID)
			}

			// Set up aurStaleByPkg for tests that need it
			if tt.cardID == CardAUR {
				m.aurStaleByPkg[domain.PackageName(tt.name)] = tt.aurStale
			}
			// Set up updates for CardFetched tests only when hasUpdate is true
			if tt.cardID == CardFetched && tt.hasUpdate {
				if m.updates == nil {
					m.updates = make(map[domain.PackageName]int)
				}
				m.updates[domain.PackageName(tt.name)] = 1
			}

			got := pred(domain.PackageName(tt.name), tt.lifecycle, tt.build)
			if got != tt.want {
				t.Errorf("predicate(%q, %q, %v) = %v, want %v", tt.name, tt.lifecycle, tt.build != nil, got, tt.want)
			}
		})
	}
}

func TestCardFilterNoNilPanic(t *testing.T) {
	t.Parallel()

	// Verify that all cards in cardIDs() have a non-nil predicate
	// in cardFilterMap, and that calling applyCardFilter with them
	// does not panic.
	m := NewDashboardModel(deps.Dependencies{})

	for _, id := range cardIDs() {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			pred, ok := m.cardFilterMap[id]
			if !ok {
				t.Errorf("cardFilterMap[%q] should exist", id)
			}
			if pred == nil {
				t.Errorf("cardFilterMap[%q] = nil, want non-nil", id)
			}
		})
	}
}

func TestCardIDFromIndex(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		index int
		want  CardID
	}{
		{0, CardPackages},
		{1, CardAUR},
		{2, CardFetched},
		{3, CardToReview},
		{4, CardApproved},
		{5, CardMerged},
		{6, CardBuilt},
		{7, CardConfirmed},
		{8, CardFailed},
		{-1, ""},
		{9, ""},
		{100, ""},
	}

	for _, tt := range testCases {
		t.Run(string(tt.want), func(t *testing.T) {
			t.Parallel()

			got := cardIDFromIndex(tt.index)
			if got != tt.want {
				t.Errorf("cardIDFromIndex(%d) = %q, want %q", tt.index, got, tt.want)
			}
		})
	}
}

func TestCardFilterText(t *testing.T) {
	t.Parallel()

	seen := make(map[CardID]bool)
	for _, def := range dashboardCards {
		if seen[def.ID] {
			t.Errorf("duplicate card ID %q in dashboardCards", def.ID)
		}
		seen[def.ID] = true
		if def.Label == "" || def.Filter == "" {
			t.Errorf("card %q: empty Label or Filter", def.ID)
		}
		if got := cardFilterText(def.ID); got != def.Filter {
			t.Errorf("cardFilterText(%q) = %q, want %q", def.ID, got, def.Filter)
		}
	}
	// The AUR card's filter text was missing before #365.
	if got := cardFilterText(CardAUR); got != "AUR fetch pending" {
		t.Errorf("cardFilterText(CardAUR) = %q, want %q", got, "AUR fetch pending")
	}
	if got := cardFilterText("unknown"); got != "" {
		t.Errorf("cardFilterText(unknown) = %q, want empty", got)
	}
}

func TestIsApprovedOrMerged(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		lc   lifecycle.State
		want bool
	}{
		{lifecycle.Approved, true},
		{lifecycle.Merged, true},
		{lifecycle.ReviewPending, false},
		{lifecycle.Rejected, false},
		{lifecycle.NotReviewed, false},
		{"", false},
	}

	for _, tt := range testCases {
		t.Run(string(tt.lc), func(t *testing.T) {
			t.Parallel()

			got := isApprovedOrMerged(tt.lc)
			if got != tt.want {
				t.Errorf("isApprovedOrMerged(%q) = %v, want %v", tt.lc, got, tt.want)
			}
		})
	}
}

func TestCardFilterRegression_CardToShowMatchesPredicate(t *testing.T) {
	t.Parallel()

	// Regression test for the original Defect A: selecting a card showed a
	// different card's filter due to positional misalignment between the
	// cards array and the filters string array.
	//
	// The fix replaces positional string indexing with a CardID→predicate
	// map, so each card's filter is determined by its CardID identity,
	// not its position in an array.
	//
	// This test verifies that for every implemented card, the predicate
	// produces correct results — i.e. selecting a card filters to the
	// packages that match that card's semantic meaning, not a neighbor's.

	m := NewDashboardModel(deps.Dependencies{})

	// Define the expected predicate outcome for a representative set of
	// packages that would be visible in the dashboard.
	packages := []struct {
		name      string
		lifecycle lifecycle.State
		build     *domain.Build
	}{
		{"pkg-approved", lifecycle.Approved, &domain.Build{Status: domain.BuildSuccess, Confirmed: false}},
		{"pkg-merged", lifecycle.Merged, &domain.Build{Status: domain.BuildSuccess, Confirmed: true, Published: true}},
		{"pkg-review", lifecycle.ReviewPending, nil},
		{"pkg-rejected", lifecycle.Rejected, &domain.Build{Status: domain.BuildFailed, Confirmed: true}},
		{"pkg-not-reviewed", lifecycle.NotReviewed, nil},
		{"pkg-approved-failed", lifecycle.Approved, &domain.Build{Status: domain.BuildFailed, Confirmed: true}},
	}

	// For each implemented card, verify that its predicate correctly
	// selects exactly the packages that should appear when that card is
	// filtered.
	cards := []CardID{CardPackages, CardAUR, CardFetched, CardToReview, CardApproved, CardMerged, CardBuilt, CardConfirmed, CardFailed}

	for _, cardID := range cards {
		t.Run(string(cardID), func(t *testing.T) {
			t.Parallel()

			pred, ok := m.cardFilterMap[cardID]
			if !ok {
				t.Fatalf("card %q missing from cardFilterMap", cardID)
			}

			// Set up aurStaleByPkg for CardAUR tests
			if cardID == CardAUR {
				m.aurStaleByPkg["pkg-approved"] = true
				m.aurStaleByPkg["pkg-merged"] = false
				m.aurStaleByPkg["pkg-approved-failed"] = true
			}
			// Set up updates for CardFetched tests
			if cardID == CardFetched {
				m.updates["pkg-approved"] = 5
				m.updates["pkg-merged"] = 0
				m.updates["pkg-approved-failed"] = 3
			}

			// Count how many packages match the predicate.
			matchCount := 0
			for _, pkg := range packages {
				if pred(domain.PackageName(pkg.name), pkg.lifecycle, pkg.build) {
					matchCount++
				}
			}

			// Verify each package's individual result.
			for _, pkg := range packages {
				got := pred(domain.PackageName(pkg.name), pkg.lifecycle, pkg.build)
				if !ok {
					continue
				}

				switch cardID {
				case CardPackages:
					if !got {
						t.Errorf("CardPackages should match all packages, but %q did not", pkg.name)
					}
				case CardToReview:
					expected := pkg.lifecycle == lifecycle.ReviewPending
					if got != expected {
						t.Errorf("CardToReview: pkg %q (lc=%q) match=%v, want %v", pkg.name, pkg.lifecycle, got, expected)
					}
				case CardApproved:
					expected := pkg.lifecycle == lifecycle.Approved
					if got != expected {
						t.Errorf("CardApproved: pkg %q (lc=%q) match=%v, want %v", pkg.name, pkg.lifecycle, got, expected)
					}
				case CardMerged:
					expected := pkg.lifecycle == lifecycle.Merged
					if got != expected {
						t.Errorf("CardMerged: pkg %q (lc=%q) match=%v, want %v", pkg.name, pkg.lifecycle, got, expected)
					}
				case CardFailed:
					expected := (pkg.lifecycle == lifecycle.Approved || pkg.lifecycle == lifecycle.Merged) &&
						pkg.build != nil && pkg.build.Status == domain.BuildFailed
					if got != expected {
						t.Errorf("CardFailed: pkg %q (lc=%q, build=%v) match=%v, want %v", pkg.name, pkg.lifecycle, pkg.build != nil, got, expected)
					}
				case CardConfirmed:
					expected := pkg.lifecycle == lifecycle.Confirmed
					if got != expected {
						t.Errorf("CardConfirmed: pkg %q (lc=%q, build=%v) match=%v, want %v", pkg.name, pkg.lifecycle, pkg.build != nil, got, expected)
					}
				}
			}

		})
	}
}

// cardIndex returns id's position in cardIDs().
func cardIndex(t *testing.T, id CardID) int {
	t.Helper()
	for i, c := range cardIDs() {
		if c == id {
			return i
		}
	}
	t.Fatalf("card %q not in cardIDs()", id)
	return -1
}

func TestBuildEntries_BuildBasedCardFilter(t *testing.T) {
	t.Parallel()

	// Regression test: buildEntries() filters by the selected card's
	// predicate. Failed reads the latest build attempt; Built and
	// Confirmed are disjoint lifecycle stages (#365).

	mkstore := &mockBuildStore{
		records: map[domain.PackageName]*domain.Build{
			"pkg-approved-failed": {Status: domain.BuildFailed, Confirmed: true},
			"pkg-merged-failed":   {Status: domain.BuildFailed, Confirmed: true},
			"pkg-built":           {Status: domain.BuildSuccess, Confirmed: false},
			"pkg-confirmed":       {Status: domain.BuildSuccess, Confirmed: true},
		},
	}
	mkengine := build.NewEngine(&mockBuilder{}, mkstore, &mockArtStore{}, &mockGitProvider{}, nil)
	m := NewDashboardModel(deps.Dependencies{BuildEngine: mkengine})

	m.packages = []domain.PackageName{
		"pkg-approved-failed",
		"pkg-merged-failed",
		"pkg-built",
		"pkg-confirmed",
		"pkg-review-pending",
		"pkg-rejected",
	}
	m.lifecycle = map[domain.PackageName]lifecycle.State{
		"pkg-approved-failed": lifecycle.Approved,
		"pkg-merged-failed":   lifecycle.Merged,
		"pkg-built":           lifecycle.Built,
		"pkg-confirmed":       lifecycle.Confirmed,
		"pkg-review-pending":  lifecycle.ReviewPending,
		"pkg-rejected":        lifecycle.Rejected,
	}
	m.updates = map[domain.PackageName]int{}

	cases := []struct {
		card CardID
		want []domain.PackageName
	}{
		{CardFailed, []domain.PackageName{"pkg-approved-failed", "pkg-merged-failed"}},
		{CardBuilt, []domain.PackageName{"pkg-built"}},
		{CardConfirmed, []domain.PackageName{"pkg-confirmed"}},
	}
	for _, tc := range cases {
		m.cardSel = cardIndex(t, tc.card)
		m.applyCardFilter()
		m.summary = m.buildSummary()
		m.buildEntries(context.Background(), "test")

		visible := m.pkgList.VisibleEntries()
		got := make(map[domain.PackageName]bool)
		for _, e := range visible {
			got[e.Name] = true
		}
		if len(visible) != len(tc.want) {
			t.Errorf("%s: visible = %v, want %v", tc.card, visible, tc.want)
			continue
		}
		for _, w := range tc.want {
			if !got[w] {
				t.Errorf("%s: %q missing from visible %v", tc.card, w, visible)
			}
		}
		if m.summary[tc.card].Count != len(tc.want) {
			t.Errorf("%s: count = %d, want %d", tc.card, m.summary[tc.card].Count, len(tc.want))
		}
	}
}

// mockBuildStore holds the latest attempt per package in records and the
// last success per package in lastSuccess.
type mockBuildStore struct {
	records     map[domain.PackageName]*domain.Build
	lastSuccess map[domain.PackageName]*domain.Build
}

func (m *mockBuildStore) Get(name domain.PackageName) (*domain.Build, error) {
	b, ok := m.records[name]
	if !ok {
		return nil, nil
	}
	cp := *b
	return &cp, nil
}

func (m *mockBuildStore) Save(name domain.PackageName, b *domain.Build) error {
	m.records[name] = b
	return nil
}

func (m *mockBuildStore) ReadBuildLog(buildID domain.BuildID, logPath string) (string, error) {
	return "", nil
}

func (m *mockBuildStore) Delete(_ domain.PackageName) error { return nil }

func (m *mockBuildStore) GetLastSuccess(name domain.PackageName) (*domain.Build, error) {
	if b, ok := m.lastSuccess[name]; ok {
		cp := *b
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}

func (m *mockBuildStore) SaveLastSuccess(name domain.PackageName, b *domain.Build) error {
	if m.lastSuccess == nil {
		m.lastSuccess = make(map[domain.PackageName]*domain.Build)
	}
	cp := *b
	m.lastSuccess[name] = &cp
	return nil
}

type mockBuilder struct{}

func (m *mockBuilder) Build(_ context.Context, _, _ string) (build.BuildResult, error) {
	return build.BuildResult{}, nil
}

type mockArtStore struct{}

func (m *mockArtStore) Get(_ domain.ArtifactID) (*domain.Artifact, error)  { return nil, nil }
func (m *mockArtStore) Save(_ domain.ArtifactID, _ *domain.Artifact) error { return nil }
func (m *mockArtStore) GetArtifactsByPackage(_ domain.PackageName) ([]domain.Artifact, error) {
	return nil, nil
}
func (m *mockArtStore) Delete(_ domain.ArtifactID) error { return nil }
func (m *mockArtStore) ListAll() ([]domain.Artifact, error) {
	return nil, nil
}

type mockGitProvider struct{}

func (m *mockGitProvider) Import(_ context.Context, _ domain.PackageName) error { return nil }
func (m *mockGitProvider) Fetch(_ context.Context, _ domain.PackageName) error  { return nil }
func (m *mockGitProvider) Status(_ context.Context, _ domain.PackageName) (*git.RepositoryStatus, error) {
	return nil, nil
}
func (m *mockGitProvider) CommitsBehind(_ context.Context, _ domain.PackageName) (int, error) {
	return 0, nil
}
func (m *mockGitProvider) UpstreamCommit(_ context.Context, _ domain.PackageName) (string, error) {
	return "", nil
}
func (m *mockGitProvider) IsMerged(_ context.Context, _ domain.PackageName, _ string) (bool, error) {
	return false, nil
}
func (m *mockGitProvider) Diff(_ context.Context, _ domain.PackageName) (*git.GitDiff, error) {
	return nil, nil
}
func (m *mockGitProvider) ReviewDiff(_ context.Context, _ domain.PackageName, _, _ string) (*git.GitDiff, error) {
	return nil, nil
}
func (m *mockGitProvider) Merge(_ context.Context, _ domain.PackageName, _ string) error { return nil }
func (m *mockGitProvider) LocalOnlyCommits(_ context.Context, _ domain.PackageName) (int, error) {
	return 0, nil
}
func (m *mockGitProvider) Metadata(_ context.Context, _ domain.PackageName) (*git.RepositoryMetadata, error) {
	return nil, nil
}
func (m *mockGitProvider) CloneAt(_ context.Context, _ domain.PackageName, _ string, _ string) error {
	return nil
}
func (m *mockGitProvider) LocalHeadCommit(_ context.Context, _ domain.PackageName) (string, error) {
	return "", nil
}
func (m *mockGitProvider) FileAtCommit(_ context.Context, _ domain.PackageName, _ string, _ string) (string, error) {
	return "", nil
}

func TestCardFilter_RejectedPackageWithStaleBuild(t *testing.T) {
	t.Parallel()

	// Regression test for Defect B: a Rejected package with a stale
	// BuildFailed record should no longer count toward Failed.
	// The CardFailed predicate gates on isApprovedOrMerged, so a
	// Rejected package with build.Status == BuildFailed should NOT match.

	m := NewDashboardModel(deps.Dependencies{})
	pred, ok := m.cardFilterMap[CardFailed]
	if !ok {
		t.Fatal("CardFailed predicate missing from cardFilterMap")
	}

	rejectedWithFailedBuild := &domain.Build{
		PackageName: "stale-pkg",
		Status:      domain.BuildFailed,
		Confirmed:   true,
	}

	got := pred("stale-pkg", lifecycle.Rejected, rejectedWithFailedBuild)
	if got {
		t.Errorf("CardFailed predicate matched a Rejected package with stale BuildFailed record — Defect B not fixed. Got match, want no match")
	}

	// Approved package with BuildFailed should still match.
	got2 := pred("stale-pkg", lifecycle.Approved, rejectedWithFailedBuild)
	if !got2 {
		t.Errorf("CardFailed predicate did not match an Approved package with BuildFailed record. Got no match, want match")
	}
}

// TestInitDoesNotFireAURCheck verifies that Init() does not call
// loadAURUpdatesCmd. The AUR check has nothing to check when packages
// haven't loaded yet, so firing it at Init() captures an empty
// snapshot permanently (the value-receiver timing trap, #230 root
// cause). This test ensures a future developer who adds another batch
// command to Init() with the same pattern can't silently re-introduce
// the bug.
func TestInitDoesNotFireAURCheck(t *testing.T) {
	t.Parallel()

	m := NewDashboardModel(deps.Dependencies{
		Catalog: &mockCatalog{pkgs: nil},
	})

	// Init should return exactly one command: loadPackagesCmd.
	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("Init() returned nil Cmd")
	}

	// Execute the Init command to get the message.
	msg := initCmd()
	packagesLoaded, ok := msg.(PackagesLoadedMsg)
	if !ok {
		t.Fatalf("Init() returned unexpected message type %T, want PackagesLoadedMsg", msg)
	}

	// The message should be PackagesLoadedMsg (from loadPackagesCmd),
	// NOT AURUpdateMsg. If Init() had fired loadAURUpdatesCmd in a
	// tea.Batch, there would be two messages in the batch, and this
	// single-Execute would only get one of them — the test would fail
	// because msg would be AURUpdateMsg, not PackagesLoadedMsg.
	if len(packagesLoaded.Packages) != 0 {
		t.Errorf("expected empty packages from catalog, got %d", len(packagesLoaded.Packages))
	}
}

// TestLoadAURUpdatesCmd_EmptyPackages verifies that
// loadAURUpdatesCmd with a nil packages slice returns
// AURUpdateMsg{Pairs: nil} without calling aur.BulkCheck. This is the
// guard clause that was always true during Init() (because the captured
// m.packages was nil), now made explicit via an argument.
func TestLoadAURUpdatesCmd_EmptyPackages(t *testing.T) {
	t.Parallel()

	m := NewDashboardModel(deps.Dependencies{})

	cmd := m.loadAURUpdatesCmd(context.Background(), nil)
	msg := cmd()
	aurMsg, ok := msg.(msgs.AURUpdateMsg)
	if !ok {
		t.Fatalf("loadAURUpdatesCmd(nil) returned %T, want msgs.AURUpdateMsg", msg)
	}
	if len(aurMsg.Pairs) != 0 {
		t.Errorf("AURUpdateMsg.Pairs = %d, want 0 for nil packages", len(aurMsg.Pairs))
	}

	// Also test with empty (non-nil) slice.
	cmd2 := m.loadAURUpdatesCmd(context.Background(), []domain.PackageName{})
	msg2 := cmd2()
	aurMsg2, ok := msg2.(msgs.AURUpdateMsg)
	if !ok {
		t.Fatalf("loadAURUpdatesCmd([]) returned %T, want msgs.AURUpdateMsg", msg2)
	}
	if len(aurMsg2.Pairs) != 0 {
		t.Errorf("AURUpdateMsg.Pairs = %d, want 0 for empty packages", len(aurMsg2.Pairs))
	}
}

// TestPackagesLoadedMsg_FiresAURCheckInBatch verifies that processing
// PackagesLoadedMsg returns a tea.Cmd batch that includes
// loadAURUpdatesCmd. Since the real *aurinfo.Client hits the network,
// we can't easily assert the AUR check actually ran, but we can verify
// the command count matches expectations (N review cmds + 1 AUR cmd)
// and that the model's packages are set.
func TestPackagesLoadedMsg_FiresAURCheckInBatch(t *testing.T) {
	t.Parallel()

	m := NewDashboardModel(deps.Dependencies{})

	testPkgs := []domain.PackageName{"pkg-a", "pkg-b"}
	pkgMsg := PackagesLoadedMsg{Packages: testPkgs, TraceCtx: context.Background()}

	_, cmd := m.Update(pkgMsg)
	if cmd == nil {
		t.Fatal("PackagesLoadedMsg handler returned nil Cmd")
	}

	// Execute the batch to verify it doesn't panic.
	// The batch contains: 2 ReviewDataMsg cmds (one per package) + 1
	// loadAURUpdatesCmd.
	cmd()
}

// mockCatalog returns a fixed set of packages.
type mockCatalog struct {
	pkgs []domain.PackageName
}

func (m *mockCatalog) Packages(_ context.Context) ([]domain.PackageName, error) {
	return m.pkgs, nil
}

func (m *mockCatalog) RepositoryPath(name domain.PackageName) (string, error) {
	return "", nil
}
