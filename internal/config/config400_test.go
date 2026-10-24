package config

import "testing"

// TestGiteaTokenSource400: the source follows Load's precedence — the
// environment variable when set, otherwise the file's key (#400).
func TestGiteaTokenSource400(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "from-env")
	if got, want := GiteaTokenSource("/x/config.jsonc"), "AUR_CURATOR_GITEA_TOKEN (environment)"; got != want {
		t.Errorf("env set: got %q, want %q", got, want)
	}
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	if got, want := GiteaTokenSource("/x/config.jsonc"), "giteaToken in /x/config.jsonc"; got != want {
		t.Errorf("env unset: got %q, want %q", got, want)
	}
}
