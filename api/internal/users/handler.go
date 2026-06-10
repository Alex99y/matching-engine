package users

import (
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
)

type UserHandler struct {
	logger      *logger.Logger
	userService *UserService
}

type CreateUserRequest struct {
	Username string `json:"username" validate:"required"`
	Email    string `json:"email" validate:"required"`
	Password string `json:"password" validate:"required,min=10"`
}

type LoginUserRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required,min=10"`
}

type LoginUserResponse struct {
	Token string `json:"token"`
}

type UsernameAvailableRequest struct {
	Username string `json:"username" validate:"required"`
}

type UsernameAvailableResponse struct {
	Available bool `json:"available"`
}

func (u *UserHandler) CreateUser(c fiber.Ctx) error {
	var req CreateUserRequest
	if err := c.Bind().Body(&req); err != nil {
		u.logger.Error("CreateUser: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	if err := u.userService.CreateNewUser(c.Context(), req.Username, req.Email, req.Password); err != nil {
		if errors.Is(err, ErrUserAlreadyExist) {
			return utils.NewErrorResponse(c, fiber.StatusConflict, "username already taken")
		}
		return utils.NewServerErrorResponse(c, u.logger, err)
	}

	return c.SendStatus(fiber.StatusCreated)
}

func (u *UserHandler) LoginUser(c fiber.Ctx) error {
	var req LoginUserRequest
	if err := c.Bind().Body(&req); err != nil {
		u.logger.Error("LoginUser: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	token, err := u.userService.LoginUser(c.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidPassword) {
			return utils.NewErrorResponse(c, fiber.StatusUnauthorized, "invalid username or password")
		}
		return utils.NewServerErrorResponse(c, u.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(LoginUserResponse{Token: token})
}

func (u *UserHandler) IsUsernameAvailable(c fiber.Ctx) error {
	var req UsernameAvailableRequest
	if err := c.Bind().Body(&req); err != nil {
		u.logger.Error("IsUsernameAvailable: invalid body, request_id=" + requestid.FromContext(c))
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid request body")
	}

	available, err := u.userService.IsUsernameAvailable(c.Context(), req.Username)
	if err != nil {
		return utils.NewServerErrorResponse(c, u.logger, err)
	}

	return c.Status(fiber.StatusOK).JSON(UsernameAvailableResponse{Available: available})
}

type BalanceResponse struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Balance  int64  `json:"balance"`
	Blocked  int64  `json:"blocked"`
}

func (u *UserHandler) GetBalance(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)

	balances, err := u.userService.GetUserBalances(c.Context(), userID)
	if err != nil {
		return utils.NewServerErrorResponse(c, u.logger, err)
	}

	response := make([]BalanceResponse, len(balances))
	for i, b := range balances {
		response[i] = BalanceResponse{
			Name:     b.InstrumentName,
			Symbol:   b.InstrumentSymbol,
			Decimals: b.InstrumentDecimals,
			Balance:  b.Balance,
			Blocked:  b.Blocked,
		}
	}

	return c.JSON(response)
}

func NewUserHandler(logger *logger.Logger, userService *UserService) *UserHandler {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if userService == nil {
		panic("userService cannot be nil")
	}
	return &UserHandler{logger: logger, userService: userService}
}
