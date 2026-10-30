package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/pramod/aur-curator/internal/domain"
)

// giteaTokenEnv is the environment variable that overrides the config
// file's giteaToken key.
const giteaTokenEnv = "AUR_CURATOR_GITEA_TOKEN"

// builderMakepkg is the default and, for now, the only builderBackend
// value Load accepts (#408).
const builderMakepkg = "makepkg"

// repoDBSuffix is the ending Load requires of repoDBName: internal/repodb
// reads gzip-compressed databases only (#408).
const repoDBSuffix = ".db.tar.gz"

// warnOut is where Load writes warnings; tests replace it.
var warnOut io.Writer = os.Stderr

// warnf writes one warning line to warnOut. A failed write is ignored:
// warnOut is stderr, and there is nowhere else to report it.
func warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(warnOut, format+"\n", args...)
}

// envToken returns the Gitea token from the environment, with surrounding
// whitespace removed; "" means not set (#408: a blank value counts as
// unset).
func envToken() string {
	return strings.TrimSpace(os.Getenv(giteaTokenEnv))
}

// GiteaTokenSource says where Load takes the Gitea token from for the
// config file at path, for error messages (#400): the environment
// variable when it is set, otherwise the file's giteaToken key — the same
// precedence Load applies, blank counting as unset. It never returns the
// token itself.
func GiteaTokenSource(path string) string {
	if envToken() != "" {
		return giteaTokenEnv + " (environment)"
	}
	return "giteaToken in " + path
}

// Load reads and validates the workspace config file at path. The rules
// are in domain.md § WorkspaceConfig § Validation (#408); in short:
//
// Load returns a non-nil error if:
//   - the file cannot be read (does not exist, permission denied, etc.)
//   - the file is not valid JSONC, including a /* comment that is never
//     closed
//   - a key matches a known key only by case (encoding/json would apply
//     it)
//   - rootPath, repositoryPath, giteaBaseURL or giteaOrg is absent or
//     blank
//   - rootPath, repositoryPath or a set activityLogPath is not absolute
//     after a leading ~ is expanded
//   - repositoryPath and rootPath are the same, or one is inside the other
//   - giteaBaseURL is not an http(s) URL with a host, or giteaOrg has a /
//   - GiteaToken cannot be resolved from EITHER the
//     AUR_CURATOR_GITEA_TOKEN environment variable OR the file's
//     giteaToken key (either source alone is sufficient; blank is unset)
//   - repoDBName is not a plain file name ending in .db.tar.gz
//   - builderBackend is set to anything but "makepkg"
//
// Load warns on stderr, and carries on, for any other unknown key, and
// when the token comes from the file and the file is group- or
// world-readable or its permissions cannot be read.
//
// Every string value is trimmed; paths are cleaned. Defaults:
//   - BuilderBackend: "makepkg"
//   - RepoDBName: "custom.db.tar.gz"
//   - ActivityLogPath: <RootPath>/.aur-curator/activity.log
//   - AutoMerge, AutoAnalyzeAfterImport: false
//
// GiteaToken resolution order:
//  1. AUR_CURATOR_GITEA_TOKEN environment variable, if set — overrides
//     the file's giteaToken key outright.
//  2. The file's giteaToken key, if the env var is unset.
//  3. Error, if neither is set.
//
// Load never re-persists a resolved value anywhere — env/file are
// re-resolved from scratch on every call.
//
// The file is decoded directly into domain.WorkspaceConfig using its
// json tags; there is no intermediate struct to keep in sync (#392). The
// known keys are those tags too.
func Load(path string) (domain.WorkspaceConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: cannot read file (file: %s): %w", path, err)
	}

	jsonc, err := stripJSONCComments(raw)
	if err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: invalid JSONC (file: %s): %w", path, err)
	}

	var cfg domain.WorkspaceConfig
	if err := json.Unmarshal(jsonc, &cfg); err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: invalid JSONC (file: %s): %w", path, err)
	}

	// Keys first: a misspelt key is often why a required one is missing,
	// so its warning is printed before any error below.
	unknown, err := checkKeys(jsonc)
	if err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: %w (file: %s)", err, path)
	}
	for _, k := range unknown {
		warnf("config: warning: unknown key %q ignored (file: %s)", k, path)
	}

	for _, v := range []*string{
		&cfg.RootPath, &cfg.RepositoryPath, &cfg.BuilderBackend, &cfg.RepoDBName,
		&cfg.GiteaBaseURL, &cfg.GiteaOrg, &cfg.ActivityLogPath,
	} {
		*v = strings.TrimSpace(*v)
	}
	cfg.GiteaToken = domain.Secret(strings.TrimSpace(cfg.GiteaToken.Value()))

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

	// Paths: expand ~, require absolute, clean.
	for _, p := range []struct {
		key string
		v   *string
	}{
		{"rootPath", &cfg.RootPath},
		{"repositoryPath", &cfg.RepositoryPath},
		{"activityLogPath", &cfg.ActivityLogPath},
	} {
		if *p.v == "" {
			continue // only activityLogPath can be empty here; defaulted below
		}
		abs, err := absPath(*p.v)
		if err != nil {
			return domain.WorkspaceConfig{}, fmt.Errorf("config: %s: %w (file: %s)", p.key, err, path)
		}
		*p.v = abs
	}
	if err := checkNesting(cfg.RootPath, cfg.RepositoryPath); err != nil {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: %w (file: %s)", err, path)
	}

	if u, err := url.Parse(cfg.GiteaBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaBaseURL must be an http:// or https:// URL with a host, got %q (file: %s)", cfg.GiteaBaseURL, path)
	}
	if strings.Contains(cfg.GiteaOrg, "/") {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaOrg must be an organization name without /, got %q (file: %s)", cfg.GiteaOrg, path)
	}

	// Resolve GiteaToken: env var wins over file key.
	if tok := envToken(); tok != "" {
		cfg.GiteaToken = domain.Secret(tok)
	} else if cfg.GiteaToken != "" {
		if w := tokenFileWarning(path); w != "" {
			warnf("%s", w)
		}
	} else {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: giteaToken is required — set %s or the giteaToken key in %s", giteaTokenEnv, path)
	}

	// Defaults for optional fields, then their checks.
	if cfg.BuilderBackend == "" {
		cfg.BuilderBackend = builderMakepkg
	}
	if cfg.BuilderBackend != builderMakepkg {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: builderBackend %q is not supported; the only backend is %q (file: %s)", cfg.BuilderBackend, builderMakepkg, path)
	}
	if cfg.RepoDBName == "" {
		cfg.RepoDBName = "custom" + repoDBSuffix
	}
	if strings.Contains(cfg.RepoDBName, "/") || !strings.HasSuffix(cfg.RepoDBName, repoDBSuffix) || cfg.RepoDBName == repoDBSuffix {
		return domain.WorkspaceConfig{}, fmt.Errorf("config: repoDBName must be a file name ending in %s, e.g. custom%s, got %q (file: %s)", repoDBSuffix, repoDBSuffix, cfg.RepoDBName, path)
	}
	if cfg.ActivityLogPath == "" {
		cfg.ActivityLogPath = filepath.Join(cfg.RootPath, ".aur-curator", "activity.log")
	}

	return cfg, nil
}

// checkKeys compares the top-level keys of the JSON object in b with the
// json tags of domain.WorkspaceConfig. A key that matches a tag only by
// case is an error, since encoding/json applies it case-insensitively;
// any other unknown key is returned, sorted, for a warning (#408).
func checkKeys(b []byte) ([]string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("invalid JSONC: %w", err)
	}
	known := knownKeys()
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unknown []string
	for _, k := range keys {
		if known[k] {
			continue
		}
		for kk := range known {
			if strings.EqualFold(k, kk) {
				return nil, fmt.Errorf("key %q must be spelt %q", k, kk)
			}
		}
		unknown = append(unknown, k)
	}
	return unknown, nil
}

// knownKeys returns the json tag names of domain.WorkspaceConfig: the keys
// Load decodes. Derived from the struct, so it cannot drift from it.
func knownKeys() map[string]bool {
	t := reflect.TypeOf(domain.WorkspaceConfig{})
	keys := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

// absPath expands a leading "~" or "~/" in p to the home directory and
// returns the cleaned result, or an error if it is not absolute (#408).
// "~user" is not expanded.
func absPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~ in %q: %w", p, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("must be an absolute path or start with ~/, got %q", p)
	}
	return filepath.Clean(p), nil
}

// checkNesting returns an error if repositoryPath and rootPath, both
// absolute and clean, are the same or one is inside the other: the
// catalog treats every subdirectory of rootPath as a package (#408).
// Symlinks are not resolved.
func checkNesting(rootPath, repositoryPath string) error {
	switch {
	case rootPath == repositoryPath:
		return fmt.Errorf("repositoryPath must not be the same as rootPath (%s)", rootPath)
	case isInside(repositoryPath, rootPath):
		return fmt.Errorf("repositoryPath %s must not be inside rootPath %s", repositoryPath, rootPath)
	case isInside(rootPath, repositoryPath):
		return fmt.Errorf("rootPath %s must not be inside repositoryPath %s", rootPath, repositoryPath)
	}
	return nil
}

// isInside reports whether path is strictly inside dir; both are absolute
// and clean.
func isInside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../")
}

// stripJSONCComments removes // line comments and /* block comments from b,
// returning a valid JSON byte slice. It tracks whether the parser is inside
// a string literal so it doesn't strip # or / characters that are part of
// string values.
//
// A /* with no closing */ is an error (#408): dropping everything after it
// could leave valid JSON, and the keys after it would silently fall back
// to their defaults.
func stripJSONCComments(b []byte) ([]byte, error) {
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
			// A backslash as the last byte steps j past the end (#408).
			if j > len(b) {
				j = len(b)
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
			if j+1 >= len(b) {
				line := 1 + bytes.Count(b[:i], []byte{'\n'})
				return nil, fmt.Errorf("/* comment starting on line %d is never closed", line)
			}
			j += 2
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
	return result, nil
}

// tokenFileWarning returns the warning Load prints when the Gitea token
// comes from the config file at path, or "" when there is none. It warns
// when the file is group- or world-readable, and, separately, when its
// permissions cannot be read (#408: that case used to be reported as
// "may be readable by other users").
func tokenFileWarning(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("config: warning: cannot check whether giteaToken file %s is readable by other users: %v", path, err)
	}
	// Group-readable (0o040) or world-readable (0o004).
	if info.Mode().Perm()&0o044 != 0 {
		return fmt.Sprintf("config: warning: giteaToken file %s may be readable by other users", path)
	}
	return ""
}
