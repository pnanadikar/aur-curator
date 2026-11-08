package build

import (
	"slices"
	"strings"
	"testing"
)

func TestCleanEnv(t *testing.T) {
	tests := []struct {
		name        string
		environ     []string
		wantKept    []string
		wantRemoved []string
	}{
		{
			name:        "empty input",
			environ:     nil,
			wantKept:    []string{},
			wantRemoved: nil,
		},
		{
			name:        "nothing denied: everything kept in order",
			environ:     []string{"PATH=/usr/bin", "HOME=/home/u", "LANG=C"},
			wantKept:    []string{"PATH=/usr/bin", "HOME=/home/u", "LANG=C"},
			wantRemoved: nil,
		},
		{
			name: "every exact name removed",
			environ: []string{
				"SSH_AUTH_SOCK=/run/user/1000/ssh-agent.socket",
				"SSH_AGENT_PID=42",
				"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus",
				"SSH_ASKPASS=/usr/lib/ssh/x11-ssh-askpass",
				"SUDO_ASKPASS=/usr/bin/askpass",
				"GIT_ASKPASS=/usr/bin/askpass",
				"DISPLAY=:0",
				"WAYLAND_DISPLAY=wayland-0",
				"XAUTHORITY=/home/u/.Xauthority",
			},
			wantKept: []string{},
			wantRemoved: []string{
				"DBUS_SESSION_BUS_ADDRESS", "DISPLAY", "GIT_ASKPASS",
				"SSH_AGENT_PID", "SSH_ASKPASS", "SSH_AUTH_SOCK",
				"SUDO_ASKPASS", "WAYLAND_DISPLAY", "XAUTHORITY",
			},
		},
		{
			name:        "AUR_CURATOR_ prefix removed",
			environ:     []string{"AUR_CURATOR_GITEA_TOKEN=secret", "AUR_CURATOR_TRACE=1", "PATH=/usr/bin"},
			wantKept:    []string{"PATH=/usr/bin"},
			wantRemoved: []string{"AUR_CURATOR_GITEA_TOKEN", "AUR_CURATOR_TRACE"},
		},
		{
			name:        "credential suffixes removed",
			environ:     []string{"GITHUB_TOKEN=a", "GH_TOKEN=b", "FOO_SECRET=c", "DB_PASSWORD=d", "PATH=/usr/bin"},
			wantKept:    []string{"PATH=/usr/bin"},
			wantRemoved: []string{"DB_PASSWORD", "FOO_SECRET", "GH_TOKEN", "GITHUB_TOKEN"},
		},
		{
			name: "names that only contain a denied name are kept",
			environ: []string{
				"MY_SSH_AUTH_SOCK=x",
				"SSH_AUTH_SOCK_OLD=x",
				"TOKEN_FILE=x",
				"XAUR_CURATOR_X=x",
				"STARSHIP_SESSION_KEY=x",
				"DISPLAYS=x",
			},
			wantKept: []string{
				"MY_SSH_AUTH_SOCK=x",
				"SSH_AUTH_SOCK_OLD=x",
				"TOKEN_FILE=x",
				"XAUR_CURATOR_X=x",
				"STARSHIP_SESSION_KEY=x",
				"DISPLAYS=x",
			},
			wantRemoved: nil,
		},
		{
			name:        "kept entries stay in order around removed ones",
			environ:     []string{"A=1", "SSH_AUTH_SOCK=s", "B=2", "DISPLAY=:0", "C=3"},
			wantKept:    []string{"A=1", "B=2", "C=3"},
			wantRemoved: []string{"DISPLAY", "SSH_AUTH_SOCK"},
		},
		{
			name:        "duplicate entries: removed name listed once",
			environ:     []string{"SSH_AUTH_SOCK=a", "PATH=/usr/bin", "SSH_AUTH_SOCK=b"},
			wantKept:    []string{"PATH=/usr/bin"},
			wantRemoved: []string{"SSH_AUTH_SOCK"},
		},
		{
			name:        "entry without = kept unchanged",
			environ:     []string{"SSH_AUTH_SOCK", "PATH=/usr/bin"},
			wantKept:    []string{"SSH_AUTH_SOCK", "PATH=/usr/bin"},
			wantRemoved: nil,
		},
		{
			name:        "empty value still removed",
			environ:     []string{"AUR_CURATOR_GITEA_TOKEN=", "PATH=/usr/bin"},
			wantKept:    []string{"PATH=/usr/bin"},
			wantRemoved: []string{"AUR_CURATOR_GITEA_TOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kept, removed := cleanEnv(tt.environ)
			if !slices.Equal(kept, tt.wantKept) {
				t.Errorf("kept = %q, want %q", kept, tt.wantKept)
			}
			if !slices.Equal(removed, tt.wantRemoved) {
				t.Errorf("removed = %q, want %q", removed, tt.wantRemoved)
			}
		})
	}
}

// TestCleanEnvRemovedHasNoValues checks that removed holds names only:
// a secret value must never reach the build log.
func TestCleanEnvRemovedHasNoValues(t *testing.T) {
	const secret = "s3cr3t-value"
	_, removed := cleanEnv([]string{
		"AUR_CURATOR_GITEA_TOKEN=" + secret,
		"GH_TOKEN=" + secret,
	})
	if len(removed) != 2 {
		t.Fatalf("removed = %q, want 2 names", removed)
	}
	for _, name := range removed {
		if strings.Contains(name, "=") || strings.Contains(name, secret) {
			t.Errorf("removed entry %q contains a value", name)
		}
	}
}
