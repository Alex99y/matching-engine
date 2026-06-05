package instruments

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	ErrInstrumentAlreadyExists = repository.ErrInstrumentAlreadyExists
	ErrInstrumentNotFound      = repository.ErrInstrumentNotFound
	ErrCreatingInstrument      = errors.New("error creating instrument")
	ErrGettingInstrument       = repository.ErrInstrumentGetFailed
	ErrDeletingInstrument      = errors.New("error deleting instrument")
)

type Instrument struct {
	Name      string
	Symbol    string
	Decimals  int
	CreatedAt time.Time
}

type InstrumentRepository interface {
	CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error
	GetInstrument(ctx context.Context, symbol string) (*repository.Instrument, error)
	GetInstruments(ctx context.Context) ([]repository.Instrument, error)
	RemoveOneInstrument(ctx context.Context, symbol string) error
}

type InstrumentService struct {
	logger               *logger.Logger
	instrumentRepository InstrumentRepository
}

func (s *InstrumentService) CreateNewInstrument(ctx context.Context, name, symbol string, decimals int) error {
	if err := s.instrumentRepository.CreateNewInstrument(ctx, name, symbol, decimals); err != nil {
		if errors.Is(err, repository.ErrInstrumentAlreadyExists) {
			return ErrInstrumentAlreadyExists
		}
		return ErrCreatingInstrument
	}
	return nil
}

func (s *InstrumentService) GetInstrument(ctx context.Context, symbol string) (*Instrument, error) {
	inst, err := s.instrumentRepository.GetInstrument(ctx, symbol)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, ErrInstrumentNotFound
		}
		return nil, ErrGettingInstrument
	}
	return &Instrument{
		Name:      inst.Name,
		Symbol:    inst.Symbol,
		Decimals:  inst.Decimals,
		CreatedAt: inst.CreatedAt,
	}, nil
}

func (s *InstrumentService) GetInstruments(ctx context.Context) ([]Instrument, error) {
	repoInstruments, err := s.instrumentRepository.GetInstruments(ctx)
	if err != nil {
		return nil, ErrGettingInstrument
	}
	instruments := make([]Instrument, len(repoInstruments))
	for i, inst := range repoInstruments {
		instruments[i] = Instrument{
			Name:      inst.Name,
			Symbol:    inst.Symbol,
			Decimals:  inst.Decimals,
			CreatedAt: inst.CreatedAt,
		}
	}
	return instruments, nil
}

func (s *InstrumentService) RemoveOneInstrument(ctx context.Context, symbol string) error {
	if err := s.instrumentRepository.RemoveOneInstrument(ctx, symbol); err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return ErrInstrumentNotFound
		}
		return ErrDeletingInstrument
	}
	return nil
}

func NewInstrumentService(
	logger *logger.Logger,
	instrumentRepository InstrumentRepository,
) *InstrumentService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if instrumentRepository == nil {
		panic("instrument repository cannot be nil")
	}
	return &InstrumentService{
		logger:               logger,
		instrumentRepository: instrumentRepository,
	}
}
