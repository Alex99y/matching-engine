package users_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/users"
	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/password"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

type insertCall struct {
	username, email, passwordHash string
}

type fakeUserRepository struct {
	insertErr   error
	insertCalls []insertCall

	usersByUsername map[string]*repository.User
	getUserErr      error

	balances    []repository.UserBalance
	balancesErr error

	operations    []repository.UserOperation
	operationsErr error
	gotLimit      int
}

func (f *fakeUserRepository) InsertUser(ctx context.Context, username, email, passwordHash string) error {
	f.insertCalls = append(f.insertCalls, insertCall{username, email, passwordHash})
	return f.insertErr
}

func (f *fakeUserRepository) GetUserByUsername(ctx context.Context, username string) (*repository.User, error) {
	if f.getUserErr != nil {
		return nil, f.getUserErr
	}
	if u, ok := f.usersByUsername[username]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (f *fakeUserRepository) GetUserBalances(ctx context.Context, userID uuid.UUID) ([]repository.UserBalance, error) {
	return f.balances, f.balancesErr
}

func (f *fakeUserRepository) GetUserOperations(ctx context.Context, userID uuid.UUID, startDate, endDate *time.Time, limit int) ([]repository.UserOperation, error) {
	f.gotLimit = limit
	return f.operations, f.operationsErr
}

func newTestService(repo users.UserRepository) *users.UserService {
	return users.NewUserService(logger.NewLogger(logger.Error), repo)
}

func TestIsUsernameAvailableTrueWhenNotFound(t *testing.T) {
	svc := newTestService(&fakeUserRepository{})

	available, err := svc.IsUsernameAvailable(context.Background(), "alice")
	if err != nil {
		t.Fatalf("IsUsernameAvailable: %v", err)
	}
	if !available {
		t.Fatal("available = false, want true")
	}
}

func TestIsUsernameAvailableFalseWhenTaken(t *testing.T) {
	repo := &fakeUserRepository{usersByUsername: map[string]*repository.User{"alice": {Username: "alice"}}}
	svc := newTestService(repo)

	available, err := svc.IsUsernameAvailable(context.Background(), "alice")
	if err != nil {
		t.Fatalf("IsUsernameAvailable: %v", err)
	}
	if available {
		t.Fatal("available = true, want false")
	}
}

func TestIsUsernameAvailableRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeUserRepository{getUserErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.IsUsernameAvailable(context.Background(), "alice")
	if !errors.Is(err, users.ErrGettingUser) {
		t.Fatalf("err = %v, want ErrGettingUser", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestCreateNewUserSuccessHashesPassword(t *testing.T) {
	repo := &fakeUserRepository{}
	svc := newTestService(repo)

	if err := svc.CreateNewUser(context.Background(), "alice", "alice@example.com", "correct-horse-battery"); err != nil {
		t.Fatalf("CreateNewUser: %v", err)
	}
	if len(repo.insertCalls) != 1 {
		t.Fatalf("insertCalls = %d, want 1", len(repo.insertCalls))
	}
	call := repo.insertCalls[0]
	if call.username != "alice" || call.email != "alice@example.com" {
		t.Fatalf("insertCalls[0] = %+v, want username=alice email=alice@example.com", call)
	}
	if call.passwordHash == "correct-horse-battery" || call.passwordHash == "" {
		t.Fatalf("passwordHash = %q, want a hashed value, not the raw password", call.passwordHash)
	}
	ok, err := password.Verify("correct-horse-battery", call.passwordHash)
	if err != nil || !ok {
		t.Fatalf("stored hash does not verify against the original password: ok=%v err=%v", ok, err)
	}
}

func TestCreateNewUserAlreadyExists(t *testing.T) {
	repo := &fakeUserRepository{insertErr: repository.ErrUserAlreadyExists}
	svc := newTestService(repo)

	err := svc.CreateNewUser(context.Background(), "alice", "alice@example.com", "correct-horse-battery")
	if !errors.Is(err, users.ErrUserAlreadyExist) {
		t.Fatalf("err = %v, want ErrUserAlreadyExist", err)
	}
}

func TestCreateNewUserRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeUserRepository{insertErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	err := svc.CreateNewUser(context.Background(), "alice", "alice@example.com", "correct-horse-battery")
	if !errors.Is(err, users.ErrCreatingUser) {
		t.Fatalf("err = %v, want ErrCreatingUser", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestValidateCredentialsSuccess(t *testing.T) {
	hash, err := password.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("password.Hash: %v", err)
	}
	userID := uuid.New()
	repo := &fakeUserRepository{usersByUsername: map[string]*repository.User{
		"alice": {ID: userID, Username: "alice", PasswordHash: hash},
	}}
	svc := newTestService(repo)

	got, err := svc.ValidateCredentials(context.Background(), "alice", "correct-horse-battery")
	if err != nil {
		t.Fatalf("ValidateCredentials: %v", err)
	}
	if got != userID {
		t.Fatalf("got = %v, want %v", got, userID)
	}
}

func TestValidateCredentialsUnknownUsernameReturnsGenericInvalidCredentials(t *testing.T) {
	svc := newTestService(&fakeUserRepository{})

	_, err := svc.ValidateCredentials(context.Background(), "ghost", "whatever-password")
	if !errors.Is(err, middleware.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestValidateCredentialsWrongPasswordReturnsGenericInvalidCredentials(t *testing.T) {
	hash, err := password.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("password.Hash: %v", err)
	}
	repo := &fakeUserRepository{usersByUsername: map[string]*repository.User{
		"alice": {Username: "alice", PasswordHash: hash},
	}}
	svc := newTestService(repo)

	_, err = svc.ValidateCredentials(context.Background(), "alice", "wrong-password")
	if !errors.Is(err, middleware.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestValidateCredentialsRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeUserRepository{getUserErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.ValidateCredentials(context.Background(), "alice", "whatever-password")
	if !errors.Is(err, users.ErrOnValidating) {
		t.Fatalf("err = %v, want ErrOnValidating", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetUserBalancesSuccess(t *testing.T) {
	repo := &fakeUserRepository{balances: []repository.UserBalance{
		{InstrumentName: "Bitcoin", InstrumentSymbol: "BTC", InstrumentDecimals: 8, Balance: 100, Blocked: 10, Frozen: 5},
	}}
	svc := newTestService(repo)

	got, err := svc.GetUserBalances(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("GetUserBalances: %v", err)
	}
	if len(got) != 1 || got[0].InstrumentSymbol != "BTC" || got[0].Balance != 100 {
		t.Fatalf("got = %+v, unexpected shape", got)
	}
}

func TestGetUserBalancesRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeUserRepository{balancesErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetUserBalances(context.Background(), uuid.New())
	if !errors.Is(err, users.ErrGetBalances) {
		t.Fatalf("err = %v, want ErrGetBalances", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetUserOperationsDefaultsLimitTo100(t *testing.T) {
	repo := &fakeUserRepository{}
	svc := newTestService(repo)

	if _, err := svc.GetUserOperations(context.Background(), uuid.New(), users.GetUserOperationsFilter{}); err != nil {
		t.Fatalf("GetUserOperations: %v", err)
	}
	if repo.gotLimit != 100 {
		t.Fatalf("limit passed to repo = %d, want 100 (default)", repo.gotLimit)
	}
}

func TestGetUserOperationsRejectsLimitOver100(t *testing.T) {
	svc := newTestService(&fakeUserRepository{})

	_, err := svc.GetUserOperations(context.Background(), uuid.New(), users.GetUserOperationsFilter{Limit: 101})
	if !errors.Is(err, users.ErrInvalidLimit) {
		t.Fatalf("err = %v, want ErrInvalidLimit", err)
	}
}

func TestGetUserOperationsRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeUserRepository{operationsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetUserOperations(context.Background(), uuid.New(), users.GetUserOperationsFilter{})
	if !errors.Is(err, users.ErrGetOperations) {
		t.Fatalf("err = %v, want ErrGetOperations", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
