package orderbook

import (
	"testing"
	"time"

	oeq "github.com/alex99y/matching-engine/core/pkg/order_events_queue"
	"github.com/alex99y/matching-engine/db/pkg/repository"
	"github.com/google/uuid"
)

// Hydrate must repopulate the expiry index from persisted orders (not just the price-level
// book), so a TTL set before a restart is still enforced after one.
func TestHydrate_RepopulatesExpiryIndex(t *testing.T) {
	o := testBook()
	id := restSellExpiring(o, uuid.New(), 100, 10, unixPtr(1000))

	if got := o.ExpireDue(1000); len(got) != 1 || got[0] != id {
		t.Fatalf("ExpireDue after Hydrate = %v, want [%s]", got, id)
	}
}

// orders.expires_at is a TIMESTAMP without a zone, so the wall clock handed to the driver is
// stored verbatim. time.Unix yields local time, which on a non-UTC host wrote an expiry offset
// from created_at (which the database sets in UTC) — west of UTC the order was persisted
// already expired and reaped on the very next sweep.
func TestDeriveInsertParamsStoresExpiryInUTC(t *testing.T) {
	expiresAt := int64(1788186143)
	event := &oeq.OpenOrderEvent{
		OrderID: uuid.New(), UserID: uuid.New(), MarketID: 1,
		Side: oeq.BuyOrder, Type: oeq.LimitOrder, TimeInForce: oeq.GoodTillCancel,
		Price: 100, Quantity: 10, ExpiresAt: &expiresAt,
	}

	p := DeriveInsertParams(event, &repository.Market{
		ID: 1, BaseInstrumentID: baseInstr, QuoteInstrumentID: quoteInstr, BaseScale: 1,
	})

	if p.ExpiresAt == nil {
		t.Fatal("expires_at was dropped")
	}
	if loc := p.ExpiresAt.Location(); loc != time.UTC {
		t.Fatalf("expires_at is in %v, want UTC — a non-UTC wall clock lands in the column verbatim", loc)
	}
	if got := p.ExpiresAt.Unix(); got != expiresAt {
		t.Fatalf("expires_at = %d, want %d", got, expiresAt)
	}
}
