package client

import (
	"context"
	"errors"
	"net/http"
)

var ErrUsernameTaken = errors.New("username already taken")

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// Register creates a new user. Returns ErrUsernameTaken (not an error the caller must abort on)
// if the username already exists — callers that re-run the same test accounts across load-test
// invocations should treat that as "already provisioned" and proceed to Login.
func (c *Client) Register(ctx context.Context, username, email, password string) error {
	err := c.do(ctx, http.MethodPost, "/users/register", "", registerRequest{
		Username: username,
		Email:    email,
		Password: password,
	}, nil, http.StatusCreated)
	if err == nil {
		return nil
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusConflict {
		return ErrUsernameTaken
	}
	return err
}

// Login authenticates and returns a write-scoped bearer token, usable directly for faucet and
// order endpoints (no separate token-minting step needed).
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	var resp loginResponse
	err := c.do(ctx, http.MethodPost, "/sessions", "", loginRequest{
		Username: username,
		Password: password,
	}, &resp, http.StatusOK)
	if err != nil {
		return "", err
	}
	return resp.Token, nil
}
