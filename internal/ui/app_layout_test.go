package ui

import "testing"

// #329 — App caps the screen body at the rows it was given, cutting from
// the bottom, so the toast and header always stay visible.
func TestCapBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		rows int
		want string
	}{
		{"fits", "a\nb\nc", 3, "a\nb\nc"},
		{"shorter", "a\nb", 3, "a\nb"},
		{"too tall: cut from the bottom", "a\nb\nc\nd\ne", 3, "a\nb\nc"},
		{"no size yet", "a\nb\nc", 0, "a\nb\nc"},
		{"negative (tiny window)", "a\nb\nc", -2, "a\nb\nc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capBody(tc.body, tc.rows); got != tc.want {
				t.Fatalf("capBody(%q, %d) = %q, want %q", tc.body, tc.rows, got, tc.want)
			}
		})
	}
}
