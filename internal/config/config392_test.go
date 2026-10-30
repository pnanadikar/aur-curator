package config

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func makeMinConfig(extra map[string]string) string {
	base := map[string]string{
		"rootPath":       "/tmp/aur-workspace",
		"repositoryPath": "/tmp/repo",
		"giteaBaseURL":   "https://git.example.com",
		"giteaOrg":       "myorg",
		"giteaToken":     "dummy",
	}
	for k, v := range extra {
		base[k] = v
	}
	return objToJSON(base)
}

func objToJSON(m map[string]string) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func loadTestConfig(t *testing.T, dir, filename, content string) domain.WorkspaceConfig {
	t.Helper()
	path := dir + "/" + filename
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	return cfg
}

func TestLoad392_AutoMergeTrue(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	dir := t.TempDir()
	base := makeMinConfig(nil)
	base = strings.TrimSuffix(base, "}") + `, "autoMerge": true}`
	cfg := loadTestConfig(t, dir, "config.json", base)
	if !cfg.AutoMerge {
		t.Errorf("AutoMerge = false, want true")
	}
}

func TestLoad392_AutoMergeAbsent(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	dir := t.TempDir()
	cfg := loadTestConfig(t, dir, "config.json", makeMinConfig(nil))
	if cfg.AutoMerge {
		t.Errorf("AutoMerge = true, want false")
	}
}

func TestLoad392_EveryFieldLoads(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	dir := t.TempDir()

	// Build map from JSON key to raw JSON value for every field.
	fixture := map[string]string{
		"rootPath":               `"/tmp/aur-workspace"`,
		"repositoryPath":         `"/tmp/repo"`,
		"builderBackend":         `"makepkg"`, // the only accepted value (#408), so the default
		"repoDBName":             `"other.db.tar.gz"`,
		"autoMerge":              `true`,
		"autoAnalyzeAfterImport": `true`,
		"giteaBaseURL":           `"https://git.example.com"`,
		"giteaOrg":               `"myorg"`,
		"giteaToken":             `"s3cret-value"`,
		"activityLogPath":        `"/tmp/activity.log"`,
	}

	// Guard: walk WorkspaceConfig fields, verify each has a json tag
	// that appears in the fixture map.
	var tRef = reflect.TypeOf(domain.WorkspaceConfig{})
	for i := 0; i < tRef.NumField(); i++ {
		f := tRef.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("WorkspaceConfig.%s has no json tag or no entry in this test's fixture — add one", f.Name)
		}
		key := strings.Split(tag, ",")[0]
		if _, ok := fixture[key]; !ok {
			t.Fatalf("WorkspaceConfig.%s json tag %q has no entry in fixture — add one", f.Name, key)
		}
	}

	// Build the file from the fixture map.
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for k, v := range fixture {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&sb, `"%s":%s`, k, v)
	}
	sb.WriteByte('}')

	cfg := loadTestConfig(t, dir, "config.json", sb.String())

	// Assert every field equals its fixture value. Exact comparison, not
	// just non-zero: a defaulted field (BuilderBackend, RepoDBName,
	// ActivityLogPath) would be non-zero even if the loader dropped it.
	v := reflect.ValueOf(cfg)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		key := strings.Split(f.Tag.Get("json"), ",")[0]
		want := reflect.New(f.Type)
		if err := json.Unmarshal([]byte(fixture[key]), want.Interface()); err != nil {
			t.Fatalf("fixture value for %s does not decode into %s: %v", key, f.Type, err)
		}
		if want.Elem().IsZero() {
			t.Fatalf("fixture value for %s is the zero value — use a non-zero, non-default value", key)
		}
		if got := v.Field(i).Interface(); !reflect.DeepEqual(got, want.Elem().Interface()) {
			t.Errorf("WorkspaceConfig.%s = %#v, want %#v (fixture key %s)", f.Name, got, want.Elem().Interface(), key)
		}
	}
}

func TestLoad392_EnvTokenWins(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "from-env")
	dir := t.TempDir()
	cfg := loadTestConfig(t, dir, "config.json", makeMinConfig(map[string]string{
		"giteaToken": "from-file",
	}))
	if cfg.GiteaToken.Value() != "from-env" {
		t.Errorf("GiteaToken = %q, want %q", cfg.GiteaToken.Value(), "from-env")
	}
}

func TestLoad392_NoTokenErrors(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	dir := t.TempDir()
	// Minimal file without giteaToken.
	fileContent := `{"rootPath":"/tmp/aur-workspace","repositoryPath":"/tmp/repo","giteaBaseURL":"https://git.example.com","giteaOrg":"myorg"}`
	if err := os.WriteFile(dir+"/config.json", []byte(fileContent), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir + "/config.json")
	if err == nil {
		t.Fatal("Load returned nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "giteaToken") {
		t.Errorf("error %q does not contain giteaToken", err.Error())
	}
}

func TestLoad392_MarshalRedactsToken(t *testing.T) {
	t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")
	dir := t.TempDir()
	cfg := loadTestConfig(t, dir, "config.json", makeMinConfig(map[string]string{
		"giteaToken": "s3cret-value",
	}))
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(out)
	if strings.Contains(outStr, "s3cret-value") {
		t.Error("json.Marshal output contains the raw secret")
	}
	if !strings.Contains(outStr, `"giteaToken":"REDACTED"`) {
		t.Errorf("json.Marshal output does not contain \"REDACTED\": %s", outStr)
	}
}
