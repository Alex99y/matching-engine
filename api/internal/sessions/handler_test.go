package sessions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/sessions"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeValidator struct {
	info *middleware.SessionInfo
	err  error
}

func (f fakeValidator) ValidateToken(ctx context.Context, rawToken string) (*middleware.SessionInfo, error) {
	return f.info, f.err
}

type fakeCredValidator struct {
	userID uuid.UUID
	err    error
}

func (f fakeCredValidator) ValidateCredentials(ctx context.Context, username, password string) (uuid.UUID, error) {
	return f.userID, f.err
}

func newTestApp(repo sessions.SessionRepository, cred sessions.CredentialValidator, sessionInfo *middleware.SessionInfo) *fiber.App {
	log := logger.NewLogger(logger.Error)
	svc := sessions.NewSessionService(log, repo)
	h := sessions.NewSessionHandler(log, svc, cred)
	auth := fiber.Handler(middleware.Auth(log, fakeValidator{info: sessionInfo}))

	app := fiber.New()
	// Mirrors router.go's actual middleware attachment per route.
	app.Post("/sessions", h.Login)
	app.Delete("/sessions", auth, h.Logout)
	app.Post("/sessions/refresh", auth, h.RefreshSession)
	app.Get("/sessions/active", auth, h.GetSessions)
	app.Delete("/sessions/active", auth, fiber.Handler(middleware.RequireLoginOrigin), h.DeleteActiveSession)
	app.Post("/sessions/tokens", auth, fiber.Handler(middleware.RequireLoginOrigin), h.CreateToken)
	return app
}

func jsonRequest(method, url string, body any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set(fiber.HeaderContentType, "application/json")
	req.Header.Set(fiber.HeaderAuthorization, "Bearer test-token")
	return req
}

func loginSessionInfo(userID uuid.UUID) *middleware.SessionInfo {
	return &middleware.SessionInfo{UserID: userID, Origin: sessionscope.OriginLogin, Scope: sessionscope.ScopeWrite}
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	app := newTestApp(&fakeSessionRepository{}, fakeCredValidator{err: middleware.ErrInvalidCredentials}, nil)

	resp, err := app.Test(jsonRequest("POST", "/sessions", sessions.LoginRequest{Username: "alice", Password: "wrong-password"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestLoginHandlerSuccess(t *testing.T) {
	userID := uuid.New()
	app := newTestApp(&fakeSessionRepository{}, fakeCredValidator{userID: userID}, nil)

	resp, err := app.Test(jsonRequest("POST", "/sessions", sessions.LoginRequest{Username: "alice", Password: "correct-horse-battery"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got sessions.LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Token == "" {
		t.Fatal("token is empty")
	}
}

func TestRefreshSessionHandlerSessionNotFoundMapsTo401(t *testing.T) {
	repo := &fakeSessionRepository{refreshErr: repository.ErrSessionNotFound}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(uuid.New()))

	resp, err := app.Test(jsonRequest("POST", "/sessions/refresh", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRefreshSessionHandlerRepositoryErrorMapsTo500(t *testing.T) {
	repo := &fakeSessionRepository{refreshErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(uuid.New()))

	resp, err := app.Test(jsonRequest("POST", "/sessions/refresh", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestGetSessionsHandlerReturnsActiveSessions(t *testing.T) {
	repo := &fakeSessionRepository{activeSessions: nil}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(uuid.New()))

	resp, err := app.Test(jsonRequest("GET", "/sessions/active", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDeleteActiveSessionHandlerRejectsMintedOrigin(t *testing.T) {
	mintedInfo := &middleware.SessionInfo{UserID: uuid.New(), Origin: sessionscope.OriginMinted, Scope: sessionscope.ScopeWrite}
	app := newTestApp(&fakeSessionRepository{}, fakeCredValidator{}, mintedInfo)

	resp, err := app.Test(jsonRequest("DELETE", "/sessions/active", sessions.RevokeTokenHashRequest{TokenHash: "hash"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a minted token must never revoke another session)", resp.StatusCode)
	}
}

func TestDeleteActiveSessionHandlerNotFound(t *testing.T) {
	repo := &fakeSessionRepository{revokeErr: repository.ErrSessionNotFound}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(uuid.New()))

	resp, err := app.Test(jsonRequest("DELETE", "/sessions/active", sessions.RevokeTokenHashRequest{TokenHash: "unknown-hash"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeleteActiveSessionHandlerSuccess(t *testing.T) {
	userID := uuid.New()
	repo := &fakeSessionRepository{}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(userID))

	resp, err := app.Test(jsonRequest("DELETE", "/sessions/active", sessions.RevokeTokenHashRequest{TokenHash: "hash"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.revokeCalls) != 1 || repo.revokeCalls[0] != (revokeCall{userID, "hash"}) {
		t.Fatalf("revokeCalls = %+v, want one call with (%v, hash)", repo.revokeCalls, userID)
	}
}

func TestCreateTokenHandlerRejectsMintedOrigin(t *testing.T) {
	mintedInfo := &middleware.SessionInfo{UserID: uuid.New(), Origin: sessionscope.OriginMinted, Scope: sessionscope.ScopeWrite}
	app := newTestApp(&fakeSessionRepository{}, fakeCredValidator{}, mintedInfo)

	resp, err := app.Test(jsonRequest("POST", "/sessions/tokens", sessions.CreateTokenRequest{Scope: "read"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a minted token must never mint another)", resp.StatusCode)
	}
}

func TestCreateTokenHandlerSuccess(t *testing.T) {
	app := newTestApp(&fakeSessionRepository{}, fakeCredValidator{}, loginSessionInfo(uuid.New()))

	resp, err := app.Test(jsonRequest("POST", "/sessions/tokens", sessions.CreateTokenRequest{Scope: "read"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got sessions.CreateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Token == "" || got.Scope != "read" {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
}

func TestLogoutHandlerSuccess(t *testing.T) {
	userID := uuid.New()
	repo := &fakeSessionRepository{}
	app := newTestApp(repo, fakeCredValidator{}, loginSessionInfo(userID))

	resp, err := app.Test(jsonRequest("DELETE", "/sessions", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if len(repo.revokeCalls) != 1 {
		t.Fatalf("revokeCalls = %d, want 1", len(repo.revokeCalls))
	}
}
