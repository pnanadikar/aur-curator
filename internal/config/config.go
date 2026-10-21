package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pramod/aur-curator/internal/domain"
)

// Load reads and validates the workspace config file at path, applying
// GiteaToken's env-var precedence and the mandatory/optional rules
// below.
//
// Load returns a non-nil error if:
//   - the file cannot be read (does not exist, permission denied, etc.)
//   - the file is not valid JSONC
//   - RootPath is absent or empty
//   - RepositoryPath is absent or empty
//   - GiteaBaseURL is absent or empty
//   - GiteaOrg is absent or empty
//   - GiteaToken cannot be resolved from EITHER the
//     AUR_CURATOR_GITEA_TOKEN environment variable OR the file's
//     giteaToken key (either source alone is sufficient)
//
// Load does NOT error, and applies documented defaults, if:
//   - BuilderBackend is absent (defaults to "makepkg")
//   - RepoDBName is absent (defaults to "custom.db.tar.gz")
//   - ActivityLogPath is absent (defaults to <RootPath>/.aur-curator/activity.log)
//   - AutoMerge or AutoAnalyzeAfterImport is absent (false)
//   - AnalyzerConfig or UIPreferences is absent (left nil)
//
// GiteaToken resolution order:
//  1. AUR_CURATOR_GITEA_TOKEN environment variable, if set — overrides
//     the file's giteaToken key outright.
//  2. The file's giteaToken key, if the env var is unset.
//  3. Error, if neither is set.
//
// If the token is sourced from the file (not the env var), Load stats
// the file and warns (log to stderr, not a refusal to start) if it is
// group- or world-readable.
//
// Load never re-persists a resolved value anywhere — env/file are
// re-resolved from scratch on every call.
//
// The file is decoded directly into domain.WorkspaceConfig using its
// json tags; there is no intermediate struct to keep in sync (#392).
// Unknown keys are ignored.
func Load(path string) (domain.WorkspaceConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: cannot read file (file: %s): %w", path, err)
	}

	jsonc := stripJSONCComments(raw)

	var cfg domain.WorkspaceConfig
	if err := json.Unmarshal(jsonc, &cfg); err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: invalid JSONC (file: %s): %w", path, err)
	}

	// Validate mandatory fields.
	if cfg.RootPath == "" {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: rootPath is required but missing or empty (file: %s)", path)
	}
	if cfg.RepositoryPath == "" {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: repositoryPath is required but missing or empty (file: %s)", path)
	}
	if cfg.GiteaBaseURL == "" {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaBaseURL is required but missing or empty (file: %s)", path)
	}
	if cfg.GiteaOrg == "" {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaOrg is required but missing or empty (file: %s)", path)
	}

	// Resolve GiteaToken: env var wins over file key.
	if envToken := os.Getenv("AUR_CURATOR_GITEA_TOKEN"); envToken != "" {
		cfg.GiteaToken = domain.Secret(envToken)
	} else if cfg.GiteaToken != "" {
		// Warn if file is group/world-readable.
		if warnFilePermissions(path) != nil {
			fmt.Fprintf(os.Stderr, "config: warning: giteaToken file %s may be readable by other users\n", path)
		}
	} else {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaToken is required — set AUR_CURATOR_GITEA_TOKEN or the giteaToken key in %s", path)
	}

	// Defaults for optional fields.
	if cfg.BuilderBackend == "" {
		cfg.BuilderBackend = "makepkg"
	}
	if cfg.RepoDBName == "" {
		cfg.RepoDBName = "custom.db.tar.gz"
	}
	if cfg.ActivityLogPath == "" {
		cfg.ActivityLogPath = filepath.Join(cfg.RootPath, ".aur-curator", "activity.log")
	}

	return cfg, nil
}

// stripJSONCComments removes // line comments and /* block comments from b,
// returning a valid JSON byte slice. It tracks whether the parser is inside
// a string literal so it doesn't strip # or / characters that are part of
// string values.
func stripJSONCComments(b []byte) []byte {
	var result []byte
	i := 0
	for i < len(b) {
		// Handle string literals — copy them verbatim.
		if b[i] == '"' {
			j := i + 1
			for j < len(b) {
				if b[j] == '\\' {
					j += 2
					continue
				}
				if b[j] == '"' {
					j++
					break
				}
				j++
			}
			result = append(result, b[i:j]...)
			i = j
			continue
		}

		// Check for // line comment.
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			// Skip until end of line.
			j := i + 2
			for j < len(b) && b[j] != '\n' {
				j++
			}
			i = j
			continue
		}

		// Check for /* block comment.
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			j := i + 2
			for j+1 < len(b) && (b[j] != '*' || b[j+1] != '/') {
				j++
			}
			if j+1 < len(b) {
				j += 2
			}
			// Preserve newlines inside block comments to avoid collapsing
			// line structure (helps JSON error messages point to the right line).
			for k := i; k < j; k++ {
				if b[k] == '\n' {
					result = append(result, '\n')
				}
			}
			i = j
			continue
		}

		result = append(result, b[i])
		i++
	}
	return result
}

// warnFilePermissions returns a non-nil error if the file at path is
// group- or world-readable.
func warnFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode()
	// Check group-readable (bit 3) or world-readable (bit 0) in the
	// permission bits (ignore setuid/setgid/sticky for this check).
	if mode&0o044 != 0 {
		return fmt.Errorf("file is group- or world-readable")
	}
	return nil
}
