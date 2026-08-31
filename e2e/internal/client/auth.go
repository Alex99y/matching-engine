package client

import (
	"context"
	"errors"
	"net/http"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

type checkUsernameRequest struct {
	Username string `json:"username"`
}

type checkUsernameResponse struct {
	Available bool `json:"available"`
}

// Register creates a user. It maps a 409 to ErrUsernameTaken; every other unexpected status
// comes back as *HTTPError.
func (c *Client) Register(ctx context.Context, username, email, password string) error {
	err := c.do(ctx, http.MethodPost, "/users/register", "",
		registerRequest{Username: username, Email: email, Password: password},
		nil, http.StatusCreated)
	if Status(err) == http.StatusConflict {
		return ErrUsernameTaken
	}
	return err
}

// CheckUsername reports whether username is still free to register.
func (c *Client) CheckUsername(ctx context.Context, username string) (bool, error) {
	var resp checkUsernameResponse
	err := c.do(ctx, http.MethodPost, "/users/check-username", "",
		checkUsernameRequest{Username: username}, &resp, http.StatusOK)
	return resp.Available, err
}

// Login exchanges credentials for a write-scoped login-session token (usable directly for
// faucet and order routes). Password must be at least 10 characters or the API returns 400.
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	var resp tokenResponse
	if err := c.do(ctx, http.MethodPost, "/sessions", "",
		loginRequest{Username: username, Password: password}, &resp, http.StatusOK); err != nil {
		return "", err
	}
	if resp.Token == "" {
		return "", errors.New("login: empty token in response")
	}
	return resp.Token, nil
}
