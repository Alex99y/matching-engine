package users

import (
	"errors"
	"strconv"

	"github.com/alex99y/matching-engine/api/pkg/middleware"
	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/api/pkg/validations"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/gofiber/fiber/v3"
	requestid "github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/google/uuid"
)

type UserHandler struct {
	logger      *logger.Logger
	userService *UserService
}

// Lengths mirror the users table (username VARCHAR(25), email VARCHAR(100)) so an
// over-long value is refused with a field error instead of a driver error on insert.
// The password bound is not a storage limit — the hash is fixed width — but argon2id
// costs CPU proportional to its input, so an unbounded password is a cheap way to
// burn the server's time.
type CreateUserRequest struct {
	Username string `json:"username" validate:"required,min=3,max=25"`
	Email    string `json:"email" validate:"required,email,max=100"`
	Password string `json:"password" validate:"required,min=6,max=128"`
}

type UsernameAvailableRequest struct {
	Username string `json:"username" validate:"required,min=3,max=25"`
}

type UsernameAvailableResponse struct {
	Available bool `json:"available"`
}

func (u *UserHandler) CreateUser(c fiber.Ctx) error {
	var req CreateUserRequest
	if err := c.Bind().Body(&req); err != nil {
		// Bind reports a malformed body and a failed field rule through the same error, so
		// the caller only learns which field is wrong if the validation case is unwrapped.
		if msg, ok := validations.FormatError(err); ok {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, msg)
		}
		u.logger.Error("CreateUser: invalid body, request_id=" + requestid.FromContext(c) + ": " + err.Error())
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

func (u *UserHandler) IsUsernameAvailable(c fiber.Ctx) error {
	var req UsernameAvailableRequest
	if err := c.Bind().Body(&req); err != nil {
		if msg, ok := validations.FormatError(err); ok {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, msg)
		}
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
	Frozen   int64  `json:"frozen"`
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
			Frozen:   b.Frozen,
		}
	}

	return c.JSON(response)
}

type OperationResponse struct {
	ID        uuid.UUID `json:"id"`
	Symbol    string    `json:"symbol"`
	Amount    int64     `json:"amount"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt int64     `json:"created_at"`
}

func (u *UserHandler) GetOperations(c fiber.Ctx) error {
	userID := middleware.UserIDFromContext(c)

	var filter GetUserOperationsFilter

	startDate, err := utils.ParseDateQuery(c, "start_date")
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	filter.StartDate = startDate

	endDate, err := utils.ParseDateQuery(c, "end_date")
	if err != nil {
		return utils.NewErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	filter.EndDate = endDate

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "invalid limit, must be a positive integer")
		}
		filter.Limit = n
	}

	operations, err := u.userService.GetUserOperations(c.Context(), userID, filter)
	if err != nil {
		if errors.Is(err, ErrInvalidLimit) {
			return utils.NewErrorResponse(c, fiber.StatusBadRequest, "limit must be between 1 and 100")
		}
		return utils.NewServerErrorResponse(c, u.logger, err)
	}

	response := make([]OperationResponse, len(operations))
	for i, op := range operations {
		resp := OperationResponse{
			ID:        op.ID,
			Symbol:    op.InstrumentSymbol,
			Amount:    op.Amount,
			Type:      op.Type,
			CreatedAt: op.CreatedAt,
		}
		if op.Reason != nil {
			resp.Reason = *op.Reason
		}
		response[i] = resp
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
