package git

import (
	"reflect"
	"testing"
)

// TestDifftoolCmd: the reviewed range, the tab's paths, run in the clone,
// and no option of aur-curator's own: --no-prompt or a tool would override
// the user's git configuration (#488).
func TestDifftoolCmd(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		paths []string
		want  []string
	}{
		{"reviewed range", "base1", []string{"PKGBUILD"}, []string{"git", "difftool", "base1", "up1", "--", "PKGBUILD"}},
		{"several files", "base1", []string{"PKGBUILD", "LICENSE"}, []string{"git", "difftool", "base1", "up1", "--", "PKGBUILD", "LICENSE"}},
		{"first review: the empty tree", "", []string{"PKGBUILD"}, []string{"git", "difftool", emptyTreeHash, "up1", "--", "PKGBUILD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := DifftoolCmd("/ws/foo", tt.base, "up1", tt.paths)
			if !reflect.DeepEqual(cmd.Args, tt.want) {
				t.Errorf("Args = %q, want %q", cmd.Args, tt.want)
			}
			if cmd.Dir != "/ws/foo" {
				t.Errorf("Dir = %q, want /ws/foo", cmd.Dir)
			}
			if cmd.Process != nil {
				t.Error("command was started")
			}
		})
	}
}
