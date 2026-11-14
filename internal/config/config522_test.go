package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// configWithBuildNetwork returns a minimal valid config whose buildNetwork
// is the raw JSON value v.
func configWithBuildNetwork(v string) string {
	return `{"rootPath":"/tmp/aur-workspace","repositoryPath":"/tmp/repo",` +
		`"giteaBaseURL":"https://git.example.com","giteaOrg":"myorg","giteaToken":"dummy",` +
		`"buildNetwork":` + v + `}`
}

// TestLoad522_BuildNetwork: names are trimmed and repeats dropped, in
// order; absent means none (#522).
func TestLoad522_BuildNetwork(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	for _, tc := range []struct {
		name, value string
		want        []domain.PackageName
	}{
		{"two names", `["yay","tor-browser-bin"]`, []domain.PackageName{"yay", "tor-browser-bin"}},
		{"trimmed", `[" yay "]`, []domain.PackageName{"yay"}},
		{"repeat dropped", `["yay","rua","yay"," yay"]`, []domain.PackageName{"yay", "rua"}},
		{"empty list", `[]`, nil},
		{"null", `null`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadTestConfig(t, t.TempDir(), "config.json", configWithBuildNetwork(tc.value))
			if !reflect.DeepEqual(cfg.BuildNetwork, tc.want) {
				t.Errorf("BuildNetwork = %#v, want %#v", cfg.BuildNetwork, tc.want)
			}
		})
	}
	t.Run("absent", func(t *testing.T) {
		cfg := loadTestConfig(t, t.TempDir(), "config.json", makeMinConfig(nil))
		if cfg.BuildNetwork != nil {
			t.Errorf("BuildNetwork = %#v, want nil", cfg.BuildNetwork)
		}
	})
}

// TestLoad522_BuildNetworkInvalid: a name that breaks the AUR's rule, or
// a value that is not a list of strings, is an error naming the key.
func TestLoad522_BuildNetworkInvalid(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	for _, tc := range []struct{ name, value, want string }{
		{"upper case", `["Yay"]`, `invalid package name "Yay"`},
		{"path", `["../yay"]`, `invalid package name "../yay"`},
		{"leading dash", `["-yay"]`, `invalid package name "-yay"`},
		{"empty", `[""]`, "invalid package name: empty"},
		{"blank", `["  "]`, "invalid package name: empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(configWithBuildNetwork(tc.value)), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if err == nil {
				t.Fatal("Load returned nil error")
			}
			if !strings.Contains(err.Error(), "buildNetwork") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want it to name buildNetwork and contain %q", err, tc.want)
			}
		})
	}
	for _, v := range []string{`"yay"`, `[1]`, `{"yay":true}`} {
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, []byte(configWithBuildNetwork(v)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("buildNetwork %s: Load returned nil error", v)
		}
	}
}
