package faucet

import (
	"context"
	"errors"
	"math"
	"math/bits"

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

// faucetAmountTenths hardcodes how much of each instrument a single faucet call credits, in
// tenths of a whole unit (BTC: 1 == 0.1, ETH: 5 == 0.5, USDT: 10000 == 1000) — tenths keep the
// fractional amounts exact integer math instead of float64, which can't represent 0.1 precisely.
// Symbols not listed here fall back to defaultFaucetAmountTenths (1 whole unit).
var faucetAmountTenths = map[string]uint64{
	"BTC":  1,
	"ETH":  5,
	"USDT": 10000,
}

const defaultFaucetAmountTenths = 10

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

// Request credits userID with symbol's hardcoded faucetAmountTenths amount and returns what
// was credited.
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

	amount, err := unitAmount(instr.Symbol, instr.Decimals)
	if err != nil {
		return nil, err
	}

	reason := faucetOperationReason
	if err := s.userRepository.AddUserBalance(ctx, userID, instr.ID, amount, &reason); err != nil {
		return nil, ErrCreditingBalance
	}

	return &FaucetCredit{Symbol: instr.Symbol, Amount: amount}, nil
}

// unitAmount computes symbol's hardcoded credit amount at decimals precision. Uses
// bits.Mul64/Div64 (128-bit-safe intermediate) rather than a raw uint64 multiply, since
// Pow10Uint64(decimals) * tenths can exceed uint64 range before the /10 brings it back down
// (e.g. an 18-decimal instrument at 10000 tenths) — same pattern as feeOf() in
// core/internal/orderbook/orderbook.go.
func unitAmount(symbol string, decimals int) (int64, error) {
	tenths, ok := faucetAmountTenths[symbol]
	if !ok {
		tenths = defaultFaucetAmountTenths
	}
	hi, lo := bits.Mul64(utils.Pow10Uint64(decimals), tenths)
	scaled, _ := bits.Div64(hi, lo, 10)
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
