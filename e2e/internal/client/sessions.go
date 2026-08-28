package client

import (
	"context"
	"net/http"
)

type MintedToken struct {
	Token     string `json:"token"`
	Scope     string `json:"scope"`
	ExpiresAt int64  `json:"expires_at"`
}

type ActiveSession struct {
	CreatedAt int64   `json:"created_at"`
	ExpiresAt int64   `json:"expires_at"`
	SessionID string  `json:"session_id"` // one-way token hash — the revoke key, never an auth credential
	Origin    string  `json:"origin"`
	Scope     string  `json:"scope"`
	UserAgent *string `json:"user_agent,omitempty"`
	IPAddress *string `json:"ip_address,omitempty"`
}

type createTokenRequest struct {
	Scope string `json:"scope"` // "read" or "write"
}

type revokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type refreshResponse struct {
	ExpiresAt int64 `json:"expires_at"`
}

// MintToken issues a scoped token from a login session. loginToken must come from Login — a
// minted token cannot mint another (API returns 403).
func (c *Client) MintToken(ctx context.Context, loginToken, scope string) (*MintedToken, error) {
	var resp MintedToken
	if err := c.do(ctx, http.MethodPost, "/sessions/tokens", loginToken,
		createTokenRequest{Scope: scope}, &resp, http.StatusCreated); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RefreshSession extends the session behind token and returns its new expiry.
func (c *Client) RefreshSession(ctx context.Context, token string) (int64, error) {
	var resp refreshResponse
	if err := c.do(ctx, http.MethodPost, "/sessions/refresh", token, nil, &resp, http.StatusOK); err != nil {
		return 0, err
	}
	return resp.ExpiresAt, nil
}

func (c *Client) ListSessions(ctx context.Context, token string) ([]ActiveSession, error) {
	var out []ActiveSession
	if err := c.do(ctx, http.MethodGet, "/sessions/active", token, nil, &out, http.StatusOK); err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeSession revokes the session identified by sessionID (an ActiveSession.SessionID).
// token must be a login session (403 otherwise). A missing session returns nil, not an error.
func (c *Client) RevokeSession(ctx context.Context, token, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/active", token,
		revokeSessionRequest{SessionID: sessionID}, nil, http.StatusOK, http.StatusNotFound)
}

// Logout revokes the session behind token.
func (c *Client) Logout(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodDelete, "/sessions", token, nil, nil, http.StatusNoContent)
}
