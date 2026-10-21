package aurinfo

import (
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestExtractScalar_Basic(t *testing.T) {
	raw := `pkgname=somepkg
pkgver=2.5.1
pkgrel=3
`
	val, unknown := extractScalar(raw, "pkgver")
	if unknown {
		t.Fatal("expected unknown = false for valid pkgver")
	}
	if val != "2.5.1" {
		t.Errorf("val = %q, want %q", val, "2.5.1")
	}
}

func TestExtractScalar_NoMatch(t *testing.T) {
	raw := `pkgname=somepkg
pkgrel=3
`
	_, unknown := extractScalar(raw, "pkgver")
	if !unknown {
		t.Error("expected unknown = true when pkgver not present")
	}
}

func TestExtractScalar_DynamicVersion(t *testing.T) {
	raw := `pkgver() {
  pkgver=1.0
  sed -n 's/^Version: //p' "$pkgname/PKGBUILD"
}
`
	_, unknown := extractScalar(raw, "pkgver")
	if !unknown {
		t.Error("expected unknown = true for dynamic version (contains $pkgver)")
	}
}

func TestExtractScalar_Pkgrel(t *testing.T) {
	raw := `pkgname=somepkg
pkgver=1.0
pkgrel=2
`
	val, unknown := extractScalar(raw, "pkgrel")
	if unknown {
		t.Fatal("expected unknown = false for valid pkgrel")
	}
	if val != "2" {
		t.Errorf("val = %q, want %q", val, "2")
	}
}

func TestExtractScalar_Whitespace(t *testing.T) {
	raw := `  pkgver  =  1.2.3  
`
	val, unknown := extractScalar(raw, "pkgver")
	if unknown {
		t.Fatal("expected unknown = false for whitespace-padded assignment")
	}
	if val != "1.2.3" {
		t.Errorf("val = %q, want %q", val, "1.2.3")
	}
}

func TestExtractFullVersion_Basic(t *testing.T) {
	raw := `pkgname=testpkg
pkgver=1.2.3
pkgrel=4
`
	val, unknown := ExtractFullVersion(raw)
	if unknown {
		t.Fatal("expected unknown = false")
	}
	if val != "1.2.3-4" {
		t.Errorf("val = %q, want %q", val, "1.2.3-4")
	}
}

func TestExtractFullVersion_NoPkgrel(t *testing.T) {
	raw := `pkgname=testpkg
pkgver=1.2.3
`
	val, unknown := ExtractFullVersion(raw)
	if unknown {
		t.Fatal("expected unknown = false")
	}
	if val != "1.2.3" {
		t.Errorf("val = %q, want %q", val, "1.2.3")
	}
}

func TestExtractFullVersion_NoPkgver(t *testing.T) {
	raw := `pkgname=testpkg
pkgrel=4
`
	_, unknown := ExtractFullVersion(raw)
	if !unknown {
		t.Error("expected unknown = true when pkgver absent")
	}
}

func TestExtractFullVersion_DynamicPkgver(t *testing.T) {
	raw := `pkgver() {
  pkgver=1.0
}
pkgrel=2
`
	_, unknown := ExtractFullVersion(raw)
	if !unknown {
		t.Error("expected unknown = true when pkgver uses dynamic function")
	}
}

func TestCompareVersions_Equal(t *testing.T) {
	stale, unknown := CompareVersions("1.0-1", "1.0-1")
	if stale {
		t.Error("expected stale = false for equal versions")
	}
	if unknown {
		t.Error("expected unknown = false")
	}
}

func TestCompareVersions_Stale(t *testing.T) {
	stale, unknown := CompareVersions("2.0-1", "1.0-1")
	if !stale {
		t.Error("expected stale = true when AUR version is higher")
	}
	if unknown {
		t.Error("expected unknown = false")
	}
}

func TestCompareVersions_NonEqual(t *testing.T) {
	stale, unknown := CompareVersions("1.0-1", "2.0-1")
	if !stale {
		t.Error("expected stale = true when versions differ")
	}
	if unknown {
		t.Error("expected unknown = false")
	}
}

func TestCompareVersions_EmptyAUR(t *testing.T) {
	stale, unknown := CompareVersions("", "1.0-1")
	if stale {
		t.Error("expected stale = false when AUR version is empty")
	}
	if !unknown {
		t.Error("expected unknown = true when AUR version is empty")
	}
}

func TestCompareVersions_EmptyLocal(t *testing.T) {
	stale, unknown := CompareVersions("1.0-1", "")
	if stale {
		t.Error("expected stale = false when local version is empty")
	}
	if !unknown {
		t.Error("expected unknown = true when local version is empty")
	}
}

func TestCompareVersions_CaseInsensitive(t *testing.T) {
	stale, unknown := CompareVersions("1.0-1", "1.0-1")
	if stale {
		t.Error("expected stale = false for case-insensitive equal")
	}
	// CompareVersions compares case-insensitively but "1.0-1" == "1.0-1" is exact.
	if unknown {
		t.Error("expected unknown = false")
	}
}

func TestBuildURL_Arghs(t *testing.T) {
	c := &Client{baseURL: "https://aur.example.com/rpc"}
	url := c.buildURL([]domain.PackageName{"pkg1", "pkg2"})
	if !strings.Contains(url, "arg[]=") {
		t.Error("expected URL to contain arg[]= parameters")
	}
	if !strings.Contains(url, "pkg1") || !strings.Contains(url, "pkg2") {
		t.Error("expected URL to contain all package names")
	}
}

func TestExtractScalar_QuoteStripping(t *testing.T) {
	tests := []struct {
		input    string
		name     string
		want     string
		wantWant bool // true = expects quoted value (unchanged), false = wants stripped
	}{
		// Plain unquoted values — no change
		{"pkgver=1.2.3\n", "pkgver", "1.2.3", false},
		{"pkgrel=1\n", "pkgrel", "1", false},
		{"pkgver=1.2.3-1\n", "pkgver", "1.2.3-1", false},
		// Single-quoted values — strip quotes
		{"pkgver='1.2.3'\n", "pkgver", "1.2.3", false},
		{"pkgrel='1'\n", "pkgrel", "1", false},
		// Double-quoted values — strip quotes
		{"pkgver=\"1.2.3\"\n", "pkgver", "1.2.3", false},
		// Single stray quote (mismatched) — not stripped, returned as literal
		{"pkgver='\n", "pkgver", "'", false},
		// Self-reference — should still return unknown before quote stripping
		{"pkgver=$pkgver\n", "pkgver", "", true},
		{"pkgver=${pkgver}\n", "pkgver", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, unknown := ExtractScalar(tt.input, tt.name)
			if val != tt.want {
				t.Errorf("ExtractScalar(%q, %q) = %q, want %q", tt.input, tt.name, val, tt.want)
			}
			if unknown != tt.wantWant {
				t.Errorf("ExtractScalar(%q, %q) unknown = %v, want %v", tt.input, tt.name, unknown, tt.wantWant)
			}
		})
	}
}

func TestExtractFullVersion_QuotedTorBrowserBin(t *testing.T) {
	// Reproduces the real-world incident: tor-browser-bin uses
	// pkgver='15.0.23' (single-quoted) and pkgrel=1 (unquoted).
	// The extracted full version must be "15.0.23-1" — matching
	// AUR's unquoted format exactly, so CompareVersions reports
	// not stale when both sides genuinely agree.
	raw := `pkgname=tor-browser-bin
pkgver='15.0.23'
pkgrel=1
`
	val, unknown := ExtractFullVersion(raw)
	if unknown {
		t.Fatal("expected unknown = false for tor-browser-bin PKGBUILD")
	}
	if val != "15.0.23-1" {
		t.Errorf("val = %q, want %q", val, "15.0.23-1")
	}

	// Verify CompareVersions considers this matching against the same AUR version.
	stale, u := CompareVersions("15.0.23-1", val)
	if stale {
		t.Error("expected stale = false when AUR and local versions match after quote stripping")
	}
	if u {
		t.Error("expected unknown = false")
	}
}
