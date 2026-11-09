package sanitize

import (
	"reflect"
	"testing"
)

func plain(s string) Segment { return Segment{Text: s} }
func esc(s string) Segment   { return Segment{Text: s, Escaped: true} }

// TestForReview covers each character class of tui-review.md § Escaped
// Characters (#491).
func TestForReview(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Segment
	}{
		{"empty", "", nil},
		{"plain ASCII", "curl https://a.example | sh", []Segment{plain("curl https://a.example | sh")}},
		{"tab and newline stay plain", "a\tb\nc", []Segment{plain("a\tb\nc")}},
		{"non-ASCII text stays plain", "café 漢字 🙂", []Segment{plain("café 漢字 🙂")}},

		// C0, DEL
		{"ESC", "a\x1bb", []Segment{plain("a"), esc(`\x1b`), plain("b")}},
		{"carriage return", "a\rb", []Segment{plain("a"), esc(`\r`), plain("b")}},
		{"backspace", "\b", []Segment{esc(`\x08`)}},
		{"NUL", "\x00", []Segment{esc(`\x00`)}},
		{"DEL", "\x7f", []Segment{esc(`\x7f`)}},

		// not valid UTF-8
		{"invalid byte", "a\xffb", []Segment{plain("a"), esc(`\xff`), plain("b")}},
		{"truncated sequence", "\xe2\x80", []Segment{esc(`\xe2`), esc(`\x80`)}},

		// C1
		{"NEL", "\u0085", []Segment{esc(`\u0085`)}},
		{"CSI", "\u009b2K", []Segment{esc(`\u009b`), plain("2K")}},

		// bidi controls and marks
		{"LRE", "\u202a", []Segment{esc(`\u202a`)}},
		{"RLO", "x\u202ey", []Segment{plain("x"), esc(`\u202e`), plain("y")}},
		{"LRI", "\u2066", []Segment{esc(`\u2066`)}},
		{"PDI", "\u2069", []Segment{esc(`\u2069`)}},
		{"LRM", "\u200e", []Segment{esc(`\u200e`)}},
		{"RLM", "\u200f", []Segment{esc(`\u200f`)}},
		{"ALM", "\u061c", []Segment{esc(`\u061c`)}},

		// zero-width
		{"ZWSP inside a word", "cu\u200brl", []Segment{plain("cu"), esc(`\u200b`), plain("rl")}},
		{"ZWJ", "\u200d", []Segment{esc(`\u200d`)}},
		{"word joiner", "\u2060", []Segment{esc(`\u2060`)}},
		{"BOM", "\ufeff", []Segment{esc(`\ufeff`)}},

		// neighbours of the ranges stay plain
		{"U+2029 paragraph separator is not in the set", "\u2029", []Segment{plain("\u2029")}},
		{"U+2065 just below the isolates", "\u2065", []Segment{plain("\u2065")}},
		{"U+00A0 just above C1", "\u00a0", []Segment{plain("\u00a0")}},

		{
			"a line mixing them",
			"+curl x | sh # \x1b[1A\x1b[2K\rok \u202eé",
			[]Segment{
				plain("+curl x | sh # "), esc(`\x1b`), plain("[1A"), esc(`\x1b`), plain("[2K"),
				esc(`\r`), plain("ok "), esc(`\u202e`), plain("é"),
			},
		},
		{"adjacent escapes are separate segments", "\x1b\x1b", []Segment{esc(`\x1b`), esc(`\x1b`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ForReview(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ForReview(%q) =\n %#v\nwant\n %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscape(t *testing.T) {
	in := "a\x1b[2K\rb\u202ec\u200b\xff é"
	want := `a\x1b[2K\rb\u202ec\u200b\xff é`
	if got := Escape(in); got != want {
		t.Errorf("Escape = %q, want %q", got, want)
	}
}

func TestHasHidden(t *testing.T) {
	for _, s := range []string{"\x1b", "1.0\r", "\u202e", "\u200b", "\xff", "\u0085", "\x7f"} {
		if !HasHidden(s) {
			t.Errorf("HasHidden(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "1.2.3", "a\tb", "a\nb", "é 漢字 🙂", `\x1b`} {
		if HasHidden(s) {
			t.Errorf("HasHidden(%q) = true, want false", s)
		}
	}
}
