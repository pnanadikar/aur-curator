package git

import "testing"

func TestHasExpectedRemotes(t *testing.T) {
	tests := []struct {
		name   string
		meta   RepositoryMetadata
		expect bool
	}{
		{
			name:   "both remotes present",
			meta:   RepositoryMetadata{OriginURL: "https://gitea.example.com/aur/foo.git", UpstreamURL: "https://aur.archlinux.org/foo.git"},
			expect: true,
		},
		{
			name:   "both remotes empty",
			meta:   RepositoryMetadata{},
			expect: false,
		},
		{
			name:   "only origin present",
			meta:   RepositoryMetadata{OriginURL: "https://gitea.example.com/aur/foo.git"},
			expect: false,
		},
		{
			name:   "only upstream present",
			meta:   RepositoryMetadata{UpstreamURL: "https://aur.archlinux.org/foo.git"},
			expect: false,
		},
		{
			name:   "origin whitespace-only",
			meta:   RepositoryMetadata{OriginURL: "   ", UpstreamURL: "https://aur.archlinux.org/foo.git"},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasExpectedRemotes(tt.meta)
			if got != tt.expect {
				t.Errorf("HasExpectedRemotes(%+v) = %v, want %v", tt.meta, got, tt.expect)
			}
		})
	}
}
