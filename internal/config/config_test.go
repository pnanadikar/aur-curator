package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("valid file all fields present", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"builderBackend": "makepkg",
			"analyzerConfig": {"key": "value"},
			"uiPreferences": {"theme": "dark"},
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "secret-token",
			"autoAnalyzeAfterImport": true
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.RootPath != "/tmp/aur" {
			t.Errorf("RootPath = %q, want %q", cfg.RootPath, "/tmp/aur")
		}
		if cfg.RepositoryPath != "/tmp/repo" {
			t.Errorf("RepositoryPath = %q, want %q", cfg.RepositoryPath, "/tmp/repo")
		}
		if cfg.BuilderBackend != "makepkg" {
			t.Errorf("BuilderBackend = %q, want %q", cfg.BuilderBackend, "makepkg")
		}
		if cfg.AnalyzerConfig == nil {
			t.Error("AnalyzerConfig is nil, want non-nil")
		}
		if cfg.UIPreferences == nil {
			t.Error("UIPreferences is nil, want non-nil")
		}
		if cfg.GiteaBaseURL != "https://gitea.example.com" {
			t.Errorf("GiteaBaseURL = %q, want %q", cfg.GiteaBaseURL, "https://gitea.example.com")
		}
		if cfg.GiteaOrg != "myorg" {
			t.Errorf("GiteaOrg = %q, want %q", cfg.GiteaOrg, "myorg")
		}
		if cfg.GiteaToken.Value() != "secret-token" {
			t.Errorf("GiteaToken = %q, want %q", cfg.GiteaToken.Value(), "secret-token")
		}
		if !cfg.AutoAnalyzeAfterImport {
			t.Error("AutoAnalyzeAfterImport = false, want true")
		}
	})

	t.Run("autoAnalyzeAfterImport true", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "secret",
			"autoAnalyzeAfterImport": true
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.AutoAnalyzeAfterImport {
			t.Errorf("AutoAnalyzeAfterImport = false, want true")
		}
	})

	t.Run("autoAnalyzeAfterImport false when absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "secret"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AutoAnalyzeAfterImport {
			t.Errorf("AutoAnalyzeAfterImport = true, want false (default)")
		}
	})

	t.Run("builderBackend defaults to makepkg when absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "secret"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.BuilderBackend != "makepkg" {
			t.Errorf("BuilderBackend = %q, want %q", cfg.BuilderBackend, "makepkg")
		}
	})

	t.Run("analyzerConfig and uiPreferences absent left nil", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "secret"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AnalyzerConfig != nil {
			t.Errorf("AnalyzerConfig = %q, want nil", string(cfg.AnalyzerConfig))
		}
		if cfg.UIPreferences != nil {
			t.Errorf("UIPreferences = %q, want nil", string(cfg.UIPreferences))
		}
	})
}

func TestLoadValidationErrors(t *testing.T) {
	base := map[string]string{
		"rootPath":       "/tmp/aur",
		"repositoryPath": "/tmp/repo",
		"giteaBaseURL":   "https://gitea.example.com",
		"giteaOrg":       "myorg",
		"giteaToken":     "secret",
	}

	t.Run("rootPath absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := buildConfig(base, map[string]string{"rootPath": ""})
		writeConfig(t, cfgPath, content)

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for missing rootPath")
		}
		if want := "rootPath is required"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})

	t.Run("repositoryPath absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := buildConfig(base, map[string]string{"repositoryPath": ""})
		writeConfig(t, cfgPath, content)

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for missing repositoryPath")
		}
		if want := "repositoryPath is required"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})

	t.Run("giteaBaseURL absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := buildConfig(base, map[string]string{"giteaBaseURL": ""})
		writeConfig(t, cfgPath, content)

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for missing giteaBaseURL")
		}
		if want := "giteaBaseURL is required"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})

	t.Run("giteaOrg absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := buildConfig(base, map[string]string{"giteaOrg": ""})
		writeConfig(t, cfgPath, content)

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for missing giteaOrg")
		}
		if want := "giteaOrg is required"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := Load("/nonexistent/path/config.jsonc")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
		if want := "cannot read file"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})

	t.Run("invalid JSONC", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		if err := os.WriteFile(cfgPath, []byte("{ invalid json"), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for invalid JSONC")
		}
		if want := "invalid JSONC"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})
}

func TestLoadGiteaToken(t *testing.T) {
	t.Run("env var set file key absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("AUR_CURATOR_GITEA_TOKEN", "env-token")

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GiteaToken.Value() != "env-token" {
			t.Errorf("GiteaToken = %q, want %q", cfg.GiteaToken.Value(), "env-token")
		}
	})

	t.Run("file key set env var absent", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "file-token"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GiteaToken.Value() != "file-token" {
			t.Errorf("GiteaToken = %q, want %q", cfg.GiteaToken.Value(), "file-token")
		}
	})

	t.Run("both env var and file key set env var wins", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg",
			"giteaToken": "file-token"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("AUR_CURATOR_GITEA_TOKEN", "env-token")

		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GiteaToken.Value() != "env-token" {
			t.Errorf("GiteaToken = %q, want %q", cfg.GiteaToken.Value(), "env-token")
		}
	})

	t.Run("neither set", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.jsonc")
		content := `{
			"rootPath": "/tmp/aur",
			"repositoryPath": "/tmp/repo",
			"giteaBaseURL": "https://gitea.example.com",
			"giteaOrg": "myorg"
		}`
		if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("AUR_CURATOR_GITEA_TOKEN", "")

		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("expected error for missing giteaToken")
		}
		if want := "giteaToken is required"; !contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err.Error(), want)
		}
	})
}

func TestStripJSONCComments(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no comments",
			input:  `{"key": "value"}`,
			expect: `{"key": "value"}`,
		},
		{
			name:   "line comment stripped trailing whitespace preserved",
			input:  `{"key": "value"} // comment`,
			expect: `{"key": "value"} `,
		},
		{
			name: "inline line comment strips after //",
			input: `{"key": "value" // comment
}`,
			expect: `{"key": "value" 
}`,
		},
		{
			name:   "block comment stripped",
			input:  `{"key": "value" /* comment */}`,
			expect: `{"key": "value" }`,
		},
		{
			name:   "string with slash not treated as comment",
			input:  `{"url": "https://example.com"}`,
			expect: `{"url": "https://example.com"}`,
		},
		{
			name:   "string with escaped quote",
			input:  `{"msg": "hello \"world\""}`,
			expect: `{"msg": "hello \"world\""}`,
		},
		{
			name: "multiple comments",
			input: `// top
{"key": "value"} // end`,
			expect: `
{"key": "value"} `,
		},
		{
			name:   "block comment with actual newline preserved",
			input:  "{\"key\": \"value\" /* multi\nline */}",
			expect: "{\"key\": \"value\" \n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripJSONCComments([]byte(tt.input))
			if string(got) != tt.expect {
				t.Errorf("stripJSONCComments(%q) = %q, want %q", tt.input, string(got), tt.expect)
			}
		})
	}
}

func buildConfig(base map[string]string, overrides map[string]string) string {
	var sb []byte
	sb = append(sb, '{')
	first := true
	for k, v := range base {
		if _, ok := overrides[k]; ok {
			continue
		}
		if !first {
			sb = append(sb, ',')
		}
		first = false
		sb = append(sb, '"')
		sb = append(sb, []byte(k)...)
		sb = append(sb, `": "`...)
		sb = append(sb, []byte(v)...)
		sb = append(sb, '"')
	}
	sb = append(sb, '}')
	return string(sb)
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
