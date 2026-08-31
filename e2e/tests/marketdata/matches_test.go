//go:build e2e

package marketdata

import (
	"net/http"
	"testing"

	"github.com/alex99y/matching-engine/e2e/internal/assert"
	"github.com/alex99y/matching-engine/e2e/internal/client"
)

// M3 — a completed trade shows up on the public tape.
//
// Rests a sell, crosses it with a buy, then reads the market's match history.
// Expect: the fill appears with the maker's price, the traded base quantity, and "buy" as the
// taker side; the feed is newest-first and its ids line up with the ones the two orders
// report privately.
func TestTradeAppearsOnTheMatchesFeed(t *testing.T) {
	ctx := env.Context(t)
	maker := env.NewFundedAccount(t)
	taker := env.NewFundedAccount(t)

	price, qty := env.Band(t), env.MinQty()
	makerID, takerID := env.Trade(t, ctx, maker, taker, price, qty)

	takerOrder := env.Fetch(t, ctx, taker.LoginToken, takerID)
	assert.Traded(t, takerOrder)
	matchID := takerOrder.Matches[0].ID

	// Both sides of the trade must reference the same match row as the tape does.
	makerOrder := env.Fetch(t, ctx, maker.LoginToken, makerID)
	assert.Traded(t, makerOrder)
	if makerOrder.Matches[0].ID != matchID {
		t.Fatalf("the two legs report different match ids: %s vs %s", makerOrder.Matches[0].ID, matchID)
	}

	var found client.MarketMatch
	assert.Eventually(t, ctx, func() error {
		feed, err := env.Client.GetMatches(ctx, env.Market.Ref, 100)
		if err != nil {
			return err
		}
		for _, m := range feed {
			if m.ID == matchID {
				found = m
				return nil
			}
		}
		return errMatchNotOnFeed{id: matchID, seen: len(feed)}
	})

	if found.Price != price {
		t.Fatalf("tape reports price %d, want the maker's %d", found.Price, price)
	}
	if found.Quantity != qty {
		t.Fatalf("tape reports quantity %d, want %d", found.Quantity, qty)
	}
	// The crossing order was the buy, so the tape must attribute the trade to the buy side.
	if found.TakerSide != string(client.Buy) {
		t.Fatalf("tape reports taker side %q, want %q", found.TakerSide, client.Buy)
	}
	if found.MatchTime <= 0 {
		t.Fatalf("tape reports match_time %d, want a unix timestamp", found.MatchTime)
	}

	feed, err := env.Client.GetMatches(ctx, env.Market.Ref, 100)
	if err != nil {
		t.Fatalf("get matches: %v", err)
	}
	for i := 1; i < len(feed); i++ {
		if feed[i-1].MatchTime < feed[i].MatchTime {
			t.Fatalf("matches are not newest-first: %d then %d", feed[i-1].MatchTime, feed[i].MatchTime)
		}
	}
}

// M3 — the tape rejects an unknown market rather than returning an empty list.
func TestMatchesFeedRejectsUnknownMarket(t *testing.T) {
	ctx := env.Context(t)

	if _, err := env.Client.GetMatches(ctx, "NOPE-NOPE", 10); err == nil {
		t.Fatal("GET /markets/NOPE-NOPE/matches succeeded, want 404")
	} else if got := client.Status(err); got != http.StatusNotFound {
		t.Fatalf("status = %d, want %d — %v", got, http.StatusNotFound, err)
	}
}

type errMatchNotOnFeed struct {
	id   string
	seen int
}

func (e errMatchNotOnFeed) Error() string {
	return "match " + e.id + " not on the feed yet"
}
