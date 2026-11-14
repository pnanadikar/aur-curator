package screens

import (
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// TestBuildLogHeaderNetwork522: the header names the network the build
// had, from its log's own line; a log without it gives no network part
// (tui-package-details.md § Build Log, #522).
func TestBuildLogHeaderNetwork522(t *testing.T) {
	t.Parallel()
	env := "aur-curator: build environment: PATH\n"
	for _, tc := range []struct {
		name, log, want string
	}{
		{"download only", env + "aur-curator: network: for downloading only\n==> Making package: yay\n", header507("success") + " · network for downloading only"},
		{"whole build", env + "aur-curator: network: for the whole build (buildNetwork)\n==> Making package: yay\n", header507("success") + " · network for the whole build"},
		{"before #522", env + "==> Making package: yay\n", header507("success")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := detailWithLog(&logStore{build: build507(domain.BuildSuccess), log: tc.log})
			m.headCommit = built507
			m = loadLog507(m)
			checkHeader507(t, m.contentPane.Content(), tc.want, "")
		})
	}
}
