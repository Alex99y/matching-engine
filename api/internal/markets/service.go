package markets

import (
	"context"
	"errors"

	"github.com/alex99y/matching-engine/api/pkg/utils"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

var (
	ErrMarketAlreadyExists = repository.ErrMarketAlreadyExists
	ErrMarketNotFound      = repository.ErrMarketNotFound
	ErrCreatingMarket      = errors.New("error creating market")
	ErrGettingMarket       = repository.ErrMarketGetFailed
	ErrDeletingMarket      = errors.New("error deleting market")
	ErrInvalidMarketRef    = utils.ErrInvalidMarketRef
	ErrInvalidInstruments  = repository.ErrInvalidInstruments
)

type Market struct {
	BaseSymbol    string
	QuoteSymbol   string
	PriceQuantum  int64
	AmountQuantum int64
	MinOrderSize  int64
	MaxOrderSize  int64
}

type MarketRepository interface {
	CreateMarket(ctx context.Context, baseSymbol, quoteSymbol string, priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64) error
	GetMarket(ctx context.Context, baseSymbol, quoteSymbol string) (*repository.Market, error)
	GetMarkets(ctx context.Context) ([]repository.Market, error)
	RemoveOneMarket(ctx context.Context, baseSymbol, quoteSymbol string) error
}

type MarketService struct {
	logger           *logger.Logger
	marketRepository MarketRepository
}

func (s *MarketService) CreateMarket(
	ctx context.Context,
	marketRef string,
	priceQuantum, amountQuantum, minOrderSize, maxOrderSize int64,
) error {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return err
	}

	if err := s.marketRepository.CreateMarket(ctx, baseSymbol, quoteSymbol, priceQuantum, amountQuantum, minOrderSize, maxOrderSize); err != nil {
		if errors.Is(err, repository.ErrMarketAlreadyExists) {
			return ErrMarketAlreadyExists
		}
		if errors.Is(err, repository.ErrInvalidInstruments) {
			return ErrInvalidInstruments
		}
		return ErrCreatingMarket
	}
	return nil
}

func (s *MarketService) GetMarket(ctx context.Context, marketRef string) (*Market, error) {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return nil, err
	}

	m, err := s.marketRepository.GetMarket(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		if errors.Is(err, repository.ErrMarketNotFound) {
			return nil, ErrMarketNotFound
		}
		return nil, ErrGettingMarket
	}
	return &Market{
		BaseSymbol:    m.BaseSymbol,
		QuoteSymbol:   m.QuoteSymbol,
		PriceQuantum:  m.PriceQuantum,
		AmountQuantum: m.AmountQuantum,
		MinOrderSize:  m.MinOrderSize,
		MaxOrderSize:  m.MaxOrderSize,
	}, nil
}

func (s *MarketService) GetMarkets(ctx context.Context) ([]Market, error) {
	repoMarkets, err := s.marketRepository.GetMarkets(ctx)
	if err != nil {
		return nil, ErrGettingMarket
	}
	markets := make([]Market, len(repoMarkets))
	for i, m := range repoMarkets {
		markets[i] = Market{
			BaseSymbol:    m.BaseSymbol,
			QuoteSymbol:   m.QuoteSymbol,
			PriceQuantum:  m.PriceQuantum,
			AmountQuantum: m.AmountQuantum,
			MinOrderSize:  m.MinOrderSize,
			MaxOrderSize:  m.MaxOrderSize,
		}
	}
	return markets, nil
}

func (s *MarketService) RemoveOneMarket(ctx context.Context, marketRef string) error {
	baseSymbol, quoteSymbol, err := utils.SplitMarketRef(marketRef)
	if err != nil {
		return err
	}

	if err := s.marketRepository.RemoveOneMarket(ctx, baseSymbol, quoteSymbol); err != nil {
		if errors.Is(err, repository.ErrMarketNotFound) {
			return ErrMarketNotFound
		}
		return ErrDeletingMarket
	}
	return nil
}

func NewMarketService(
	logger *logger.Logger,
	marketRepository MarketRepository,
) *MarketService {
	if logger == nil {
		panic("logger cannot be nil")
	}
	if marketRepository == nil {
		panic("market repository cannot be nil")
	}
	return &MarketService{
		logger:           logger,
		marketRepository: marketRepository,
	}
}
