package orders

import (
	"fmt"

	"github.com/alex99y/matching-engine/common/pkg/utils"
	"github.com/alex99y/matching-engine/db/pkg/repository"
)

// backfillSides fills in Side for any row the repository left nil (i.e. every non-open order —
// open_orders.side only covers orders still resting in the book), via resolveMarketRef. Leaves
// Side nil on a resolution failure rather than erroring the whole request.
func (o *OrderService) backfillSides(orders []repository.OrderRow) {
	for i := range orders {
		if orders[i].Side != nil {
			continue
		}
		if _, side, err := o.resolveMarketRef(orders[i].HaveInstrumentID, orders[i].WantInstrumentID); err == nil {
			orders[i].Side = &side
		}
	}
}

// resolveMarketRef finds the market ref and side for a have/want instrument pair. Which
// instrument is base vs quote isn't known up front, so it tries both orderings against the
// cache — the same approach CancelOrder/BatchCancelOrders already used inline.
func (o *OrderService) resolveMarketRef(haveInstrumentID, wantInstrumentID int) (marketRef, side string, err error) {
	haveInstr, err := o.cacheService.GetInstrumentByID(haveInstrumentID)
	if err != nil {
		return "", "", fmt.Errorf("resolve have instrument: %w", err)
	}
	wantInstr, err := o.cacheService.GetInstrumentByID(wantInstrumentID)
	if err != nil {
		return "", "", fmt.Errorf("resolve want instrument: %w", err)
	}

	ref := utils.MergeMarketRef(haveInstr.Symbol, wantInstr.Symbol)
	if _, err := o.cacheService.GetMarketByRef(ref); err == nil {
		return ref, "sell", nil // have=base, want=quote
	}
	ref = utils.MergeMarketRef(wantInstr.Symbol, haveInstr.Symbol)
	if _, err := o.cacheService.GetMarketByRef(ref); err != nil {
		return "", "", ErrMarketNotFound
	}
	return ref, "buy", nil // have=quote, want=base
}
