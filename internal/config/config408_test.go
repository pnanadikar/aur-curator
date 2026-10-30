package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripJSONCComments408_UnterminatedBlock: a /* that is never closed
// is an error naming its line, not a silent drop of the rest of the file.
func TestStripJSONCComments408_UnterminatedBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		line  string
	}{
		{"at end", `{"a": "x"} /* tail`, "line 1"},
		{"swallows closing brace", "{\n\"a\": \"x\"\n/* \"b\": true }", "line 3"},
		{"only slash star", `/*`, "line 1"},
		{"star but no slash", `{} /* x *`, "line 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripJSONCComments([]byte(tt.input))
			if err == nil {
				t.Fatalf("stripJSONCComments(%q) = %q, want error", tt.input, got)
			}
			if !strings.Contains(err.Error(), "never closed") || !strings.Contains(err.Error(), tt.line) {
				t.Errorf("error = %q, want it to say never closed on %s", err, tt.line)
			}
		})
	}
}

// TestStripJSONCComments408_BackslashAtEnd: a string ending in a
// backslash as the very last byte is copied as is, without reading past
// the end. The slice has no spare capacity, so an overrun panics.
func TestStripJSONCComments408_BackslashAtEnd(t *testing.T) {
	in := []byte(`{"a": "x\`)
	in = in[:len(in):len(in)]
	got, err := stripJSONCComments(in)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if string(got) != string(in) {
		t.Errorf("got %q, want %q", got, in)
	}
}

// TestLoad408_UnterminatedBlockComment is the issue's case: without the
// error, the kept "}" made this valid JSON and autoMerge was dropped.
func TestLoad408_UnterminatedBlockComment(t *testing.T) {
	t.Setenv(giteaTokenEnv, "tok")
	p := filepath.Join(t.TempDir(), "c.jsonc")
	writeConfig(t, p, `{"rootPath": "/r", "repositoryPath": "/p", "giteaBaseURL": "https://g", "giteaOrg": "o" /* "autoMerge": true }`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load: want error for an unterminated /* comment")
	}
	if !strings.Contains(err.Error(), "never closed") {
		t.Errorf("error = %q, want it to say the comment is never closed", err)
	}
}

// TestTokenFileWarning408 covers the three outcomes; a stat failure has
// its own message.
func TestTokenFileWarning408(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "private.jsonc")
	shared := filepath.Join(dir, "shared.jsonc")
	writeConfig(t, private, "{}")
	writeConfig(t, shared, "{}")
	if err := os.Chmod(shared, 0o644); err != nil {
		t.Fatal(err)
	}

	if w := tokenFileWarning(private); w != "" {
		t.Errorf("0600 file: warning = %q, want none", w)
	}
	if w := tokenFileWarning(shared); !strings.Contains(w, "may be readable by other users") {
		t.Errorf("0644 file: warning = %q, want readable-by-others", w)
	}
	w := tokenFileWarning(filepath.Join(dir, "missing.jsonc"))
	if !strings.Contains(w, "cannot check") || strings.Contains(w, "may be readable") {
		t.Errorf("missing file: warning = %q, want cannot-check, not readable-by-others", w)
	}
}

// load408 writes the JSON object made of base408 plus extra (raw JSON
// values; "" removes a key) and loads it, with the token env var unset.
// It returns what Load wrote to warnOut.
func load408(t *testing.T, extra map[string]string) configResult {
	t.Helper()
	t.Setenv(giteaTokenEnv, "")
	var warn bytes.Buffer
	old := warnOut
	warnOut = &warn
	t.Cleanup(func() { warnOut = old })

	fields := map[string]string{
		"rootPath":       `"/srv/aur"`,
		"repositoryPath": `"/srv/repo"`,
		"giteaBaseURL":   `"https://git.example.com"`,
		"giteaOrg":       `"aur"`,
		"giteaToken":     `"tok"`,
	}
	for k, v := range extra {
		if v == "" {
			delete(fields, k)
		} else {
			fields[k] = v
		}
	}
	var sb strings.Builder
	sb.WriteString("{")
	first := true
	for k, v := range fields {
		if !first {
			sb.WriteString(",")
		}
		first = false
		sb.WriteString(`"` + k + `":` + v)
	}
	sb.WriteString("}")
	p := filepath.Join(t.TempDir(), "c.jsonc")
	writeConfig(t, p, sb.String())
	cfg, err := Load(p)
	return configResult{cfg: cfg.RootPath + "|" + cfg.RepositoryPath + "|" + cfg.ActivityLogPath + "|" +
		cfg.GiteaBaseURL + "|" + cfg.GiteaOrg + "|" + cfg.GiteaToken.Value() + "|" + cfg.RepoDBName + "|" + cfg.BuilderBackend,
		err: err, warn: warn.String()}
}

type configResult struct {
	cfg  string
	err  error
	warn string
}

// TestLoad408_Errors: each value Load must refuse, with the words the
// error must contain.
func TestLoad408_Errors(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]string
		want  string
	}{
		{"blank rootPath", map[string]string{"rootPath": `"   "`}, "rootPath is required"},
		{"blank giteaOrg", map[string]string{"giteaOrg": `" "`}, "giteaOrg is required"},
		{"relative rootPath", map[string]string{"rootPath": `"aur"`}, "rootPath: must be an absolute path"},
		{"~user not expanded", map[string]string{"repositoryPath": `"~bob/repo"`}, "repositoryPath: must be an absolute path"},
		{"relative activityLogPath", map[string]string{"activityLogPath": `"log/activity.log"`}, "activityLogPath: must be an absolute path"},
		{"same paths", map[string]string{"repositoryPath": `"/srv/aur/"`}, "must not be the same as rootPath"},
		{"repo inside root", map[string]string{"repositoryPath": `"/srv/aur/repo"`}, "repositoryPath /srv/aur/repo must not be inside rootPath"},
		{"root inside repo", map[string]string{"rootPath": `"/srv/repo/aur"`}, "rootPath /srv/repo/aur must not be inside repositoryPath"},
		{"url without scheme", map[string]string{"giteaBaseURL": `"git.example.com"`}, "giteaBaseURL must be an http:// or https:// URL"},
		{"url other scheme", map[string]string{"giteaBaseURL": `"ssh://git.example.com"`}, "giteaBaseURL must be"},
		{"url without host", map[string]string{"giteaBaseURL": `"https://"`}, "giteaBaseURL must be"},
		{"org with slash", map[string]string{"giteaOrg": `"aur/pkgs"`}, "giteaOrg must be an organization name without /"},
		{"blank file token", map[string]string{"giteaToken": `"  "`}, "giteaToken is required"},
		{"repoDBName no suffix", map[string]string{"repoDBName": `"custom"`}, "repoDBName must be a file name ending in .db.tar.gz"},
		{"repoDBName zstd", map[string]string{"repoDBName": `"custom.db.tar.zst"`}, "repoDBName must be"},
		{"repoDBName with slash", map[string]string{"repoDBName": `"../custom.db.tar.gz"`}, "repoDBName must be"},
		{"repoDBName no name", map[string]string{"repoDBName": `".db.tar.gz"`}, "repoDBName must be"},
		{"builderBackend other", map[string]string{"builderBackend": `"chroot"`}, `builderBackend "chroot" is not supported`},
		{"key differs by case", map[string]string{"AutoMerge": `true`}, `key "AutoMerge" must be spelt "autoMerge"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := load408(t, tt.extra)
			if r.err == nil {
				t.Fatalf("Load succeeded (%s), want error containing %q", r.cfg, tt.want)
			}
			if !strings.Contains(r.err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", r.err, tt.want)
			}
		})
	}
}

// TestLoad408_Accepted: values Load accepts, and what it makes of them.
// The fields are rootPath|repositoryPath|activityLogPath|giteaBaseURL|
// giteaOrg|giteaToken|repoDBName|builderBackend.
func TestLoad408_Accepted(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	tests := []struct {
		name  string
		extra map[string]string
		want  string
	}{
		{"defaults", nil,
			"/srv/aur|/srv/repo|/srv/aur/.aur-curator/activity.log|https://git.example.com|aur|tok|custom.db.tar.gz|makepkg"},
		{"trimmed and cleaned", map[string]string{
			"rootPath": `" /srv//aur/ "`, "giteaBaseURL": `" http://g.lan:3000 "`, "giteaOrg": `" aur "`,
			"giteaToken": `" tok "`, "repoDBName": `" mine.db.tar.gz "`, "builderBackend": `" makepkg "`},
			"/srv/aur|/srv/repo|/srv/aur/.aur-curator/activity.log|http://g.lan:3000|aur|tok|mine.db.tar.gz|makepkg"},
		{"tilde expanded", map[string]string{"rootPath": `"~/aur"`, "activityLogPath": `"~"`},
			home + "/aur|/srv/repo|" + home + "|https://git.example.com|aur|tok|custom.db.tar.gz|makepkg"},
		{"sibling with shared prefix is not inside", map[string]string{"repositoryPath": `"/srv/aur-repo"`},
			"/srv/aur|/srv/aur-repo|/srv/aur/.aur-curator/activity.log|https://git.example.com|aur|tok|custom.db.tar.gz|makepkg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := load408(t, tt.extra)
			if r.err != nil {
				t.Fatalf("Load error = %v", r.err)
			}
			if r.cfg != tt.want {
				t.Errorf("got  %s\nwant %s", r.cfg, tt.want)
			}
		})
	}
}

// TestLoad408_UnknownKeysWarn: unknown keys (including the removed
// analyzerConfig and uiPreferences) are named in sorted order, once each,
// and do not stop the load.
func TestLoad408_UnknownKeysWarn(t *testing.T) {
	r := load408(t, map[string]string{"uiPreferences": `{}`, "analyzerConfig": `{"a":1}`, "automerg": `true`})
	if r.err != nil {
		t.Fatalf("Load error = %v", r.err)
	}
	a := strings.Index(r.warn, `unknown key "analyzerConfig"`)
	b := strings.Index(r.warn, `unknown key "automerg"`)
	c := strings.Index(r.warn, `unknown key "uiPreferences"`)
	if a < 0 || b < 0 || c < 0 || a > b || b > c {
		t.Errorf("warnings = %q, want analyzerConfig, automerg, uiPreferences in that order", r.warn)
	}
	if n := strings.Count(r.warn, "unknown key"); n != 3 {
		t.Errorf("%d unknown-key warnings, want 3: %q", n, r.warn)
	}
}

// TestLoad408_KnownKeysNoWarning: a file using every known key warns
// about nothing.
func TestLoad408_KnownKeysNoWarning(t *testing.T) {
	r := load408(t, map[string]string{
		"repoDBName": `"custom.db.tar.gz"`, "builderBackend": `"makepkg"`, "autoMerge": `false`,
		"autoAnalyzeAfterImport": `false`, "activityLogPath": `"/srv/log/activity.log"`,
	})
	if r.err != nil {
		t.Fatalf("Load error = %v", r.err)
	}
	if strings.Contains(r.warn, "unknown key") {
		t.Errorf("warnings = %q, want no unknown-key warning", r.warn)
	}
}

// TestLoad408_EnvToken: a blank AUR_CURATOR_GITEA_TOKEN counts as unset,
// for Load and GiteaTokenSource alike; a set one is trimmed.
func TestLoad408_EnvToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.jsonc")
	writeConfig(t, p, `{"rootPath":"/srv/aur","repositoryPath":"/srv/repo","giteaBaseURL":"https://g","giteaOrg":"o","giteaToken":"from-file"}`)

	t.Setenv(giteaTokenEnv, "  ")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.GiteaToken.Value() != "from-file" {
		t.Errorf("blank env: token = %q, want from-file", cfg.GiteaToken.Value())
	}
	if got := GiteaTokenSource(p); got != "giteaToken in "+p {
		t.Errorf("blank env: GiteaTokenSource = %q, want the file", got)
	}

	t.Setenv(giteaTokenEnv, " from-env\n")
	cfg, err = Load(p)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.GiteaToken.Value() != "from-env" {
		t.Errorf("env set: token = %q, want from-env", cfg.GiteaToken.Value())
	}
}
