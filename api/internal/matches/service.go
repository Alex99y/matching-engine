package matches

import (
	"context"
	"errors"
	"time"

	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

var (
	ErrFetchFailed  = errors.New("failed to fetch matches")
	ErrInvalidLimit = errors.New("limit must be between 1 and 100")
)

const maxMatchesLimit = 100

type Match struct {
	ID        uuid.UUID
	Price     uint64
	Quantity  uint64
	TakerSide string
	MatchTime time.Time
}

type GetMatchesFilter struct {
	StartDate *time.Time
	EndDate   *time.Time
	Limit     int
}

type MatchRepository interface {
	GetMatches(ctx context.Context, marketID int, startDate, endDate *time.Time, limit int) ([]repository.Match, error)
}

type MatchService struct {
	logger *logger.Logger
	repo   MatchRepository
}

func (s *MatchService) GetMatches(ctx context.Context, marketID int, filter GetMatchesFilter) ([]Match, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = maxMatchesLimit
	} else if limit > maxMatchesLimit {
		return nil, ErrInvalidLimit
	}

	rows, err := s.repo.GetMatches(ctx, marketID, filter.StartDate, filter.EndDate, limit)
	if err != nil {
		return nil, ErrFetchFailed
	}
	out := make([]Match, len(rows))
	for i, r := range rows {
		out[i] = Match{
			ID:        r.ID,
			Price:     r.Price,
			Quantity:  r.Quantity,
			TakerSide: r.TakerSide,
			MatchTime: r.MatchTime,
		}
	}
	return out, nil
}

func NewMatchService(log *logger.Logger, repo MatchRepository) *MatchService {
	if log == nil {
		panic("logger cannot be nil")
	}
	if repo == nil {
		panic("repo cannot be nil")
	}
	return &MatchService{logger: log, repo: repo}
}
