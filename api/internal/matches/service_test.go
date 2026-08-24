package matches_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alex99y/matching-engine/api/internal/matches"
	"github.com/alex99y/matching-engine/common/pkg/logger"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

type fakeMatchRepository struct {
	rows []repository.Match
	err  error

	gotMarketID          int
	gotStartDate, gotEnd *time.Time
	gotLimit             int
}

func (f *fakeMatchRepository) GetMatches(ctx context.Context, marketID int, startDate, endDate *time.Time, limit int) ([]repository.Match, error) {
	f.gotMarketID = marketID
	f.gotStartDate = startDate
	f.gotEnd = endDate
	f.gotLimit = limit
	return f.rows, f.err
}

func TestGetMatchesDefaultsLimitTo100(t *testing.T) {
	repo := &fakeMatchRepository{}
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), repo)

	if _, err := svc.GetMatches(context.Background(), 1, matches.GetMatchesFilter{}); err != nil {
		t.Fatalf("GetMatches: %v", err)
	}
	if repo.gotLimit != 100 {
		t.Fatalf("limit passed to repo = %d, want 100 (default)", repo.gotLimit)
	}
}

func TestGetMatchesRejectsLimitOver100(t *testing.T) {
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), &fakeMatchRepository{})

	_, err := svc.GetMatches(context.Background(), 1, matches.GetMatchesFilter{Limit: 101})
	if !errors.Is(err, matches.ErrInvalidLimit) {
		t.Fatalf("err = %v, want ErrInvalidLimit", err)
	}
}

func TestGetMatchesPassesLimitAndDateRangeThrough(t *testing.T) {
	repo := &fakeMatchRepository{}
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), repo)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := svc.GetMatches(context.Background(), 7, matches.GetMatchesFilter{
		StartDate: &start,
		EndDate:   &end,
		Limit:     10,
	}); err != nil {
		t.Fatalf("GetMatches: %v", err)
	}
	if repo.gotMarketID != 7 || repo.gotLimit != 10 {
		t.Fatalf("repo got marketID=%d limit=%d, want 7, 10", repo.gotMarketID, repo.gotLimit)
	}
	if repo.gotStartDate == nil || !repo.gotStartDate.Equal(start) {
		t.Fatalf("repo got startDate=%v, want %v", repo.gotStartDate, start)
	}
	if repo.gotEnd == nil || !repo.gotEnd.Equal(end) {
		t.Fatalf("repo got endDate=%v, want %v", repo.gotEnd, end)
	}
}

func TestGetMatchesMapsRepositoryRows(t *testing.T) {
	id := uuid.New()
	matchTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeMatchRepository{rows: []repository.Match{
		{ID: id, Price: 100, Quantity: 5, TakerSide: "buy", MatchTime: matchTime},
	}}
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), repo)

	got, err := svc.GetMatches(context.Background(), 1, matches.GetMatchesFilter{})
	if err != nil {
		t.Fatalf("GetMatches: %v", err)
	}
	if len(got) != 1 || got[0].ID != id || got[0].Price != 100 || got[0].Quantity != 5 ||
		got[0].TakerSide != "buy" || !got[0].MatchTime.Equal(matchTime) {
		t.Fatalf("got = %+v, want one match mapped from the repository row", got)
	}
}

func TestGetMatchesRepositoryErrorReturnsErrFetchFailed(t *testing.T) {
	svc := matches.NewMatchService(logger.NewLogger(logger.Error), &fakeMatchRepository{err: errors.New("dial tcp: connection refused")})

	_, err := svc.GetMatches(context.Background(), 1, matches.GetMatchesFilter{})
	if !errors.Is(err, matches.ErrFetchFailed) {
		t.Fatalf("err = %v, want ErrFetchFailed", err)
	}
}
