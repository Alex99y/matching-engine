// Package client is a black-box HTTP client for the matching-engine API. It speaks only the
// public wire contract (JSON over REST) — e2e is a separate module and, by Go's internal/
// rule, cannot import api/internal/* even from the same repo. That is deliberate: the suite
// exercises exactly what an external client sees.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is safe for concurrent use. One instance per suite is enough.
type Client struct {
	baseURL string // .../api/v1
	rootURL string // baseURL with the /api/v1 suffix removed, for /health
	http    *http.Client
}

func New(baseURL string) *Client {
	base := strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: base,
		rootURL: strings.TrimSuffix(base, "/api/v1"),
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        256,
				MaxIdleConnsPerHost: 256,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Health hits GET /health (outside /api/v1). Used by the harness to wait for the stack.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rootURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Method: http.MethodGet, Path: "/health", Status: resp.StatusCode}
	}
	return nil
}

// OpenAPISpec fetches the served API description. Like /health it sits outside /api/v1.
func (c *Client) OpenAPISpec(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rootURL+"/openapi.yaml", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{Method: http.MethodGet, Path: "/openapi.yaml", Status: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

// do issues one request against the /api/v1 base and decodes a JSON body into out (when
// non-nil). token may be empty for public endpoints. Any status not in wantStatus is
// returned as *HTTPError with the raw body attached.
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

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if !contains(wantStatus, resp.StatusCode) {
		return &HTTPError{Method: method, Path: path, Status: resp.StatusCode, Body: string(respBody)}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}

// query builds a "?k=v&..." suffix (url-encoded, keys sorted), skipping empty values.
// Returns "" when nothing is set.
func query(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func contains(haystack []int, needle int) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
