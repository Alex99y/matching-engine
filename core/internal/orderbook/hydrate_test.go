package orderbook

import (
	"testing"

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
