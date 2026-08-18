// Package client is a minimal black-box HTTP client for the matching-engine API. It talks only
// the public wire contract (JSON over REST) — e2e is a separate Go module and, by Go's internal/
// visibility rule, cannot import api/internal/* types even though it lives in the same repo. That
// is by design: an e2e suite should exercise the same contract any external client sees.
package client

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

// HTTPError is returned when the API responds with a status code the caller didn't expect.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.Status, e.Body)
}

// Client wraps a single shared *http.Client tuned for high concurrent request rates (up to
// level 3's 1,500 op/s across the spam pool plus the measured account). The default transport's
// MaxIdleConnsPerHost (2) would otherwise force constant TCP/TLS renegotiation under that load.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1024,
				MaxIdleConnsPerHost: 1024,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// do issues an HTTP request and decodes a JSON response body into out (if non-nil). token is
// optional — pass "" for unauthenticated endpoints. It accepts every status in wantStatus;
// anything else is returned as *HTTPError with the raw response body attached for debugging.
func (c *Client) do(ctx context.Context, method, path, token string, body, out any, wantStatus ...int) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if !statusAllowed(resp.StatusCode, wantStatus) {
		return &HTTPError{Status: resp.StatusCode, Body: string(respBody)}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func statusAllowed(status int, want []int) bool {
	for _, s := range want {
		if status == s {
			return true
		}
	}
	return false
}
