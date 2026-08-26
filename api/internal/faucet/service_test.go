package faucet_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/alex99y/matching-engine/api/internal/faucet"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type fakeInstrumentRepository struct {
	instr *repository.Instrument
	err   error
}

func (f fakeInstrumentRepository) GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error) {
	return f.instr, f.err
}

type userBalanceCall struct {
	userID       uuid.UUID
	instrumentID int
	amount       int64
	reason       *string
}

type fakeUserRepository struct {
	addBalanceErr error
	calls         []userBalanceCall
}

func (f *fakeUserRepository) AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	f.calls = append(f.calls, userBalanceCall{userID, instrumentID, amount, reason})
	return f.addBalanceErr
}

func newTestService(instrRepo faucet.InstrumentRepository, userRepo faucet.UserRepository) *faucet.FaucetService {
	return faucet.NewFaucetService(logger.NewLogger(logger.Error), instrRepo, userRepo)
}

func TestRequestCreditsHardcodedAmountBySymbol(t *testing.T) {
	tests := []struct {
		symbol   string
		decimals int
		want     int64
	}{
		{"BTC", 8, 10_000_000},               // 0.1 BTC in satoshis
		{"ETH", 18, 500_000_000_000_000_000}, // 0.5 ETH in wei
		{"USDT", 6, 1_000_000_000},           // 1000 USDT in micro-USDT
		{"DOGE", 8, 100_000_000},             // no entry in faucetAmountTenths: falls back to 1 whole unit
	}

	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 42, Symbol: tt.symbol, Decimals: tt.decimals}}
			userRepo := &fakeUserRepository{}
			svc := newTestService(instrRepo, userRepo)

			userID := uuid.New()
			credit, err := svc.Request(context.Background(), userID, tt.symbol)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if credit.Symbol != tt.symbol || credit.Amount != tt.want {
				t.Fatalf("credit = %+v, want {%s %d}", credit, tt.symbol, tt.want)
			}

			if len(userRepo.calls) != 1 {
				t.Fatalf("AddUserBalance calls = %d, want 1", len(userRepo.calls))
			}
			call := userRepo.calls[0]
			if call.userID != userID || call.instrumentID != 42 || call.amount != tt.want {
				t.Fatalf("call = %+v, want userID=%v instrumentID=42 amount=%d", call, userID, tt.want)
			}
			if call.reason == nil || *call.reason != "faucet" {
				t.Fatalf("reason = %v, want \"faucet\"", call.reason)
			}
		})
	}
}

func TestRequestInstrumentNotFound(t *testing.T) {
	instrRepo := fakeInstrumentRepository{err: repository.ErrInstrumentNotFound}
	userRepo := &fakeUserRepository{}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "NOPE")
	if !errors.Is(err, faucet.ErrInstrumentNotFound) {
		t.Fatalf("err = %v, want ErrInstrumentNotFound", err)
	}
	if len(userRepo.calls) != 0 {
		t.Fatalf("AddUserBalance calls = %d, want 0", len(userRepo.calls))
	}
}

func TestRequestInstrumentRepositoryErrorDoesNotLeak(t *testing.T) {
	instrRepo := fakeInstrumentRepository{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	userRepo := &fakeUserRepository{}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "BTC")
	if !errors.Is(err, faucet.ErrGettingInstrument) {
		t.Fatalf("err = %v, want ErrGettingInstrument", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestRequestAmountOverflow(t *testing.T) {
	// 16 decimals combined with USDT's 10000-tenths credit pushes the amount past
	// math.MaxInt64 while staying inside bits.Div64's 64-bit quotient limit — the "quotient too
	// big for int64" branch of unitAmount's overflow check.
	instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 1, Symbol: "USDT", Decimals: 16}}
	userRepo := &fakeUserRepository{}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "USDT")
	if !errors.Is(err, faucet.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
	if len(userRepo.calls) != 0 {
		t.Fatalf("AddUserBalance calls = %d, want 0 (overflow must be caught before crediting)", len(userRepo.calls))
	}
}

// TestRequestAmountOverflowAtMul64Limit exercises the other overflow branch: at 18 decimals —
// the max allowed when creating an instrument, see instrumentInput.validate in
// cli/internal/command/instrument.go — USDT's 10000-tenths credit makes the Mul64 product's high
// word reach the divisor itself, which used to panic inside bits.Div64 instead of returning
// ErrAmountOverflow.
func TestRequestAmountOverflowAtMul64Limit(t *testing.T) {
	instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 1, Symbol: "USDT", Decimals: 18}}
	userRepo := &fakeUserRepository{}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "USDT")
	if !errors.Is(err, faucet.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
	if len(userRepo.calls) != 0 {
		t.Fatalf("AddUserBalance calls = %d, want 0 (overflow must be caught before crediting)", len(userRepo.calls))
	}
}

func TestRequestDecimalsOutOfPow10Range(t *testing.T) {
	// decimals has no DB-level bound (no CHECK constraint on instruments.decimals), and
	// Pow10Uint64 panics above 19 — a corrupted or hand-edited row must still get a clean error.
	instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 1, Symbol: "BTC", Decimals: 20}}
	userRepo := &fakeUserRepository{}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "BTC")
	if !errors.Is(err, faucet.ErrAmountOverflow) {
		t.Fatalf("err = %v, want ErrAmountOverflow", err)
	}
	if len(userRepo.calls) != 0 {
		t.Fatalf("AddUserBalance calls = %d, want 0 (overflow must be caught before crediting)", len(userRepo.calls))
	}
}

func TestRequestCreditingBalanceErrorDoesNotLeak(t *testing.T) {
	instrRepo := fakeInstrumentRepository{instr: &repository.Instrument{ID: 1, Symbol: "BTC", Decimals: 8}}
	userRepo := &fakeUserRepository{addBalanceErr: errors.New("constraint violation on user_balances_pkey")}
	svc := newTestService(instrRepo, userRepo)

	_, err := svc.Request(context.Background(), uuid.New(), "BTC")
	if !errors.Is(err, faucet.ErrCreditingBalance) {
		t.Fatalf("err = %v, want ErrCreditingBalance", err)
	}
	if strings.Contains(err.Error(), "user_balances_pkey") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
