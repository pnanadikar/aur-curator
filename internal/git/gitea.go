package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pramod/aur-curator/internal/sanitize"
)

type giteaForge struct {
	baseURL     string
	org         string
	token       string
	tokenSource string
	client      *http.Client
}

// NewGiteaForge returns a Forge for the Gitea API at baseURL, creating and
// deleting repositories in org. tokenSource says where token came from
// (config.GiteaTokenSource); it is named in 401/403 errors so the user
// knows which token to fix (#400). The token itself is never shown.
func NewGiteaForge(baseURL, org, token, tokenSource string) Forge {
	return &giteaForge{
		baseURL:     strings.TrimRight(baseURL, "/"),
		org:         org,
		token:       token,
		tokenSource: tokenSource,
		client:      http.DefaultClient,
	}
}

// statusError describes an unexpected API status for op (ErrCreateRepository
// or ErrDeleteRepository). 401 and 403 say why and where the token came
// from; scope and action name what the call needs ("write:organization",
// "create repositories"). Gitea's own message, when the body has one, is
// added to every status (#400).
func (g *giteaForge) statusError(op error, resp *http.Response, scope, action string) error {
	var err error
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		err = fmt.Errorf("%w: Gitea rejected the API token (401 Unauthorized): invalid, expired or revoked", op)
	case http.StatusForbidden:
		err = fmt.Errorf("%w: the API token is not allowed to %s in %q (403 Forbidden): it needs the %s scope and that permission in the organization",
			op, action, g.org, scope)
	case http.StatusConflict:
		err = fmt.Errorf("%w: unexpected status %d (repository with the same name already exists)", op, resp.StatusCode)
	default:
		err = fmt.Errorf("%w: unexpected status %d", op, resp.StatusCode)
	}
	if msg := giteaMessage(resp.Body); msg != "" {
		err = fmt.Errorf("%w; Gitea: %q", err, msg)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		err = fmt.Errorf("%w; token from %s", err, g.tokenSource)
	}
	return err
}

// giteaMessage returns the "message" field of a Gitea API error body,
// sanitized for display, or "" if there is none.
func giteaMessage(body io.Reader) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 4096)).Decode(&parsed); err != nil {
		return ""
	}
	return sanitize.ForMessage(strings.TrimSpace(parsed.Message))
}

func (g *giteaForge) CreateRepository(ctx context.Context, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/orgs/%s/repos", g.baseURL, g.org),
		bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCreateRepository, err)
	}
	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCreateRepository, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return "", g.statusError(ErrCreateRepository, resp, "write:organization", "create repositories")
	}
	var result struct {
		CloneURL string `json:"clone_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", ErrCreateRepository, err)
	}
	if result.CloneURL == "" {
		return "", fmt.Errorf("%w: missing clone_url in response", ErrCreateRepository)
	}
	return result.CloneURL, nil
}

func (g *giteaForge) DeleteRepository(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/repos/%s/%s", g.baseURL, g.org, name),
		nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDeleteRepository, err)
	}
	req.Header.Set("Authorization", "token "+g.token)
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDeleteRepository, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusNoContent {
		return g.statusError(ErrDeleteRepository, resp, "write:repository", "delete repositories")
	}
	return nil
}
