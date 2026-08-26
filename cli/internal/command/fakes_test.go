package command

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/alex99y/matching-engine/db/pkg/repository"
)

func setRepos(t *testing.T, instr instrumentRepository, mkt marketRepository, usr userRepository) {
	t.Helper()
	prevInstr, prevMkt, prevUsr := instrumentRepo, marketRepo, userRepo
	instrumentRepo, marketRepo, userRepo = instr, mkt, usr
	t.Cleanup(func() {
		instrumentRepo, marketRepo, userRepo = prevInstr, prevMkt, prevUsr
	})
}

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) (stdout string, err error) {
	t.Helper()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)

	old := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("pipe: %v", pipeErr)
	}
	os.Stdout = w

	err = cmd.Execute()

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), err
}

type instrumentCreateCall struct {
	name     string
	symbol   string
	decimals int
}

type fakeInstrumentRepo struct {
	createCalls       []instrumentCreateCall
	createErrBySymbol map[string]error

	instrument    *repository.Instrument
	instrumentErr error

	instruments    []repository.Instrument
	instrumentsErr error
}

func (f *fakeInstrumentRepo) CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error {
	f.createCalls = append(f.createCalls, instrumentCreateCall{name, symbol, decimals})
	return f.createErrBySymbol[symbol]
}

func (f *fakeInstrumentRepo) GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error) {
	if f.instrumentErr != nil {
		return nil, f.instrumentErr
	}
	return f.instrument, nil
}

func (f *fakeInstrumentRepo) GetInstruments(ctx context.Context) ([]repository.Instrument, error) {
	return f.instruments, f.instrumentsErr
}

type marketCreateCall struct {
	baseSymbol, quoteSymbol                                 string
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64
	takerFeeBps, makerFeeBps                                int64
}

type fakeMarketRepo struct {
	createCalls     []marketCreateCall
	createErrByName map[string]error

	market    *repository.Market
	marketErr error

	markets    []repository.Market
	marketsErr error
}

func (f *fakeMarketRepo) CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, takerFeeBps, makerFeeBps int64) error {
	f.createCalls = append(f.createCalls, marketCreateCall{
		baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize, takerFeeBps, makerFeeBps,
	})
	return f.createErrByName[baseSymbol+"-"+quoteSymbol]
}

func (f *fakeMarketRepo) GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error) {
	if f.marketErr != nil {
		return nil, f.marketErr
	}
	return f.market, nil
}

func (f *fakeMarketRepo) GetMarkets(ctx context.Context) ([]repository.Market, error) {
	return f.markets, f.marketsErr
}

type balanceCall struct {
	userID       uuid.UUID
	instrumentID int
	amount       int64
	reason       *string
}

type fakeUserRepo struct {
	usersByUsername map[string]*repository.User
	getUserErr      error

	freezeErr   error
	freezeCalls []uuid.UUID

	unfreezeErr   error
	unfreezeCalls []uuid.UUID

	addBalanceErr   error
	addBalanceCalls []balanceCall

	removeBalanceErr   error
	removeBalanceCalls []balanceCall

	freezeBalanceErr   error
	freezeBalanceCalls []balanceCall

	unfreezeBalanceErr   error
	unfreezeBalanceCalls []balanceCall
}

func (f *fakeUserRepo) GetUserByUsername(ctx context.Context, username string) (*repository.User, error) {
	if f.getUserErr != nil {
		return nil, f.getUserErr
	}
	if u, ok := f.usersByUsername[username]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (f *fakeUserRepo) FreezeUser(ctx context.Context, userID uuid.UUID) error {
	f.freezeCalls = append(f.freezeCalls, userID)
	return f.freezeErr
}

func (f *fakeUserRepo) UnfreezeUser(ctx context.Context, userID uuid.UUID) error {
	f.unfreezeCalls = append(f.unfreezeCalls, userID)
	return f.unfreezeErr
}

func (f *fakeUserRepo) AddUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	f.addBalanceCalls = append(f.addBalanceCalls, balanceCall{userID, instrumentID, amount, reason})
	return f.addBalanceErr
}

func (f *fakeUserRepo) RemoveUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	f.removeBalanceCalls = append(f.removeBalanceCalls, balanceCall{userID, instrumentID, amount, reason})
	return f.removeBalanceErr
}

func (f *fakeUserRepo) FreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	f.freezeBalanceCalls = append(f.freezeBalanceCalls, balanceCall{userID, instrumentID, amount, reason})
	return f.freezeBalanceErr
}

func (f *fakeUserRepo) UnfreezeUserBalance(ctx context.Context, userID uuid.UUID, instrumentID int, amount int64, reason *string) error {
	f.unfreezeBalanceCalls = append(f.unfreezeBalanceCalls, balanceCall{userID, instrumentID, amount, reason})
	return f.unfreezeBalanceErr
}
