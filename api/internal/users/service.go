package users

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/api/pkg/jwt"
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
	ErrOnLogging        = errors.New("error logging in")
	ErrInvalidPassword  = errors.New("invalid username or password")
	ErrSessionExpired   = jwt.ErrExpiredToken
	ErrInvalidSession   = jwt.ErrInvalidToken
	ErrGetBalances      = errors.New("error getting balances")
)

type UserRepository interface {
	InsertUser(ctx context.Context, username, email, passwordHash string) error
	GetUserByUsername(ctx context.Context, username string) (*repository.User, error)
	GetUserBalances(ctx context.Context, userID uuid.UUID) ([]repository.UserBalance, error)
}

type JWTManager interface {
	Verify(session string) (*jwt.Claims, error)
	Sign(userId string, sessionId string, expiresOn time.Duration) (string, error)
}

type UserService struct {
	logger         *logger.Logger
	jwtManager     JWTManager
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
	return false, nil // user exists
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

	err = u.userRepository.InsertUser(
		ctx,
		username,
		email,
		hashedPassword,
	)

	if err != nil {
		if errors.Is(err, repository.ErrUserAlreadyExists) {
			return ErrUserAlreadyExist
		}
		return ErrCreatingUser
	}
	return nil
}

func (u *UserService) LoginUser(
	ctx context.Context,
	username string,
	userPassword string,
) (string, error) {
	storedUser, err := u.userRepository.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return "", ErrInvalidPassword
		}
		return "", ErrOnLogging
	}

	isValidPassword, err := password.Verify(userPassword, storedUser.PasswordHash)
	if err != nil {
		u.logger.ErrorO(err)
		return "", ErrOnLogging
	}
	if !isValidPassword {
		return "", ErrInvalidPassword
	}

	// @TODO: Persist login
	sessionId := "TODO"
	defaultTtl := time.Hour * 24 * 7
	jwt, err := u.jwtManager.Sign(storedUser.ID.String(), sessionId, defaultTtl)

	if err != nil {
		u.logger.Error("error signing jwt")
		u.logger.ErrorO(err)
		return "", ErrOnLogging
	}

	return jwt, nil
}

func (u *UserService) IsLoggedIn(jwt string) (*uuid.UUID, string, error) {
	session, err := u.jwtManager.Verify(jwt)
	if err != nil {
		return nil, "", err
	}

	id, err := uuid.Parse(session.UserID)

	if err != nil {
		return nil, "", ErrInvalidSession
	}

	return &id, session.SessionID, nil
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
	jwtManager JWTManager,
	userRepository UserRepository,
) *UserService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if jwtManager == nil {
		panic("jwt manager cannot be nil")
	}
	if userRepository == nil {
		panic("user repository cannot be nil")
	}
	return &UserService{
		logger:         logger,
		jwtManager:     jwtManager,
		userRepository: userRepository,
	}
}
