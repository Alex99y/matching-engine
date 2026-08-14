package faucet

import (
	"context"
	"errors"
	"math"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

var (
	ErrInstrumentNotFound = repository.ErrInstrumentNotFound
	ErrGettingInstrument  = repository.ErrInstrumentGetFailed
	ErrAmountOverflow     = errors.New("faucet amount overflows int64 for this instrument's decimals")
	ErrCreditingBalance   = errors.New("error crediting faucet balance")
)

// faucetUnits is how many whole units of an instrument a single call credits. Kept at 1 so
// unitAmount never overflows int64 even at the highest decimals the CLI allows instrument
// creation with (18, e.g. ETH) — 10^18 fits comfortably, 10 * 10^18 would not.
const faucetUnits = 1

const faucetOperationReason = "faucet"

type InstrumentRepository interface {
	GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error)
}

type UserRepository interface {
	AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error
}

type FaucetService struct {
	logger               *logger.Logger
	instrumentRepository InstrumentRepository
	userRepository       UserRepository
}

type FaucetCredit struct {
	Symbol string
	Amount int64
}

// Request credits userID with faucetUnits worth of symbol and returns what was credited.
//
// TODO(spam): this endpoint has no per-user/per-IP rate limit and no lifetime cap — any
// authenticated user can call it as fast as they want and mint unbounded balance. Acceptable for
// a sandbox/POC only; before this runs anywhere that matters, add a limiter (see the commented-out
// one in api/internal/server/server.go) and/or a running total cap per user+instrument.
func (s *FaucetService) Request(ctx context.Context, userID uuid.UUID, symbol string) (*FaucetCredit, error) {
	instr, err := s.instrumentRepository.GetInstrument(ctx, symbol)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, ErrInstrumentNotFound
		}
		return nil, ErrGettingInstrument
	}

	amount, err := unitAmount(instr.Decimals)
	if err != nil {
		return nil, err
	}

	reason := faucetOperationReason
	if err := s.userRepository.AddUserBalance(ctx, userID, instr.ID, amount, &reason); err != nil {
		return nil, ErrCreditingBalance
	}

	return &FaucetCredit{Symbol: instr.Symbol, Amount: amount}, nil
}

func unitAmount(decimals int) (int64, error) {
	scaled := utils.Pow10Uint64(decimals) * faucetUnits
	if scaled > math.MaxInt64 {
		return 0, ErrAmountOverflow
	}
	return int64(scaled), nil
}

func NewFaucetService(
	logger *logger.Logger,
	instrumentRepository InstrumentRepository,
	userRepository UserRepository,
) *FaucetService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if instrumentRepository == nil {
		panic("instrument repository cannot be nil")
	}
	if userRepository == nil {
		panic("user repository cannot be nil")
	}
	return &FaucetService{
		logger:               logger,
		instrumentRepository: instrumentRepository,
		userRepository:       userRepository,
	}
}
