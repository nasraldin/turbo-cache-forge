package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin, provisional wrapper over the Phase 3 /api/v1 Management
// API. It never touches storage or the database — HTTP only.
type Client struct {
	BaseURL string
	Token   string // OIDC JWT from `turbo-cache login`
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// APIError is returned for any non-2xx /api/v1 response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api: %d: %s", e.StatusCode, e.Message)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- tokens ---

type createTokenResponse struct {
	Token string `json:"token"` // plaintext, shown once — the server never returns it again
}

// CreateToken calls POST /api/v1/tokens and returns the plaintext token.
func (c *Client) CreateToken(ctx context.Context, name string) (string, error) {
	var out createTokenResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/tokens", map[string]string{"name": name}, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// --- projects ---

// Project mirrors the real POST /api/v1/projects 201 body (snake_case).
type Project struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// CreateProject calls POST /api/v1/projects. The request body is exactly
// {"slug","name"} — do NOT send id/created_at (server-assigned).
func (c *Client) CreateProject(ctx context.Context, slug, name string) (Project, error) {
	var out Project
	body := map[string]string{"slug": slug, "name": name}
	if err := c.do(ctx, http.MethodPost, "/api/v1/projects", body, &out); err != nil {
		return Project{}, err
	}
	return out, nil
}

// --- stats ---

// Stats mirrors the real GET /api/v1/stats body. There is NO hit_rate field —
// callers compute it as Hits/(Hits+Misses). Requests is all-time (hits+misses),
// not a 24h window.
type Stats struct {
	StorageBytes  int64 `json:"storage_bytes"`
	ArtifactCount int64 `json:"artifact_count"`
	Hits          int64 `json:"hits"`
	Misses        int64 `json:"misses"`
	Requests      int64 `json:"requests"`
	BytesUp       int64 `json:"bytes_up"`
	BytesDown     int64 `json:"bytes_down"`
}

// Stats calls GET /api/v1/stats.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	if err := c.do(ctx, http.MethodGet, "/api/v1/stats", nil, &out); err != nil {
		return Stats{}, err
	}
	return out, nil
}
