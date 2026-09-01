package users_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/users"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeValidator struct {
	userID uuid.UUID
}

func (f fakeValidator) ValidateToken(ctx context.Context, rawToken string) (*middleware.SessionInfo, error) {
	return &middleware.SessionInfo{UserID: f.userID}, nil
}

func newTestApp(repo users.UserRepository) *fiber.App {
	log := logger.NewLogger(logger.Error)
	svc := users.NewUserService(log, repo)
	h := users.NewUserHandler(log, svc)
	auth := fiber.Handler(middleware.Auth(log, fakeValidator{userID: uuid.New()}))

	// Same validator the real server installs (server.go) — without it Bind skips the
	// validate tags entirely and every field rule below would pass vacuously.
	app := fiber.New(fiber.Config{StructValidator: validations.NewStructValidator()})
	// Mirrors router.go: register/check-username are public, balances/operations require auth.
	app.Post("/users/register", h.CreateUser)
	app.Post("/users/check-username", h.IsUsernameAvailable)
	app.Get("/users/balances", auth, h.GetBalance)
	app.Get("/users/operations", auth, h.GetOperations)
	return app
}

func jsonRequest(method, url string, body any) *http.Request {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set(fiber.HeaderContentType, "application/json")
	req.Header.Set(fiber.HeaderAuthorization, "Bearer test-token")
	return req
}

func TestCreateUserHandlerInvalidBody(t *testing.T) {
	app := newTestApp(&fakeUserRepository{})

	req := httptest.NewRequest("POST", "/users/register", strings.NewReader("not-json"))
	req.Header.Set(fiber.HeaderContentType, "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// Every field rule is rejected with a message naming the offending field, not the generic
// "invalid request body" — a caller cannot fix a 400 it cannot attribute.
func TestCreateUserHandlerRejectsInvalidFieldsWithASpecificMessage(t *testing.T) {
	valid := users.CreateUserRequest{
		Username: "alice", Email: "alice@example.com", Password: "hunter2!",
	}
	cases := []struct {
		name    string
		mutate  func(*users.CreateUserRequest)
		wantMsg string
	}{
		{"password below the minimum", func(r *users.CreateUserRequest) { r.Password = "12345" }, "password must be at least 6 characters"},
		{"password absent", func(r *users.CreateUserRequest) { r.Password = "" }, "password is required"},
		{"password beyond the cap", func(r *users.CreateUserRequest) { r.Password = strings.Repeat("x", 129) }, "password must be at most 128 characters"},
		{"email malformed", func(r *users.CreateUserRequest) { r.Email = "alice-at-example" }, "email must be a valid email address"},
		{"email absent", func(r *users.CreateUserRequest) { r.Email = "" }, "email is required"},
		{"email beyond the column width", func(r *users.CreateUserRequest) { r.Email = strings.Repeat("a", 96) + "@x.io" }, "email must be at most 100 characters"},
		{"username absent", func(r *users.CreateUserRequest) { r.Username = "" }, "username is required"},
		{"username below the minimum", func(r *users.CreateUserRequest) { r.Username = "ab" }, "username must be at least 3 characters"},
		{"username beyond the column width", func(r *users.CreateUserRequest) { r.Username = strings.Repeat("a", 26) }, "username must be at most 25 characters"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(&fakeUserRepository{})
			body := valid
			tc.mutate(&body)

			resp, err := app.Test(jsonRequest("POST", "/users/register", body))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			var got struct {
				Message string `json:"message"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got.Message, tc.wantMsg)
			}
		})
	}
}

// A rejected registration must never reach the repository — the insert would otherwise be
// the thing enforcing the column widths, by failing.
func TestCreateUserHandlerDoesNotInsertWhenValidationFails(t *testing.T) {
	repo := &fakeUserRepository{}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("POST", "/users/register", users.CreateUserRequest{
		Username: "alice", Email: "not-an-email", Password: "hunter2!",
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(repo.insertCalls) != 0 {
		t.Fatalf("repository was called %d time(s) for a request that never validated", len(repo.insertCalls))
	}
}

func TestCreateUserHandlerSuccess(t *testing.T) {
	app := newTestApp(&fakeUserRepository{})

	resp, err := app.Test(jsonRequest("POST", "/users/register", users.CreateUserRequest{
		Username: "alice", Email: "alice@example.com", Password: "correct-horse-battery",
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

func TestCreateUserHandlerAlreadyExists(t *testing.T) {
	repo := &fakeUserRepository{insertErr: repository.ErrUserAlreadyExists}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("POST", "/users/register", users.CreateUserRequest{
		Username: "alice", Email: "alice@example.com", Password: "correct-horse-battery",
	}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestIsUsernameAvailableHandlerReturnsFlag(t *testing.T) {
	app := newTestApp(&fakeUserRepository{})

	resp, err := app.Test(jsonRequest("POST", "/users/check-username", users.UsernameAvailableRequest{Username: "alice"}))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got users.UsernameAvailableResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Available {
		t.Fatal("available = false, want true")
	}
}

func TestGetBalanceHandlerReturnsBalances(t *testing.T) {
	repo := &fakeUserRepository{balances: []repository.UserBalance{
		{InstrumentName: "Bitcoin", InstrumentSymbol: "BTC", InstrumentDecimals: 8, Balance: 100, Blocked: 10, Frozen: 5},
	}}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("GET", "/users/balances", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []users.BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "BTC" || got[0].Balance != 100 {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
}

func TestGetBalanceHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeUserRepository{balancesErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("GET", "/users/balances", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}

func TestGetOperationsHandlerInvalidLimit(t *testing.T) {
	app := newTestApp(&fakeUserRepository{})

	resp, err := app.Test(jsonRequest("GET", "/users/operations?limit=101", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetOperationsHandlerInvalidDate(t *testing.T) {
	app := newTestApp(&fakeUserRepository{})

	resp, err := app.Test(jsonRequest("GET", "/users/operations?start_date=not-a-date", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetOperationsHandlerReturnsOperations(t *testing.T) {
	id := uuid.New()
	reason := "faucet"
	repo := &fakeUserRepository{operations: []repository.UserOperation{
		{ID: id, InstrumentSymbol: "BTC", Amount: 100, Type: "deposit", Reason: &reason, CreatedAt: 123},
	}}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("GET", "/users/operations", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []users.OperationResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != id || got[0].Symbol != "BTC" || got[0].Reason != "faucet" {
		t.Fatalf("body = %+v, unexpected shape", got)
	}
}

func TestGetOperationsHandlerServiceErrorMapsTo500WithoutLeakingDetail(t *testing.T) {
	repo := &fakeUserRepository{operationsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	app := newTestApp(repo)

	resp, err := app.Test(jsonRequest("GET", "/users/operations", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("response leaked internal error detail: %s", body)
	}
}
