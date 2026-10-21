package build

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests for #378: a checksum that cannot be computed is an error, never an
// empty string recorded on the artifact.

// TestSha256HashKnownContent pins the digest format: lowercase hex SHA-256.
func TestSha256HashKnownContent(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "abc.bin")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sha256Hash(p)
	if err != nil {
		t.Fatalf("sha256Hash: %v", err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("sha256Hash = %q, want %q", got, want)
	}
}

// TestSha256HashMissingFileIsError: before #378 a read failure returned an
// empty digest and no error.
func TestSha256HashMissingFileIsError(t *testing.T) {
	t.Parallel()
	got, err := sha256Hash(filepath.Join(t.TempDir(), "missing.pkg.tar.zst"))
	if err == nil {
		t.Fatalf("sha256Hash = %q with no error, want an error", got)
	}
	if got != "" {
		t.Fatalf("sha256Hash = %q on error, want empty", got)
	}
}

// TestExtractArtifactChecksumErrorFails: a path that stats and parses as an
// artifact name but cannot be read (here, a directory) must make
// extractArtifact fail instead of returning an artifact with no checksum.
func TestExtractArtifactChecksumErrorFails(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "foo-1.0-1-x86_64.pkg.tar.zst")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	a, err := extractArtifact(p)
	if err == nil {
		t.Fatalf("extractArtifact = %+v with no error, want a checksum error", a)
	}
}
