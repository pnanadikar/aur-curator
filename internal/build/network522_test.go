package build

import "testing"

// TestNetworkFromLog: only the second line counts, and only the two
// texts aur-curator writes (#522).
func TestNetworkFromLog(t *testing.T) {
	env := "aur-curator: build environment: PATH\n"
	for _, tc := range []struct {
		name      string
		log       string
		whole, ok bool
	}{
		{"download", env + "aur-curator: network: for downloading only\n==> Making package\n", false, true},
		{"whole", env + "aur-curator: network: for the whole build (buildNetwork)\n", true, true},
		{"no trailing newline", env + "aur-curator: network: for downloading only", false, true},
		{"before #522", env + "==> Making package\n", false, false},
		{"empty", "", false, false},
		{"one line", env, false, false},
		{"printed by the build", env + "==> Starting build()\naur-curator: network: for the whole build (buildNetwork)\n", false, false},
		{"unknown text", env + "aur-curator: network: everywhere\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			whole, ok := NetworkFromLog(tc.log)
			if whole != tc.whole || ok != tc.ok {
				t.Errorf("NetworkFromLog = %v, %v; want %v, %v", whole, ok, tc.whole, tc.ok)
			}
		})
	}
	// What the builder writes, it reads back.
	for _, n := range []bool{false, true} {
		if whole, ok := NetworkFromLog(env + networkLogLine(n) + "\n"); !ok || whole != n {
			t.Errorf("round trip %v: %v, %v", n, whole, ok)
		}
	}
}
