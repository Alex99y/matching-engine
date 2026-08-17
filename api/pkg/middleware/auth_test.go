package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/sessionscope"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeValidator struct {
	info *SessionInfo
	err  error
}

func (f *fakeValidator) ValidateToken(ctx context.Context, rawToken string) (*SessionInfo, error) {
	return f.info, f.err
}

// newAuthTestApp wires Auth in front of any extra guards (e.g. RequireWrite), then a
// terminal handler that echoes back whatever the middleware stored in context.
func newAuthTestApp(v TokenValidator, guards ...fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Use(fiber.Handler(Auth(logger.NewLogger(logger.Error), v)))
	for _, g := range guards {
		app.Use(g)
	}
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"user_id": UserIDFromContext(c),
			"origin":  SessionOriginFromContext(c),
			"scope":   SessionScopeFromContext(c),
			"frozen":  UserFrozenFromContext(c),
		})
	})
	return app
}

func decodeErrorMessage(t *testing.T, body io.Reader) string {
	t.Helper()
	var er struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(body).Decode(&er); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return er.Message
}

func TestAuthMissingHeader(t *testing.T) {
	app := newAuthTestApp(&fakeValidator{})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if msg := decodeErrorMessage(t, resp.Body); msg != "missing or invalid authorization header" {
		t.Errorf("message = %q", msg)
	}
}

func TestAuthNonBearerHeader(t *testing.T) {
	app := newAuthTestApp(&fakeValidator{})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Basic dXNlcjpwYXNz")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAuthInvalidSession(t *testing.T) {
	app := newAuthTestApp(&fakeValidator{err: ErrInvalidSession})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if msg := decodeErrorMessage(t, resp.Body); msg != "invalid or expired session" {
		t.Errorf("message = %q", msg)
	}
}

// A validator error that is not ErrInvalidSession is an infrastructure failure — it must
// surface as an opaque 500, per go-layer-architecture's "isolate infrastructure errors" rule.
func TestAuthValidatorErrorIsOpaqueServerError(t *testing.T) {
	app := newAuthTestApp(&fakeValidator{err: errors.New("db unreachable: dsn=secret")})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), "secret") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}

func TestAuthSuccessSetsLocals(t *testing.T) {
	info := &SessionInfo{UserID: uuid.New(), Origin: sessionscope.OriginLogin, Scope: sessionscope.ScopeWrite}
	app := newAuthTestApp(&fakeValidator{info: info})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got struct {
		UserID string `json:"user_id"`
		Origin string `json:"origin"`
		Scope  string `json:"scope"`
		Frozen bool   `json:"frozen"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserID != info.UserID.String() {
		t.Errorf("user_id = %q, want %q", got.UserID, info.UserID.String())
	}
	if got.Origin != sessionscope.OriginLogin {
		t.Errorf("origin = %q, want %q", got.Origin, sessionscope.OriginLogin)
	}
	if got.Scope != sessionscope.ScopeWrite {
		t.Errorf("scope = %q, want %q", got.Scope, sessionscope.ScopeWrite)
	}
	if got.Frozen != false {
		t.Errorf("frozen = %v, want false", got.Frozen)
	}
}

// The accessor functions must be safe to call even when Auth never ran (e.g. a public route),
// returning zero values rather than panicking on a bad type assertion.
func TestContextAccessorsZeroValueWhenUnset(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"user_id": UserIDFromContext(c),
			"origin":  SessionOriginFromContext(c),
			"scope":   SessionScopeFromContext(c),
			"frozen":  UserFrozenFromContext(c),
		})
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	var got struct {
		UserID string `json:"user_id"`
		Origin string `json:"origin"`
		Scope  string `json:"scope"`
		Frozen bool   `json:"frozen"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserID != uuid.Nil.String() {
		t.Errorf("user_id = %q, want zero uuid", got.UserID)
	}
	if got.Origin != "" || got.Scope != "" {
		t.Errorf("origin/scope = %q/%q, want empty", got.Origin, got.Scope)
	}
	if got.Frozen != false {
		t.Errorf("frozen = %v, want false", got.Frozen)
	}
}

func TestRequireWrite(t *testing.T) {
	cases := []struct {
		name     string
		scope    string
		wantCode int
	}{
		{"write scope allowed", sessionscope.ScopeWrite, fiber.StatusOK},
		{"read scope blocked", sessionscope.ScopeRead, fiber.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := &SessionInfo{UserID: uuid.New(), Origin: sessionscope.OriginLogin, Scope: c.scope}
			app := newAuthTestApp(&fakeValidator{info: info}, RequireWrite)

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != c.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.wantCode)
			}
		})
	}
}

func TestRequireNotFrozen(t *testing.T) {
	cases := []struct {
		name     string
		frozen   bool
		wantCode int
	}{
		{"not frozen allowed", false, fiber.StatusOK},
		{"frozen blocked", true, fiber.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := &SessionInfo{UserID: uuid.New(), Origin: sessionscope.OriginLogin, Scope: sessionscope.ScopeWrite, Frozen: c.frozen}
			app := newAuthTestApp(&fakeValidator{info: info}, RequireNotFrozen)

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != c.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.wantCode)
			}
		})
	}
}

func TestRequireLoginOrigin(t *testing.T) {
	cases := []struct {
		name     string
		origin   string
		wantCode int
	}{
		{"login origin allowed", sessionscope.OriginLogin, fiber.StatusOK},
		{"minted origin blocked", sessionscope.OriginMinted, fiber.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := &SessionInfo{UserID: uuid.New(), Origin: c.origin, Scope: sessionscope.ScopeWrite}
			app := newAuthTestApp(&fakeValidator{info: info}, RequireLoginOrigin)

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set(fiber.HeaderAuthorization, "Bearer sometoken")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != c.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.wantCode)
			}
		})
	}
}
