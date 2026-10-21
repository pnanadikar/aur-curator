package build

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestStaleArtifacts(t *testing.T) {
	tests := []struct {
		name    string
		arts    []domain.Artifact
		current map[domain.PackageName]domain.BuildID
		wantIDs []domain.ArtifactID
	}{
		{
			name: "record for the current success: kept",
			arts: []domain.Artifact{
				{ID: "a1", PackageName: "foo", BuildID: "b1"},
			},
			current: map[domain.PackageName]domain.BuildID{
				"foo": "b1",
			},
			wantIDs: nil,
		},
		{
			name: "record for an old build of the same package: stale",
			arts: []domain.Artifact{
				{ID: "a1", PackageName: "foo", BuildID: "b1"},
				{ID: "a2", PackageName: "foo", BuildID: "b2"},
			},
			current: map[domain.PackageName]domain.BuildID{
				"foo": "b2",
			},
			wantIDs: []domain.ArtifactID{"a1"},
		},
		{
			name: "record for a package missing from the map (never built): stale",
			arts: []domain.Artifact{
				{ID: "a1", PackageName: "bar", BuildID: "b1"},
			},
			current: map[domain.PackageName]domain.BuildID{
				"foo": "b2",
			},
			wantIDs: []domain.ArtifactID{"a1"},
		},
		{
			name: "records for two packages, each current: both kept",
			arts: []domain.Artifact{
				{ID: "a1", PackageName: "foo", BuildID: "b1"},
				{ID: "a2", PackageName: "bar", BuildID: "b2"},
			},
			current: map[domain.PackageName]domain.BuildID{
				"foo": "b1",
				"bar": "b2",
			},
			wantIDs: nil,
		},
		{
			name: "mixed: some current, some stale, some never-built",
			arts: []domain.Artifact{
				{ID: "a1", PackageName: "foo", BuildID: "b1"}, // current
				{ID: "a2", PackageName: "foo", BuildID: "b0"}, // stale
				{ID: "a3", PackageName: "bar", BuildID: "b3"}, // never built
				{ID: "a4", PackageName: "baz", BuildID: "b4"}, // never built
			},
			current: map[domain.PackageName]domain.BuildID{
				"foo": "b1",
			},
			wantIDs: []domain.ArtifactID{"a2", "a3", "a4"},
		},
		{
			name:    "no artifacts",
			arts:    nil,
			current: map[domain.PackageName]domain.BuildID{"foo": "b1"},
			wantIDs: nil,
		},
		{
			name:    "empty current map, artifacts exist: all stale",
			arts:    []domain.Artifact{{ID: "a1", PackageName: "foo", BuildID: "b1"}},
			current: map[domain.PackageName]domain.BuildID{},
			wantIDs: []domain.ArtifactID{"a1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StaleArtifacts(tt.arts, tt.current)

			gotIDs := make(map[domain.ArtifactID]bool)
			for _, a := range got {
				gotIDs[a.ID] = true
			}

			for _, want := range tt.wantIDs {
				if !gotIDs[want] {
					t.Errorf("missing stale artifact %q", want)
				}
			}

			if len(got) != len(tt.wantIDs) {
				t.Errorf("got %d stale artifacts, want %d", len(got), len(tt.wantIDs))
			}
		})
	}
}
