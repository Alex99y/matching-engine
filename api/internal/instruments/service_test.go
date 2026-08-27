package instruments_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/instruments"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

type createCall struct {
	name, symbol string
	decimals     int
}

type fakeInstrumentRepository struct {
	createErr   error
	createCalls []createCall

	instr    *repository.Instrument
	instrErr error

	instrs    []repository.Instrument
	instrsErr error

	removeErr   error
	removeCalls []string
}

func (f *fakeInstrumentRepository) CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error {
	f.createCalls = append(f.createCalls, createCall{name, symbol, decimals})
	return f.createErr
}

func (f *fakeInstrumentRepository) GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error) {
	return f.instr, f.instrErr
}

func (f *fakeInstrumentRepository) GetInstruments(ctx context.Context) ([]repository.Instrument, error) {
	return f.instrs, f.instrsErr
}

func (f *fakeInstrumentRepository) RemoveOneInstrument(ctx context.Context, symbol string) error {
	f.removeCalls = append(f.removeCalls, symbol)
	return f.removeErr
}

func newTestService(repo instruments.InstrumentRepository) *instruments.InstrumentService {
	return instruments.NewInstrumentService(logger.NewLogger(logger.Error), repo)
}

func TestCreateNewInstrumentSuccess(t *testing.T) {
	repo := &fakeInstrumentRepository{}
	svc := newTestService(repo)

	if err := svc.CreateNewInstrument(context.Background(), "Bitcoin", "BTC", 8); err != nil {
		t.Fatalf("CreateNewInstrument: %v", err)
	}
	if len(repo.createCalls) != 1 || repo.createCalls[0] != (createCall{"Bitcoin", "BTC", 8}) {
		t.Fatalf("createCalls = %+v, want one call with (Bitcoin, BTC, 8)", repo.createCalls)
	}
}

func TestCreateNewInstrumentAlreadyExists(t *testing.T) {
	repo := &fakeInstrumentRepository{createErr: repository.ErrInstrumentAlreadyExists}
	svc := newTestService(repo)

	err := svc.CreateNewInstrument(context.Background(), "Bitcoin", "BTC", 8)
	if !errors.Is(err, instruments.ErrInstrumentAlreadyExists) {
		t.Fatalf("err = %v, want ErrInstrumentAlreadyExists", err)
	}
}

func TestCreateNewInstrumentRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeInstrumentRepository{createErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	err := svc.CreateNewInstrument(context.Background(), "Bitcoin", "BTC", 8)
	if !errors.Is(err, instruments.ErrCreatingInstrument) {
		t.Fatalf("err = %v, want ErrCreatingInstrument", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetInstrumentSuccess(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeInstrumentRepository{instr: &repository.Instrument{
		Name: "Bitcoin", Symbol: "BTC", Decimals: 8, CreatedAt: created,
	}}
	svc := newTestService(repo)

	got, err := svc.GetInstrument(context.Background(), "BTC")
	if err != nil {
		t.Fatalf("GetInstrument: %v", err)
	}
	if got.Name != "Bitcoin" || got.Symbol != "BTC" || got.Decimals != 8 || !got.CreatedAt.Equal(created) {
		t.Fatalf("got = %+v, unexpected mapping", got)
	}
}

func TestGetInstrumentNotFound(t *testing.T) {
	repo := &fakeInstrumentRepository{instrErr: repository.ErrInstrumentNotFound}
	svc := newTestService(repo)

	_, err := svc.GetInstrument(context.Background(), "NOPE")
	if !errors.Is(err, instruments.ErrInstrumentNotFound) {
		t.Fatalf("err = %v, want ErrInstrumentNotFound", err)
	}
}

func TestGetInstrumentRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeInstrumentRepository{instrErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetInstrument(context.Background(), "BTC")
	if !errors.Is(err, instruments.ErrGettingInstrument) {
		t.Fatalf("err = %v, want ErrGettingInstrument", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestGetInstrumentsSuccess(t *testing.T) {
	repo := &fakeInstrumentRepository{instrs: []repository.Instrument{
		{Name: "Bitcoin", Symbol: "BTC", Decimals: 8},
		{Name: "Tether", Symbol: "USDT", Decimals: 6},
	}}
	svc := newTestService(repo)

	got, err := svc.GetInstruments(context.Background())
	if err != nil {
		t.Fatalf("GetInstruments: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "BTC" || got[1].Symbol != "USDT" {
		t.Fatalf("got = %+v, want [BTC USDT]", got)
	}
}

func TestGetInstrumentsRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeInstrumentRepository{instrsErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	_, err := svc.GetInstruments(context.Background())
	if !errors.Is(err, instruments.ErrGettingInstrument) {
		t.Fatalf("err = %v, want ErrGettingInstrument", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}

func TestRemoveOneInstrumentSuccess(t *testing.T) {
	repo := &fakeInstrumentRepository{}
	svc := newTestService(repo)

	if err := svc.RemoveOneInstrument(context.Background(), "BTC"); err != nil {
		t.Fatalf("RemoveOneInstrument: %v", err)
	}
	if len(repo.removeCalls) != 1 || repo.removeCalls[0] != "BTC" {
		t.Fatalf("removeCalls = %v, want [BTC]", repo.removeCalls)
	}
}

func TestRemoveOneInstrumentNotFound(t *testing.T) {
	repo := &fakeInstrumentRepository{removeErr: repository.ErrInstrumentNotFound}
	svc := newTestService(repo)

	err := svc.RemoveOneInstrument(context.Background(), "NOPE")
	if !errors.Is(err, instruments.ErrInstrumentNotFound) {
		t.Fatalf("err = %v, want ErrInstrumentNotFound", err)
	}
}

func TestRemoveOneInstrumentRepositoryErrorDoesNotLeak(t *testing.T) {
	repo := &fakeInstrumentRepository{removeErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")}
	svc := newTestService(repo)

	err := svc.RemoveOneInstrument(context.Background(), "BTC")
	if !errors.Is(err, instruments.ErrDeletingInstrument) {
		t.Fatalf("err = %v, want ErrDeletingInstrument", err)
	}
	if strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error leaked internal detail: %s", err.Error())
	}
}
