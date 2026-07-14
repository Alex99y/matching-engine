package users

import (
	"context"
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/password"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

var (
	ErrHashingPassword  = errors.New("couldn't hash password")
	ErrCreatingUser     = errors.New("error creating user")
	ErrUserAlreadyExist = repository.ErrUserAlreadyExists
	ErrGettingUser      = repository.ErrUserGetFailed
	ErrOnValidating     = errors.New("error validating credentials")
	ErrGetBalances      = errors.New("error getting balances")
)

type UserRepository interface {
	InsertUser(ctx context.Context, username, email, passwordHash string) error
	GetUserByUsername(ctx context.Context, username string) (*repository.User, error)
	GetUserBalances(ctx context.Context, userID uuid.UUID) ([]repository.UserBalance, error)
}

type UserService struct {
	logger         *logger.Logger
	userRepository UserRepository
}

func (u *UserService) IsUsernameAvailable(ctx context.Context, username string) (bool, error) {
	_, err := u.userRepository.GetUserByUsername(ctx, username)
	if errors.Is(err, repository.ErrUserNotFound) {
		return true, nil
	}
	if err != nil {
		return false, ErrGettingUser
	}
	return false, nil
}

func (u *UserService) CreateNewUser(
	ctx context.Context,
	username string,
	email string,
	userPassword string,
) error {
	hashedPassword, err := password.Hash(userPassword)
	if err != nil {
		u.logger.ErrorO(err)
		return ErrHashingPassword
	}

	err = u.userRepository.InsertUser(ctx, username, email, hashedPassword)
	if err != nil {
		if errors.Is(err, repository.ErrUserAlreadyExists) {
			return ErrUserAlreadyExist
		}
		return ErrCreatingUser
	}
	return nil
}

// ValidateCredentials checks the username/password pair and returns the user's ID on success.
// Returns middleware.ErrInvalidCredentials when the pair is wrong so the sessions handler
// can map it to a 401 without importing this package.
func (u *UserService) ValidateCredentials(
	ctx context.Context,
	username string,
	userPassword string,
) (uuid.UUID, error) {
	storedUser, err := u.userRepository.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return uuid.UUID{}, middleware.ErrInvalidCredentials
		}
		return uuid.UUID{}, ErrOnValidating
	}

	isValid, err := password.Verify(userPassword, storedUser.PasswordHash)
	if err != nil {
		u.logger.ErrorO(err)
		return uuid.UUID{}, ErrOnValidating
	}
	if !isValid {
		return uuid.UUID{}, middleware.ErrInvalidCredentials
	}

	return storedUser.ID, nil
}

func (u *UserService) GetUserBalances(ctx context.Context, userID uuid.UUID) ([]repository.UserBalance, error) {
	balances, err := u.userRepository.GetUserBalances(ctx, userID)
	if err != nil {
		return nil, ErrGetBalances
	}
	return balances, nil
}

func NewUserService(
	logger *logger.Logger,
	userRepository UserRepository,
) *UserService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if userRepository == nil {
		panic("user repository cannot be nil")
	}
	return &UserService{
		logger:         logger,
		userRepository: userRepository,
	}
}
