package aurinfo

import "testing"

// TestHiddenCharacterMakesVersionUnreadable491: a version holding a
// control, bidi or zero-width character is not readable, so it is never
// shown raw nor reported as up to date (#491).
func TestHiddenCharacterMakesVersionUnreadable491(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
	}{
		{"ESC in pkgver", "pkgver=1.0\x1b[2K\npkgrel=1\n"},
		{"carriage return in pkgver", "pkgver=9.9\r1.0\npkgrel=1\n"},
		{"bidi override in pkgrel", "pkgver=1.0\npkgrel=1\u202e\n"},
		{"zero-width space in a quoted pkgver", "pkgver='1.\u200b0'\npkgrel=1\n"},
		{"ESC in epoch", "epoch=1\x1b\npkgver=1.0\npkgrel=1\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if v, unknown := ExtractFullVersion(tt.raw); !unknown {
				t.Errorf("ExtractFullVersion = %q, want not readable", v)
			}
		})
	}

	if v, unknown := ExtractFullVersion("epoch=2\npkgver=1.2.3\npkgrel=4\n"); unknown || v != "2:1.2.3-4" {
		t.Errorf("ordinary version = %q (unknown %v), want 2:1.2.3-4", v, unknown)
	}
}
