package widgets

import (
	"testing"

	"github.com/pramod/aur-curator/internal/theme"
)

func TestFlexHeight(t *testing.T) {
	t.Parallel()

	frame := theme.SectionBorderStyle().GetVerticalFrameSize()

	tests := []struct {
		name  string
		total int
		fixed []string
		want  int
	}{
		{
			name:  "normal case",
			total: 40,
			fixed: []string{"line 1", "line 2"},
			want:  40 - frame - 2,
		},
		{
			name:  "floor case",
			total: 10,
			fixed: []string{"line 1", "line 2", "line 3"},
			want:  MinFlexHeight,
		},
		{
			name:  "blank entries count as one row each",
			total: 30,
			fixed: []string{"", "", "line"},
			want:  30 - frame - 3,
		},
		{
			name:  "multi-line parts add their row counts",
			total: 40,
			fixed: []string{"a\nb\nc", "d"},
			want:  40 - frame - 4,
		},
		{
			name:  "no fixed parts leaves frame only",
			total: 50,
			want:  50 - frame,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FlexHeight(tc.total, tc.fixed...)
			if got != tc.want {
				t.Errorf("FlexHeight(%d, %v) = %d, want %d", tc.total, tc.fixed, got, tc.want)
			}
		})
	}
}
