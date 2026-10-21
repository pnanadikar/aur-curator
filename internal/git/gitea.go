package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type giteaForge struct {
	baseURL string
	org     string
	token   string
	client  *http.Client
}

func NewGiteaForge(baseURL, org, token string) Forge {
	return &giteaForge{
		baseURL: strings.TrimRight(baseURL, "/"),
		org:     org,
		token:   token,
		client:  http.DefaultClient,
	}
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
		err := fmt.Errorf("%w: unexpected status %d", ErrCreateRepository, resp.StatusCode)
		if resp.StatusCode == http.StatusConflict {
			err = fmt.Errorf("%w (repository with the same name already exists)", err)
		}
		return "", err
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
		return fmt.Errorf("%w: unexpected status %d", ErrDeleteRepository, resp.StatusCode)
	}
	return nil
}
